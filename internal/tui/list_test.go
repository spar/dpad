package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/muesli/termenv"
)

func TestContainersNavigateAndSelect(t *testing.T) {
	v := NewContainersView(nil, nil)
	v.Update(containersMsg{items: []container.Summary{
		{ID: "1", Names: []string{"/web"}, State: "running"},
		{ID: "2", Names: []string{"/db"}, State: "running"},
		{ID: "3", Names: []string{"/old"}, State: "exited"},
	}})
	v.SetSize(120, 10)
	if _, name, _ := v.Selected(); name != "db" {
		t.Errorf("initial selection = %q, want db (first by status, then name)", name)
	}
	v.HandleKey("j")
	v.HandleKey("j")
	if _, name, _ := v.Selected(); name != "old" {
		t.Errorf("after j j selection = %q, want old", name)
	}
	v.HandleKey("s") // name → created → status cycles; status → name
	if v.key != "name" {
		t.Errorf("s cycled to %q, want name", v.key)
	}
	// The cursor follows the container, not the row number.
	if _, name, _ := v.Selected(); name != "old" {
		t.Errorf("after re-sort selection = %q, want old", name)
	}
	v.SetFilter("we")
	out := ansi.Strip(v.View())
	if !strings.Contains(out, "web") || strings.Contains(out, "old") {
		t.Errorf("filter \"we\" shows:\n%s", out)
	}
}

func TestImagesAreNotSelectable(t *testing.T) {
	v := NewImagesView(nil, nil)
	v.Update(imagesMsg{items: []image.Summary{{ID: "sha256:abc", RepoTags: []string{"a:1", "a:2"}}}})
	v.SetSize(120, 10)
	out := ansi.Strip(v.View())
	if strings.Count(out, "\n") != 2 {
		t.Errorf("want a header and one row per tag:\n%s", out)
	}
	if _, _, ok := v.Selected(); ok {
		t.Error("image rows should not select a container")
	}
}

func BenchmarkContainersView(b *testing.B) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	b.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	items := make([]container.Summary, 200)
	for i := range items {
		items[i] = container.Summary{ID: fmt.Sprintf("%064d", i), Names: []string{fmt.Sprintf("/svc-%d", i)},
			Image: "nginx:1.27-alpine", State: "running", Status: "Up 3 hours (healthy)"}
	}
	v := NewContainersView(nil, nil)
	v.Update(containersMsg{items: items})
	v.SetSize(160, 40)
	b.Run("view", func(b *testing.B) {
		for b.Loop() {
			_ = v.View()
		}
	})
	b.Run("keypress", func(b *testing.B) {
		for b.Loop() {
			v.HandleKey("j")
			_ = v.View()
		}
	})
}

func TestContainersReloadOnChange(t *testing.T) {
	f := &fakeClient{containers: []container.Summary{{ID: "1", Names: []string{"/web"}}}}
	v := NewContainersView(nil, f)
	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	cmd := v.Update(containersWatchMsg{ch: ch})
	changed, ok := cmd().(containersChangedMsg)
	if !ok {
		t.Fatalf("a change signal should produce containersChangedMsg")
	}
	// The change reloads containers (and keeps waiting for more changes).
	for _, msg := range runBatch(v.Update(changed)) {
		v.Update(msg)
	}
	if !v.loaded || len(v.items) != 1 {
		t.Errorf("containers not reloaded after a change: %v", v.items)
	}
}

// runBatch runs the commands in a tea.Batch that finish promptly.
func runBatch(cmd tea.Cmd) []tea.Msg {
	var out []tea.Msg
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		return nil
	}
	for _, c := range batch {
		done := make(chan tea.Msg, 1)
		go func() { done <- c() }()
		select {
		case m := <-done:
			out = append(out, m)
		case <-time.After(200 * time.Millisecond):
		}
	}
	return out
}
