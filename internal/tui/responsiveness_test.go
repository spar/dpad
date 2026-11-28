package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"

	"github.com/spar/dpad/internal/docker"
)

func TestPreparedListInvalidation(t *testing.T) {
	v := NewContainersView(t.Context(), &fakeClient{})
	v.SetSize(120, 10)
	v.Update(containersMsg{items: []container.Summary{{ID: "a", Names: []string{"/alpha"}, Image: "image-a", State: container.StateRunning}, {ID: "b", Names: []string{"/beta"}, Image: "image-b", State: container.StateRunning}}})
	v.View()
	table := v.table
	v.HandleKey("j")
	v.View()
	if v.table != table {
		t.Fatal("selection remeasured the entire table")
	}
	v.SetSize(60, 7)
	for line := range strings.SplitSeq(ansi.Strip(v.View()), "\n") {
		if ansi.StringWidth(line) > 60 {
			t.Fatal("resize ignored cached table width")
		}
	}
	if v.table != table {
		t.Fatal("viewport resize rebuilt unchanged cells")
	}
	v.HandleKey("r")
	v.View()
	if v.table == table {
		t.Fatal("sort change reused obsolete header/rows")
	}
	v.SetFilter("alpha")
	if out := v.View(); strings.Contains(out, "beta") {
		t.Fatal(out)
	}
	v.SetFilter("")
	v.Update(containersMsg{items: []container.Summary{{ID: "c", Names: []string{"/charlie"}, State: container.StateRunning}}})
	if out := v.View(); !strings.Contains(out, "charlie") || strings.Contains(out, "alpha") {
		t.Fatal("data refresh retained stale cells", out)
	}
	v.Update(containersMsg{})
	v.View()
	if v.table != nil {
		t.Fatal("empty view retained a large old prepared table")
	}
}
func TestEventRefreshDebouncesAndManualRefreshInvalidatesTimer(t *testing.T) {
	v := NewContainersView(t.Context(), &fakeClient{})
	if v.Update(eventsBatchMsg{updates: []docker.EventUpdate{}}) != nil {
		t.Fatal("empty event batch scheduled a reload")
	}
	batch := eventsBatchMsg{updates: []docker.EventUpdate{testEvent(1)}}
	if v.Update(batch) == nil {
		t.Fatal("no coalesced refresh")
	}
	gen := v.eventGeneration
	for range 100 {
		if v.Update(batch) != nil {
			t.Fatal("event burst scheduled multiple timers")
		}
	}
	manual := v.Update(refreshMsg{})
	if manual == nil {
		t.Fatal("manual refresh was delayed")
	}
	if v.Update(containersEventRefreshMsg{generation: gen}) != nil {
		t.Fatal("old automatic timer duplicated manual refresh")
	}
	v.Update(manual())
	if v.Update(batch) == nil {
		t.Fatal("next event window was lost")
	}
	cmd := v.Update(containersEventRefreshMsg{generation: v.eventGeneration})
	if cmd == nil {
		t.Fatal("coalesced refresh never ran")
	}
	v.Update(cmd())
}
func TestSSHDetectionAndImmediateLogScrolling(t *testing.T) {
	for _, k := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		t.Setenv(k, "")
	}
	if remoteTerminal() {
		t.Fatal("local terminal classified as SSH")
	}
	for _, k := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		t.Setenv(k, "present")
		if !remoteTerminal() {
			t.Fatal("SSH terminal undetected", k)
		}
		t.Setenv(k, "")
	}
	t.Setenv("SSH_TTY", "present")
	v, ctx := newLogPane(t.Context(), "test", 80, 10)
	defer v.Close()
	_ = ctx
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprint(i)
	}
	v.vp.SetContentLines(lines)
	if cmd := v.scrollBy(10); cmd != nil || v.animating || v.vp.YOffset() != 10 {
		t.Fatal("SSH paging queued animation frames")
	}
	v.scrollBy(1000)
	if !v.vp.AtBottom() {
		t.Fatal("SSH scroll exceeded bounds")
	}
}
