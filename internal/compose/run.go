package compose

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

type Event struct {
	Project, State, Output string
}

// Run updates projects serially. A failed pull never starts that project, and
// failures do not prevent independent projects from being updated.
func Run(ctx, lifetime context.Context, env []string, projects []Project) <-chan Event {
	out := make(chan Event, 64)
	send := func(e Event) {
		select {
		case out <- e:
		case <-lifetime.Done():
		}
	}
	go func() {
		defer close(out)
		for _, p := range projects {
			if lifetime.Err() != nil {
				return
			}
			if p.Issue != "" {
				send(Event{p.Name, "skipped", p.Issue})
				continue
			}
			if ctx.Err() != nil {
				send(Event{p.Name, "cancelled", "Not started"})
				continue
			}
			projectCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
			update(projectCtx, env, p, out, send)
			cancel()
		}
	}()
	return out
}

func update(ctx context.Context, env []string, p Project, out chan<- Event, emit func(Event)) {
	log := &limitedOutput{limit: 32 << 10}
	send := func(state, output string) { emit(Event{p.Name, state, output}) }
	check := func() error {
		fresh := p
		if err := resolve(ctx, env, &fresh); err != nil {
			return err
		}
		if !Same([]Project{p}, []Project{fresh}) {
			return errors.New("compose configuration changed; review a fresh preview before updating")
		}
		return nil
	}
	for _, step := range []struct {
		state string
		title string
		args  []string
	}{
		{"pulling", "[1/2] Pull images", []string{"pull", "--include-deps"}},
		{"starting", "[2/2] Start updated services", []string{"up", "-d", "--no-build", "--pull", "never"}},
	} {
		send("checking", log.String())
		if err := check(); err != nil {
			state := "failed"
			if ctx.Err() != nil {
				state = "cancelled"
			}
			send(state, log.String()+"\n"+err.Error())
			return
		}
		progress := "plain"
		if p.JSONProgress {
			progress = "json"
		}
		args := append(progressArgs(p, progress), step.args...)
		args = append(args, "--")
		args = append(args, p.Services...)
		cmd, err := command(ctx, env, p, args...)
		if err != nil {
			send("failed", err.Error())
			return
		}
		_, _ = fmt.Fprintf(log, "\n%s\n$ %s %s\n", step.title, strings.Join(p.CLI, " "), strings.Join(step.args, " "))
		send(step.state, log.String())
		writer := &eventWriter{tail: log, project: p.Name, state: step.state, out: out}
		cmd.Stdout, cmd.Stderr = writer, writer
		if err := cmd.Run(); err != nil {
			state := "failed"
			detail := fmt.Sprintf("%s failed: %v", step.state, err)
			if step.state == "starting" {
				detail += "; some services may already have been recreated"
			}
			if ctx.Err() != nil {
				state = "cancelled"
				if step.state == "starting" {
					state = "unknown"
					detail += "; the remote operation may still be finishing, refresh project state"
				}
			}
			send(state, log.String()+"\n"+detail)
			return
		}
	}
	_, _ = fmt.Fprintln(log, "\nUpdate complete.")
	send("succeeded", log.String())
}

// limitedOutput retains a bounded tail and reports overflow for configuration
// reads. It is also safe when a command writes stdout and stderr concurrently.
type limitedOutput struct {
	mu       sync.Mutex
	data     []byte
	limit    int
	overflow bool
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	if len(w.data)+n > w.limit {
		w.overflow = true
	}
	if n >= w.limit {
		w.data = slices.Clone(p[n-w.limit:])
	} else {
		if excess := len(w.data) + n - w.limit; excess > 0 {
			w.data = slices.Clone(w.data[excess:])
		}
		w.data = append(w.data, p...)
	}
	return n, nil
}

func (w *limitedOutput) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	text := strings.ToValidUTF8(string(w.data), "�")
	if w.overflow {
		if newline := strings.IndexByte(text, '\n'); newline >= 0 {
			text = text[newline+1:]
		}
	}
	return text
}

type eventWriter struct {
	tail           *limitedOutput
	project, state string
	out            chan<- Event
}

func (w *eventWriter) Write(p []byte) (int, error) {
	n, err := w.tail.Write(p)
	select {
	case w.out <- Event{w.project, w.state, w.tail.String()}:
	default:
	}
	return n, err
}
