package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"dpad/internal/docker"
	"dpad/internal/sortx"
)

func TestStatsSelectedMatchesHighlight(t *testing.T) {
	v := NewStatsView(context.Background(), nil, sortx.Name, false)
	v.Update(statsStartedMsg{})
	v.Update(statsBatchMsg{
		{Stats: docker.Stats{ID: "a", Name: "alpha"}},
		{Stats: docker.Stats{ID: "b", Name: "beta"}},
	})
	v.cur.key = "b"

	// Filtering hides the cursor's row, so the first visible row is highlighted.
	v.SetSize(120, 10)
	v.SetFilter("alpha")
	out := ansi.Strip(v.View())
	if strings.Contains(out, "beta") {
		t.Fatalf("filter should hide beta:\n%s", out)
	}
	if id, name, ok := v.Selected(); !ok || id != "a" || name != "alpha" {
		t.Errorf("Selected() = %q %q %v, want the highlighted row a alpha", id, name, ok)
	}
}

func TestWaitStatsBatchesUpdates(t *testing.T) {
	ch := make(chan docker.StatsUpdate)
	go func() {
		for _, id := range []string{"a", "b", "c"} {
			ch <- docker.StatsUpdate{Stats: docker.Stats{ID: id}}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	batch, ok := waitStats(ch)().(statsSourceBatchMsg)
	if !ok || len(batch.batch) != 3 {
		t.Errorf("waitStats returned %v, want one batch of 3 staggered updates", batch)
	}
}

func TestStatsWatchStartsOnFirstFocus(t *testing.T) {
	v := NewStatsView(context.Background(), nil, sortx.CPU, false)
	if v.Init() != nil {
		t.Error("Init should not start watching stats")
	}
	if v.Focus() == nil {
		t.Error("first Focus should start watching stats")
	}
	if v.Focus() != nil {
		t.Error("later Focus calls should not start another watch")
	}
}
