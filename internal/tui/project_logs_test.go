package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"

	"dockernext/internal/docker"
)

type projectReadCall struct {
	id     string
	opts   docker.LogOptions
	lines  chan<- docker.LogLine
	finish chan error
	exited chan struct{}
}
type projectReadClient struct {
	fakeClient
	calls chan *projectReadCall
}

func (c *projectReadClient) Logs(ctx context.Context, id string, opts docker.LogOptions, lines chan<- docker.LogLine) error {
	call := &projectReadCall{id: id, opts: opts, lines: lines, finish: make(chan error, 1), exited: make(chan struct{})}
	defer close(call.exited)
	select {
	case c.calls <- call:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-call.finish:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func receiveProject[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("project log operation timed out")
		var zero T
		return zero
	}
}

func testProjectSession(t *testing.T, c dockerClient) (*projectLogSession, <-chan docker.LogLine, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	lines := make(chan docker.LogLine, 512)
	errch := make(chan error, 1)
	p := &projectLogSession{ctx: ctx, client: c, project: "shop", snapshots: make(chan projectLogSnapshot, 1), status: make(chan projectLogStatus, 1), events: make(chan projectLogEvent, 256), lines: lines, done: make(chan struct{}), retryBase: 5 * time.Millisecond, retryMax: 20 * time.Millisecond}
	go p.run(errch)
	t.Cleanup(func() { cancel(); receiveProject(t, p.done) })
	return p, lines, cancel
}

func projectStatusUntil(t *testing.T, p *projectLogSession, condition func(projectLogStatus) bool) projectLogStatus {
	t.Helper()
	for {
		status := receiveProject(t, p.status)
		if condition(status) {
			return status
		}
	}
}

func TestProjectLogsTrackScaleRecreationAndEmptyProjects(t *testing.T) {
	c := &projectReadClient{calls: make(chan *projectReadCall, 16)}
	p, lines, _ := testProjectSession(t, c)
	web := composeContainer("web-id", "shop", "web", "running")
	other := composeContainer("unrelated", "other", "web", "running")
	p.update(projectLogSnapshot{items: []container.Summary{web, other}})
	first := receiveProject(t, c.calls)
	if first.id != "web-id" || first.opts.Tail != "500" || first.opts.Since != "" {
		t.Fatalf("unexpected first source: %+v", first)
	}
	first.lines <- docker.LogLine{Time: time.Unix(1000, 0), Text: "first"}
	if line := receiveProject(t, lines); line.Text != "first" {
		t.Fatal("initial source missing")
	}
	worker := composeContainer("worker-id", "shop", "worker", "running")
	p.update(projectLogSnapshot{items: []container.Summary{web, worker, other}})
	second := receiveProject(t, c.calls)
	if second.id != "worker-id" {
		t.Fatal("scale-up did not add project member")
	}
	projectStatusUntil(t, p, func(s projectLogStatus) bool { return s.sources == 2 })
	replacement := composeContainer("web-new-id", "shop", "web", "running")
	replacement.Names = web.Names
	p.update(projectLogSnapshot{items: []container.Summary{replacement, worker}})
	third := receiveProject(t, c.calls)
	receiveProject(t, first.exited)
	if third.id != "web-new-id" || third.opts.Since != "" {
		t.Fatal("recreated container inherited old ID cursor")
	}
	third.lines <- docker.LogLine{Time: time.Unix(1001, 0), Text: "replacement"}
	if line := receiveProject(t, lines); line.Text != "replacement" || line.Container != "web-id" {
		t.Fatalf("replacement source not shown with current name: %+v", line)
	}
	p.update(projectLogSnapshot{})
	receiveProject(t, second.exited)
	receiveProject(t, third.exited)
	projectStatusUntil(t, p, func(s projectLogStatus) bool { return s.sources == 0 })
	select {
	case <-p.done:
		t.Fatal("empty project closed its viewer")
	default:
	}
	p.update(projectLogSnapshot{items: []container.Summary{worker}})
	if receiveProject(t, c.calls).id != "worker-id" {
		t.Fatal("empty project did not accept a new member")
	}
}

func TestProjectLogsReconnectWithoutReplayingTimestampBoundary(t *testing.T) {
	c := &projectReadClient{calls: make(chan *projectReadCall, 16)}
	p, lines, _ := testProjectSession(t, c)
	p.update(projectLogSnapshot{items: []container.Summary{composeContainer("web", "shop", "web", "running")}})
	first := receiveProject(t, c.calls)
	at := time.Unix(1000, 123)
	for _, text := range []string{"a", "b"} {
		first.lines <- docker.LogLine{Time: at, Text: text}
		receiveProject(t, lines)
	}
	first.finish <- errors.New("connection reset")
	projectStatusUntil(t, p, func(s projectLogStatus) bool { return strings.Contains(s.err, "connection reset") })
	second := receiveProject(t, c.calls)
	if second.opts.Since != at.Format(time.RFC3339Nano) || second.opts.Tail != "all" {
		t.Fatalf("reconnect did not resume from cursor: %+v", second.opts)
	}
	for _, text := range []string{"a", "b", "c"} {
		second.lines <- docker.LogLine{Time: at, Text: text}
	}
	second.lines <- docker.LogLine{Time: at.Add(time.Second), Text: "d"}
	for _, want := range []string{"c", "d"} {
		if line := receiveProject(t, lines); line.Text != want {
			t.Fatalf("replayed or lost boundary line: got %q want %q", line.Text, want)
		}
	}
	projectStatusUntil(t, p, func(s projectLogStatus) bool { return s.err == "" && strings.Contains(s.notice, "reconnected") })
}

func TestLogCursorKeepsNewLinesWhenBoundaryPrefixWasRotated(t *testing.T) {
	var cursor logCursor
	var replay []uint64
	at := time.Unix(1000, 0)
	for _, text := range []string{"a", "b"} {
		cursor.accept(docker.LogLine{Time: at, Text: text}, &replay)
	}
	replay = append(replay, cursor.hashes...)
	if cursor.accept(docker.LogLine{Time: at, Text: "b"}, &replay) {
		t.Fatal("retained boundary record replayed")
	}
	if !cursor.accept(docker.LogLine{Time: at, Text: "new"}, &replay) {
		t.Fatal("new equal-timestamp record was lost after rotation")
	}
	if !cursor.accept(docker.LogLine{Time: at, Text: "b", Stderr: true}, &replay) {
		t.Fatal("stderr was confused with stdout")
	}
	for i := range 2 * maxLogBoundary {
		cursor.accept(docker.LogLine{Time: at, Text: fmt.Sprint(i)}, &replay)
	}
	if len(cursor.hashes) > maxLogBoundary {
		t.Fatal("timestamp boundary state grew without a bound")
	}
}

func BenchmarkProjectLogCursor(b *testing.B) {
	var cursor logCursor
	var replay []uint64
	line := docker.LogLine{Time: time.Unix(1000, 0), Text: "INFO incoming request completed"}
	cursor.accept(line, &replay)
	b.ReportAllocs()
	for b.Loop() {
		line.Time = line.Time.Add(time.Nanosecond)
		cursor.accept(line, &replay)
	}
}

func TestProjectStoppedSourceWaitsThenRestartsWithoutOverlap(t *testing.T) {
	c := &projectReadClient{calls: make(chan *projectReadCall, 16)}
	p, lines, _ := testProjectSession(t, c)
	stopped := composeContainer("web", "shop", "web", "exited")
	p.update(projectLogSnapshot{items: []container.Summary{stopped}})
	first := receiveProject(t, c.calls)
	first.lines <- docker.LogLine{Time: time.Unix(1000, 0), Text: "historical"}
	receiveProject(t, lines)
	first.finish <- nil
	projectStatusUntil(t, p, func(s projectLogStatus) bool { return s.following == 0 && s.retrying == 0 })
	select {
	case <-c.calls:
		t.Fatal("stopped source was polled repeatedly")
	case <-time.After(30 * time.Millisecond):
	}
	running := composeContainer("web", "shop", "web", "running")
	p.update(projectLogSnapshot{items: []container.Summary{running}})
	second := receiveProject(t, c.calls)
	p.update(projectLogSnapshot{items: []container.Summary{stopped}})
	projectStatusUntil(t, p, func(s projectLogStatus) bool { return strings.Contains(s.notice, "source stopped") })
	p.update(projectLogSnapshot{items: []container.Summary{running}})
	third := receiveProject(t, c.calls)
	select {
	case <-second.exited:
	default:
		t.Fatal("restart overlapped readers for the same ID")
	}
	if third.id != "web" {
		t.Fatal("same-ID restart did not reconnect")
	}
}

func TestProjectDiscoveryFailureKeepsSourcesAndRecovers(t *testing.T) {
	c := &projectReadClient{calls: make(chan *projectReadCall, 16)}
	p, lines, _ := testProjectSession(t, c)
	items := []container.Summary{composeContainer("web", "shop", "web", "running")}
	p.update(projectLogSnapshot{items: items})
	call := receiveProject(t, c.calls)
	p.update(projectLogSnapshot{err: errors.New("daemon offline")})
	status := projectStatusUntil(t, p, func(s projectLogStatus) bool { return strings.Contains(s.err, "daemon offline") })
	if status.sources != 1 {
		t.Fatal("failed discovery discarded sources")
	}
	call.lines <- docker.LogLine{Time: time.Unix(1000, 0), Text: "still streaming"}
	if receiveProject(t, lines).Text != "still streaming" {
		t.Fatal("discovery error stopped healthy reader")
	}
	p.update(projectLogSnapshot{items: items})
	projectStatusUntil(t, p, func(s projectLogStatus) bool { return s.err == "" })
}

func TestProjectBackoffIsCappedAndCloseCancelsRetries(t *testing.T) {
	var calls atomic.Int32
	c := &multiLogClient{stream: func(context.Context, string, docker.LogOptions, chan<- docker.LogLine) error {
		calls.Add(1)
		return errors.New("unavailable")
	}}
	p, _, cancel := testProjectSession(t, c)
	p.update(projectLogSnapshot{items: []container.Summary{composeContainer("web", "shop", "web", "running")}})
	projectStatusUntil(t, p, func(s projectLogStatus) bool { return strings.Contains(s.notice, "in 20ms") })
	cancel()
	receiveProject(t, p.done)
	before := calls.Load()
	time.Sleep(30 * time.Millisecond)
	if calls.Load() != before {
		t.Fatal("retry continued after session closed")
	}
}

func TestProjectViewerKeepsFilterScrollAndBufferAcrossSourceChanges(t *testing.T) {
	c := &projectReadClient{calls: make(chan *projectReadCall, 16)}
	web := composeContainer("web", "shop", "web", "running")
	v, cmd := newProjectLogsView(context.Background(), c, "shop", []container.Summary{web}, 100, 4)
	defer func() { v.Close(); receiveProject(t, v.project.done) }()
	first := receiveProject(t, c.calls)
	for i := range 20 {
		first.lines <- docker.LogLine{Time: time.Unix(1000, int64(i)), Text: fmt.Sprintf("INFO %02d", i)}
	}
	for len(v.lines) < 20 {
		cmd = v.Update(nextLogMessage(t, cmd))
	}
	v.setQuery("INFO")
	v.Update(key("g"))
	worker := composeContainer("much-longer-worker-name", "shop", "worker", "running")
	v.Update(containersMsg{items: []container.Summary{web, worker}})
	second := receiveProject(t, c.calls)
	second.lines <- docker.LogLine{Time: time.Unix(1001, 0), Text: "INFO new source"}
	for len(v.lines) < 21 {
		cmd = v.Update(nextLogMessage(t, cmd))
	}
	if v.query != "INFO" || v.vp.YOffset() != 0 || v.vp.AtBottom() || !strings.Contains(ansi.Strip(v.View()), "INFO 00") {
		t.Fatalf("source change lost state: query=%q offset=%d bottom=%v view=%q", v.query, v.vp.YOffset(), v.vp.AtBottom(), ansi.Strip(v.View()))
	}
	if len(v.colors) != 0 {
		t.Fatal("dynamic source colors retained an unbounded name map")
	}
	if v.Update(projectLogStatusMsg{ch: make(chan docker.LogLine)}) != nil {
		t.Fatal("stale viewer status created a second waiter")
	}
}
