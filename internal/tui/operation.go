package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"dpad/internal/render"
	"dpad/internal/sortx"
)

// opStage is where a previewed operation (prune, project action, Compose
// update) is in its life.
type opStage string

const (
	stageLoading    opStage = "loading"
	stageReading    opStage = "reading"
	stagePreview    opStage = "preview"
	stageChecking   opStage = "checking"
	stageRunning    opStage = "running"
	stageCancelling opStage = "cancelling"
	stageDone       opStage = "done"
	stageError      opStage = "error"
)

const projectWorkers = 4

// projectResult reports progress on one target; an empty id carries a note
// for the whole operation.
type projectResult struct{ id, status, detail string }

// opRow is one target of an operation and what happened to it.
type opRow[T any] struct {
	target         T
	status, detail string
}

// operation is the part prune and project actions share: a list of targets
// that is previewed, rechecked on confirmation, then worked through while
// each row reports its result.
type operation[T any] struct {
	list[opRow[T]]
	ctx             context.Context
	cancel          context.CancelFunc
	operationCancel context.CancelFunc
	client          dockerClient
	stage           opStage
	note            string
	targets         []T
	results         <-chan projectResult
	id              func(T) string
}

func newOperation[T any](ctx context.Context, c dockerClient, id func(T) string) operation[T] {
	ctx, cancel := context.WithCancel(ctx)
	return operation[T]{
		ctx: ctx, cancel: cancel, client: c, stage: stageLoading, id: id,
		list: list[opRow[T]]{sort: func([]opRow[T], sortx.Key, bool) {}, loaded: true},
	}
}

func (o *operation[T]) busy() bool { return o.stage == stageRunning || o.stage == stageCancelling }

// requestCancel stops work that hasn't started; requests in flight finish.
func (o *operation[T]) requestCancel() {
	o.operationCancel()
	o.stage = stageCancelling
}

func (o *operation[T]) Close() {
	o.cancel()
	if o.operationCancel != nil {
		o.operationCancel()
	}
}

func (o *operation[T]) SetSize(width, height int) {
	o.list.SetSize(width, height)
	o.page = max(height-4, 1)
}

// showTargets lists targets as pending rows.
func (o *operation[T]) showTargets(targets []T) {
	o.targets = targets
	rows := make([]opRow[T], len(targets))
	for i, t := range targets {
		rows[i] = opRow[T]{target: t, status: "pending"}
	}
	o.setItems(rows, nil)
}

// start runs the confirmed operation; run gets a context that requestCancel ends.
func (o *operation[T]) start(run func(ctx context.Context) <-chan projectResult) {
	o.stage = stageRunning
	ctx, cancel := context.WithCancel(o.ctx)
	o.operationCancel = cancel
	o.results = run(ctx)
}

func (o *operation[T]) waitResults(message func([]projectResult, bool) tea.Msg) tea.Cmd {
	return waitActionBatch(o.ctx, o.results, message)
}

// applyResults records a batch of results on their rows; done ends the operation.
func (o *operation[T]) applyResults(results []projectResult, done bool) {
	for _, result := range results {
		if result.id == "" {
			o.note = result.detail
			continue
		}
		for i := range o.items {
			if o.id(o.items[i].target) == result.id {
				o.items[i].status, o.items[i].detail = result.status, result.detail
				break
			}
		}
	}
	o.rowCache = nil
	if done {
		o.stage = stageDone
		o.operationCancel()
	}
}

func (o *operation[T]) navigate(key string) {
	_, keys := o.visible()
	o.cur.navKey(keys, key, o.page)
}

// counts tallies rows by status.
func (o *operation[T]) counts() map[string]int {
	counts := map[string]int{}
	for _, r := range o.items {
		counts[r.status]++
	}
	return counts
}

// frame draws the title, a note line and body below them. The selected row's
// detail, when it has one, replaces the note.
func (o *operation[T]) frame(title, note string, body func(height int) string) string {
	_, keys := o.visible()
	if len(keys) > 0 {
		selected := keys[o.cur.index(keys)]
		for _, r := range o.items {
			if o.id(r.target) == selected && r.detail != "" {
				note = r.detail
				break
			}
		}
	}
	note = strings.NewReplacer("\r", " ", "\n", " ").Replace(note)
	lines := []string{ansi.Truncate(render.Bold.Render(title), o.width, ""), ansi.Truncate(render.Dim.Render(note), o.width, "")}
	if o.height > 2 {
		lines = append(lines, strings.Split(body(max(o.height-2, 1)), "\n")...)
	}
	return strings.Join(lines[:min(len(lines), max(o.height, 1))], "\n")
}

// opFailure is the result for a target whose request failed. A request that
// was cut off may still have applied, which hint tells the user how to check.
func opFailure(id string, err error, hint string) projectResult {
	result := projectResult{id: id, status: "failed", detail: err.Error()}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		result.status = "unknown"
		result.detail += "; " + hint
	}
	return result
}

// runTargets applies do to each target, workers at a time, and reports each
// one as running and then okStatus or a failure. Targets not started before
// ctx ends are reported cancelled. finish, when set, is sent as a closing note.
func runTargets[T any](ctx, viewCtx context.Context, targets []T, workers int, id func(T) string, do func(context.Context, T) error, okStatus, hint, finish string) <-chan projectResult {
	out := make(chan projectResult, 64)
	send := func(r projectResult) {
		select {
		case out <- r:
		case <-viewCtx.Done():
		}
	}
	go func() {
		defer close(out)
		jobs := make(chan T)
		var wg sync.WaitGroup
		for range min(workers, len(targets)) {
			wg.Go(func() {
				for t := range jobs {
					if ctx.Err() != nil {
						send(projectResult{id(t), "cancelled", "Not started"})
						continue
					}
					send(projectResult{id(t), "running", ""})
					actionCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
					err := do(actionCtx, t)
					cancel()
					if err != nil {
						send(opFailure(id(t), err, hint))
					} else {
						send(projectResult{id: id(t), status: okStatus})
					}
				}
			})
		}
		for _, t := range targets {
			select {
			case jobs <- t:
			case <-viewCtx.Done():
				close(jobs)
				wg.Wait()
				return
			}
		}
		close(jobs)
		wg.Wait()
		if finish != "" {
			send(projectResult{detail: finish})
		}
	}()
	return out
}

// dismiss handles the close key for an operation: it cancels one that is
// running, and closes one that isn't. It reports whether it closed.
func dismiss(v interface {
	busy() bool
	requestCancel()
	Close()
}) bool {
	if v.busy() {
		v.requestCancel()
		return false
	}
	v.Close()
	return true
}
