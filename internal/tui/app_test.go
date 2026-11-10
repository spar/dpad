package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"dockernext/internal/sortx"
)

func newTestApp() *App {
	return &App{
		tabs:    []tab{NewContainersView(nil, nil), NewImagesView(nil, nil), NewStatsView(nil, nil, sortx.CPU, false)},
		filters: make([]string, 3),
	}
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
