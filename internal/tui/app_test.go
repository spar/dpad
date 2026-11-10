package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"

	"dockernext/internal/sortx"
)

func newTestApp() *App {
	a := &App{
		tabs:   []tab{NewContainersView(nil, nil), NewImagesView(nil, nil), NewStatsView(nil, nil, sortx.CPU, false)},
		filter: textinput.New(),
	}
	a.resize(120, 30)
	return a
}

func press(a *App, keys ...string) {
	for _, k := range keys {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		switch k {
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		}
		a.Update(msg)
	}
}

func TestAppSwitchTabs(t *testing.T) {
	a := newTestApp()
	for _, tt := range []struct {
		key  string
		want int
	}{{"3", 2}, {"1", 0}, {"tab", 1}, {"4", 1}, {"0", 1}} {
		press(a, tt.key)
		if a.active != tt.want {
			t.Errorf("after %q active = %d, want %d", tt.key, a.active, tt.want)
		}
	}
}

func TestAppFilterIsPerTab(t *testing.T) {
	a := newTestApp()
	press(a, "/", "w", "e", "enter")
	if got := a.tabs[0].Filter(); got != "we" {
		t.Fatalf("containers filter = %q, want we", got)
	}
	press(a, "2")
	if a.filter.Value() != "" || a.tabs[1].Filter() != "" {
		t.Errorf("images tab should start unfiltered, input shows %q", a.filter.Value())
	}
	press(a, "1")
	if a.filter.Value() != "we" {
		t.Errorf("switching back should restore the filter, input shows %q", a.filter.Value())
	}
	press(a, "esc")
	if a.tabs[0].Filter() != "" {
		t.Errorf("esc should clear the filter, got %q", a.tabs[0].Filter())
	}
}

func TestFooterHidesContainerActionsOnImages(t *testing.T) {
	a := newTestApp()
	a.Update(containersMsg{items: []container.Summary{{ID: "1", Names: []string{"/web"}}}})
	if f := ansi.Strip(a.footer()); !strings.Contains(f, "logs") {
		t.Errorf("containers footer should offer logs: %q", f)
	}
	press(a, "2")
	if f := ansi.Strip(a.footer()); strings.Contains(f, "logs") || strings.Contains(f, "S/x/R") {
		t.Errorf("images footer offers container actions: %q", f)
	}
}
