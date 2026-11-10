package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"dockernext/internal/docker"
)

func TestStatsSelectedMatchesHighlight(t *testing.T) {
	v := NewStatsView(nil, nil, "name", false)
	v.started = true
	v.data["a"] = docker.Stats{ID: "a", Name: "alpha"}
	v.data["b"] = docker.Stats{ID: "b", Name: "beta"}
	v.cur.key = "b"

	// Filtering hides the cursor's row, so the first visible row is highlighted.
	out := ansi.Strip(v.View(120, 10, "alpha"))
	if strings.Contains(out, "beta") {
		t.Fatalf("filter should hide beta:\n%s", out)
	}
	if id, name, ok := v.Selected(); !ok || id != "a" || name != "alpha" {
		t.Errorf("Selected() = %q %q %v, want the highlighted row a alpha", id, name, ok)
	}
}
