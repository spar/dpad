package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"dockernext/internal/docker"
	"dockernext/internal/render"
)

type multiLogClient struct {
	fakeClient
	stream func(context.Context, string, docker.LogOptions, chan<- docker.LogLine) error
}

func (c *multiLogClient) Logs(ctx context.Context, id string, opts docker.LogOptions, out chan<- docker.LogLine) error {
	return c.stream(ctx, id, opts, out)
}

func nextLogMessage(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	out := make(chan tea.Msg, 1)
	go func() { out <- cmd() }()
	select {
	case msg := <-out:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("log command did not finish")
		return nil
	}
}

func TestProjectLogsMergePrefixAndFilter(t *testing.T) {
	c := &multiLogClient{stream: func(ctx context.Context, id string, opts docker.LogOptions, out chan<- docker.LogLine) error {
		if !opts.Follow || opts.Tail != "500" {
			t.Errorf("unexpected log options: %+v", opts)
		}
		select {
		case out <- docker.LogLine{Container: id, Text: "ERROR from " + id, Stderr: id == "db"}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	v, cmd := newLogsView(context.Background(), c, []logTarget{{id: "web", name: "web"}, {id: "db", name: "db"}}, "project shop", 100, 10)
	defer v.Close()
	for !v.ended {
		cmd = v.Update(nextLogMessage(t, cmd))
	}
	if len(v.lines) != 2 || v.err != nil {
		t.Fatalf("not all project logs arrived: %s", v.Status())
	}
	text := ansi.Strip(v.View())
	if !strings.Contains(text, "web │ ERROR from web") || !strings.Contains(text, "db  ┃ ERROR from db") {
		t.Fatalf("project logs lost source names or stderr marker: %q", text)
	}
	if v.colors["web"].Render("source") == v.colors["db"].Render("source") {
		t.Fatal("source colors are not distinct")
	}
	v.setQuery("from web")
	if v.visible != 1 || strings.Contains(ansi.Strip(v.View()), "from db") {
		t.Fatal("merged logs did not filter")
	}
	v.setQuery("")
	if v.visible != 2 {
		t.Fatal("clearing filter did not restore merged logs")
	}
}

func TestProjectLogFailuresRemainVisibleWhileOtherSourcesFollow(t *testing.T) {
	stopped := make(chan struct{})
	c := &multiLogClient{stream: func(ctx context.Context, id string, _ docker.LogOptions, _ chan<- docker.LogLine) error {
		if id == "bad" {
			return errors.New("logging disabled")
		}
		defer close(stopped)
		<-ctx.Done()
		return ctx.Err()
	}}
	v, cmd := newLogsView(context.Background(), c, []logTarget{{id: "bad", name: "bad"}, {id: "live", name: "live"}}, "project shop", 100, 10)
	defer v.Close()
	cmd = v.Update(nextLogMessage(t, cmd))
	if v.ended || !strings.Contains(v.Status(), "bad: logging disabled") {
		t.Fatalf("source failure hidden while following: %s", v.Status())
	}
	v.setQuery("does-not-match")
	if !strings.Contains(v.Status(), "logging disabled") {
		t.Fatal("filter hid source failure")
	}
	v.Close()
	for !v.ended {
		cmd = v.Update(nextLogMessage(t, cmd))
	}
	select {
	case <-stopped:
	default:
		t.Fatal("closing view did not cancel all source streams")
	}
	if strings.Contains(v.Status(), "context canceled") {
		t.Fatal("normal cancellation was shown as an error")
	}
}

func newTestLogsView(height int) *LogsView {
	return &LogsView{
		ch:     make(chan docker.LogLine),
		vp:     viewport.New(viewport.WithWidth(80), viewport.WithHeight(height)),
		search: textinput.New(),
	}
}

func addLogLines(v *LogsView, texts ...string) {
	lines := make([]docker.LogLine, len(texts))
	for i, text := range texts {
		lines[i] = docker.LogLine{Text: text}
	}
	v.Update(logBatchMsg{ch: v.ch, lines: lines})
}

func TestLogsFilterRetainsBufferAndAppliesToNewLines(t *testing.T) {
	v := newTestLogsView(10)
	addLogLines(v, "INFO ready", "ERROR timeout", "WARN retry")
	v.setQuery("ERROR|WARN")
	if !strings.Contains(v.lines[1].text, render.Match.Render("ERROR")) {
		t.Fatal("matching text was not highlighted")
	}
	if v.visible != 2 || strings.Contains(ansi.Strip(v.View()), "INFO") {
		t.Fatalf("filter did not narrow the view: %q", v.View())
	}
	addLogLines(v, "INFO healthy", "ERROR failed")
	if v.visible != 3 || len(v.lines) != 5 || !strings.Contains(ansi.Strip(v.View()), "ERROR failed") {
		t.Fatalf("new lines did not respect filter: %s", v.Status())
	}
	v.Update(tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl})
	if v.query != "" || v.visible != 5 || !strings.Contains(ansi.Strip(v.View()), "INFO healthy") {
		t.Fatal("clearing filter did not restore buffered lines")
	}
}

func TestLogsInvalidRegexKeepsLastValidResults(t *testing.T) {
	v := newTestLogsView(10)
	addLogLines(v, "error: timeout", "INFO ready")
	v.setQuery("(?i)ERROR")
	v.Update(key("/"))
	v.Update(key("["))
	if v.searchErr == nil || v.query != "(?i)ERROR" || v.visible != 1 {
		t.Fatalf("invalid regex replaced valid filter: %s", v.Status())
	}
	v.Update(key("enter"))
	if !v.searching {
		t.Fatal("enter accepted an invalid regex")
	}
	addLogLines(v, "ERROR failed")
	if v.visible != 2 {
		t.Fatal("last valid filter was not applied to new logs")
	}
	v.Update(key("esc"))
	if v.searching || v.searchErr != nil || v.search.Value() != "(?i)ERROR" {
		t.Fatalf("cancel did not restore prior query: %s", v.Status())
	}
}

func TestLogsFilterEditingKeepsAndCancels(t *testing.T) {
	v := newTestLogsView(10)
	addLogLines(v, "ERROR timeout", "INFO ready")
	for _, k := range []string{"/", "E", "R", "R", "O", "R", "enter"} {
		v.Update(key(k))
	}
	if v.searching || v.query != "ERROR" || v.visible != 1 {
		t.Fatalf("enter did not keep filter: %s", v.Status())
	}
	v.Update(key("/"))
	v.Update(key("x"))
	if v.visible != 0 || !strings.Contains(v.View(), "no matching log lines") {
		t.Fatal("no-match filter did not show empty state")
	}
	v.Update(key("esc"))
	if v.query != "ERROR" || v.visible != 1 {
		t.Fatal("escape did not restore committed filter")
	}
}

func TestLogsPastedRegexFiltersImmediately(t *testing.T) {
	a := newTestApp()
	v := newTestLogsView(10)
	a.logs = v
	addLogLines(v, "Error: café unavailable", "INFO healthy")
	press(a, "/")
	a.Update(tea.PasteMsg{Content: "(?i)error.*café"})
	if v.query != "(?i)error.*café" || v.visible != 1 || v.searchErr != nil {
		t.Fatalf("pasted query was not applied: %s", v.Status())
	}
	press(a, "enter")
	if v.searching {
		t.Fatal("pasted query was not committed")
	}
}

func TestLogsFilteredBufferEviction(t *testing.T) {
	v := newTestLogsView(10)
	v.setQuery("ERROR")
	addLogLines(v, "ERROR oldest")
	for range maxLogLines {
		addLogLines(v, "INFO latest")
	}
	if len(v.lines) != maxLogLines || v.visible != 0 {
		t.Fatalf("evicted match is still visible: %s", v.Status())
	}
	v.setQuery("")
	if v.visible != maxLogLines {
		t.Fatal("filter changed buffer retention")
	}
}

func TestLogsFilterPreservesFollowAndPausedScroll(t *testing.T) {
	v := newTestLogsView(3)
	v.setQuery("ERROR")
	for i := range 10 {
		addLogLines(v, fmt.Sprintf("ERROR %d", i))
	}
	if !v.vp.AtBottom() {
		t.Fatal("filtered logs stopped following")
	}
	v.Update(key("g"))
	addLogLines(v, "ERROR next", "INFO hidden")
	if v.vp.YOffset() != 0 || v.vp.AtBottom() {
		t.Fatal("new logs moved a paused viewport")
	}
}

func TestLogsEvictionPreservesPausedVisibleLine(t *testing.T) {
	v := newTestLogsView(4)
	texts := make([]string, maxLogLines)
	for i := range texts {
		texts[i] = fmt.Sprintf("line %04d", i)
	}
	addLogLines(v, texts...)
	v.vp.SetYOffset(100)
	before := ansi.Strip(v.View())
	addLogLines(v, "new line")
	if v.vp.YOffset() != 99 || ansi.Strip(v.View()) != before {
		t.Fatal("buffer eviction moved paused content")
	}
}

func TestLogsWaitBatchesSteadyStream(t *testing.T) {
	v := &LogsView{ch: make(chan docker.LogLine), errc: make(chan error, 1)}
	go func() {
		for range 20 {
			v.ch <- docker.LogLine{Text: "x"}
			time.Sleep(time.Millisecond)
		}
	}()
	msg, ok := v.wait()().(logBatchMsg)
	if !ok || len(msg.lines) < 10 {
		t.Errorf("want one batch with most of a steady 1ms stream, got %d lines", len(msg.lines))
	}
}

func TestLogsPageDownGlidesToTarget(t *testing.T) {
	v := &LogsView{vp: viewport.New(viewport.WithWidth(40), viewport.WithHeight(10))}
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprint(i)
	}
	v.vp.SetContentLines(lines)

	cmd := v.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	v.Update(tea.KeyPressMsg{Code: tea.KeyPgDown}) // a second press extends the target
	if v.target != 20 {
		t.Fatalf("target = %d, want 20", v.target)
	}
	frames := 0
	for cmd != nil {
		frames++
		if frames > 20 {
			t.Fatal("animation did not settle")
		}
		cmd = v.Update(scrollFrameMsg{v, v.gen})
	}
	if y := v.vp.YOffset(); y != 20 {
		t.Errorf("YOffset = %d, want 20", y)
	}
	if frames < 3 {
		t.Errorf("settled in %d frames, want a visible glide", frames)
	}
}
