package tui

import (
	"context"
	"fmt"
	"hash/maphash"
	"sync"
	"time"

	"github.com/moby/moby/api/types/container"

	"dockernext/internal/docker"
)

const projectLogBackoff = time.Second
const maxProjectLogBackoff = 5 * time.Second
const maxLogBoundary = 256

var logBoundarySeed = maphash.MakeSeed()

type projectLogSnapshot struct {
	items []container.Summary
	err   error
}
type projectLogStatus struct {
	sources, following, retrying int
	notice, err                  string
}
type projectLogStatusMsg struct {
	ch     <-chan docker.LogLine
	status projectLogStatus
}

type projectLogSession struct {
	ctx                 context.Context
	client              dockerClient
	project             string
	snapshots           chan projectLogSnapshot
	status              chan projectLogStatus
	events              chan projectLogEvent
	lines               chan<- docker.LogLine
	done                chan struct{}
	retryBase, retryMax time.Duration
}

type projectLogSource struct {
	id, name  string
	active    bool
	attempted bool
	attempt   *projectLogAttempt
	restart   bool
	retryAt   time.Time
	backoff   time.Duration
	err       error
	cursor    logCursor
}

type logCursor struct {
	at     time.Time
	hashes []uint64
	next   int
}

func (c *logCursor) accept(line docker.LogLine, replay *[]uint64) bool {
	if line.Time.IsZero() {
		return true
	}
	if line.Time.Before(c.at) {
		return false
	}
	hash := maphash.String(logBoundarySeed, line.Text)
	if line.Stderr {
		hash ^= 1
	}
	if line.Time.After(c.at) {
		c.at = line.Time
		c.hashes = c.hashes[:0]
		c.next = 0
		*replay = nil
	} else {
		for i, h := range *replay {
			if h == hash {
				last := len(*replay) - 1
				(*replay)[i] = (*replay)[last]
				*replay = (*replay)[:last]
				return false
			}
		}
	}
	if len(c.hashes) < maxLogBoundary {
		c.hashes = append(c.hashes, hash)
	} else {
		c.hashes[c.next] = hash
		c.next = (c.next + 1) % maxLogBoundary
	}
	return true
}

type projectLogAttempt struct {
	source       *projectLogSource
	cancel       context.CancelFunc
	discard      bool
	replay       []uint64
	started      time.Time
	reconnecting bool
}
type projectLogEvent struct {
	attempt *projectLogAttempt
	line    docker.LogLine
	done    bool
	err     error
}

func newProjectLogSession(ctx context.Context, c dockerClient, project string, items []container.Summary, lines chan<- docker.LogLine, errc chan<- error) *projectLogSession {
	p := &projectLogSession{ctx: ctx, client: c, project: project, snapshots: make(chan projectLogSnapshot, 1), status: make(chan projectLogStatus, 1), events: make(chan projectLogEvent, 256), lines: lines, done: make(chan struct{}), retryBase: projectLogBackoff, retryMax: maxProjectLogBackoff}
	p.update(projectLogSnapshot{items: items})
	go p.run(errc)
	return p
}

func (p *projectLogSession) update(snapshot projectLogSnapshot) {
	for {
		select {
		case <-p.ctx.Done():
			return
		case p.snapshots <- snapshot:
			return
		default:
		}
		select {
		case <-p.snapshots:
		default:
		}
	}
}

func (p *projectLogSession) read(ctx context.Context, a *projectLogAttempt, opts docker.LogOptions) {
	lines := make(chan docker.LogLine, 64)
	done := make(chan error, 1)
	go func() { done <- p.client.Logs(ctx, a.source.id, opts, lines); close(lines) }()
	for line := range lines {
		select {
		case p.events <- projectLogEvent{attempt: a, line: line}:
		case <-ctx.Done():
		}
	}
	err := <-done
	select {
	case p.events <- projectLogEvent{attempt: a, done: true, err: err}:
	case <-p.ctx.Done():
	}
}

func (p *projectLogSession) run(errc chan<- error) {
	var workers sync.WaitGroup
	sources := make(map[string]*projectLogSource)
	defer func() {
		for _, s := range sources {
			if s.attempt != nil {
				s.attempt.cancel()
			}
		}
		workers.Wait()
		errc <- nil
		close(p.lines)
		close(p.done)
	}()
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	var retry <-chan time.Time
	notice := "waiting for project containers"
	var discoveryErr error
	schedule := func() {
		timer.Stop()
		retry = nil
		var next time.Time
		for _, s := range sources {
			if !s.retryAt.IsZero() && (next.IsZero() || s.retryAt.Before(next)) {
				next = s.retryAt
			}
		}
		if !next.IsZero() {
			timer.Reset(max(time.Until(next), 0))
			retry = timer.C
		}
	}
	publish := func() {
		status := projectLogStatus{sources: len(sources), notice: notice}
		if discoveryErr != nil {
			status.err = "container discovery: " + discoveryErr.Error()
		}
		for _, s := range sources {
			if s.attempt != nil {
				status.following++
			}
			if !s.retryAt.IsZero() || s.restart {
				status.retrying++
			}
			if status.err == "" && s.err != nil {
				status.err = s.name + ": " + s.err.Error()
			}
		}
		select {
		case p.status <- status:
		default:
			select {
			case <-p.status:
			default:
			}
			select {
			case p.status <- status:
			default:
			}
		}
	}
	start := func(s *projectLogSource) {
		ctx, cancel := context.WithCancel(p.ctx)
		a := &projectLogAttempt{source: s, cancel: cancel, replay: append([]uint64(nil), s.cursor.hashes...), started: time.Now(), reconnecting: s.attempted}
		s.attempted = true
		opts := docker.LogOptions{Follow: true, Tail: "500"}
		if !s.cursor.at.IsZero() {
			opts.Tail = "all"
			opts.Since = s.cursor.at.Format(time.RFC3339Nano)
		}
		s.attempt, s.retryAt, s.restart = a, time.Time{}, false
		workers.Go(func() { p.read(ctx, a, opts) })
	}
	for {
		select {
		case <-p.ctx.Done():
			return
		case snapshot := <-p.snapshots:
			discoveryErr = snapshot.err
			if snapshot.err != nil {
				publish()
				continue
			}
			seen := make(map[string]bool)
			joined, left := 0, 0
			changedName := ""
			for _, c := range snapshot.items {
				if c.Labels[composeProjectLabel] != p.project {
					continue
				}
				seen[c.ID] = true
				active := c.State == container.StateRunning || c.State == container.StatePaused || c.State == container.StateRestarting
				s := sources[c.ID]
				if s == nil {
					s = &projectLogSource{id: c.ID, name: docker.ContainerName(c.Names), active: active, backoff: p.retryBase}
					sources[c.ID] = s
					start(s)
					joined++
					changedName = s.name
				} else {
					s.name = docker.ContainerName(c.Names)
					if !s.active && active {
						s.backoff = p.retryBase
						if s.attempt != nil {
							s.restart = true
							s.attempt.discard = true
							s.attempt.cancel()
						} else {
							start(s)
						}
						notice = "reconnecting " + s.name
					}
					if s.active && !active {
						notice = "source stopped " + s.name
					}
					s.active = active
					if !active && s.err == nil {
						s.retryAt = time.Time{}
					}
				}
			}
			for id, s := range sources {
				if !seen[id] {
					if s.attempt != nil {
						s.attempt.cancel()
					}
					delete(sources, id)
					left++
					changedName = s.name
				}
			}
			if joined+left > 1 {
				notice = fmt.Sprintf("sources changed: %d joined, %d left", joined, left)
			} else if joined == 1 {
				notice = "joined " + changedName
			} else if left == 1 {
				notice = "left " + changedName
			}
			publish()
			schedule()
		case event := <-p.events:
			a := event.attempt
			s := sources[a.source.id]
			if s != a.source || s.attempt != a {
				continue
			}
			if !event.done {
				if a.discard {
					continue
				}
				line := event.line
				if !s.cursor.accept(line, &a.replay) {
					continue
				}
				if s.err != nil || a.reconnecting {
					notice = "reconnected " + s.name
					s.err = nil
					a.reconnecting = false
					publish()
				}
				s.backoff = p.retryBase
				line.Container = s.name
				select {
				case p.lines <- line:
				case <-p.ctx.Done():
					return
				}
				continue
			}
			a.cancel()
			s.attempt = nil
			if s.restart {
				start(s)
				publish()
				schedule()
				continue
			}
			s.err = event.err
			if s.active || event.err != nil {
				if time.Since(a.started) >= p.retryMax {
					s.backoff = p.retryBase
				}
				delay := s.backoff
				s.retryAt = time.Now().Add(delay)
				s.backoff = min(2*delay, p.retryMax)
				notice = fmt.Sprintf("reconnecting %s in %s", s.name, delay)
			} else {
				notice = "waiting for " + s.name + " to start"
			}
			publish()
			schedule()
		case now := <-retry:
			for _, s := range sources {
				if !s.retryAt.IsZero() && !s.retryAt.After(now) {
					start(s)
				}
			}
			publish()
			schedule()
		}
	}
}
