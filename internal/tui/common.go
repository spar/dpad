// Package tui implements the interactive full-screen interface and the live
// `dn stats` view.
package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"dockernext/internal/render"
)

const refreshInterval = 2 * time.Second

// tab is one top-level view of the App.
type tab interface {
	Title() string
	Init() tea.Cmd
	// Update handles data messages; every tab sees every non-key message.
	Update(msg tea.Msg) tea.Cmd
	// HandleKey handles keys when the tab is active; returns false if unhandled.
	HandleKey(key string) (tea.Cmd, bool)
	View(width, height int, filter string) string
	// Selected returns the highlighted container, if the tab lists containers.
	Selected() (id, name string, ok bool)
	Status() string
	Help() [][2]string
}

// cursor tracks the selected row by key, so it survives re-sorting.
type cursor struct{ key string }

func (c *cursor) index(keys []string) int {
	if i := slices.Index(keys, c.key); i >= 0 {
		return i
	}
	return 0
}

func (c *cursor) move(keys []string, delta int) {
	if len(keys) == 0 {
		return
	}
	i := min(max(c.index(keys)+delta, 0), len(keys)-1)
	c.key = keys[i]
}

// navKey applies standard list navigation; returns false if key isn't one.
func (c *cursor) navKey(keys []string, key string, page int) bool {
	switch key {
	case "up", "k":
		c.move(keys, -1)
	case "down", "j":
		c.move(keys, 1)
	case "pgup", "ctrl+u":
		c.move(keys, -page)
	case "pgdown", "ctrl+d":
		c.move(keys, page)
	case "home", "g":
		c.move(keys, -len(keys))
	case "end", "G":
		c.move(keys, len(keys))
	default:
		return false
	}
	return true
}

// tableWindow renders a table with the selected row kept in view within height lines.
func tableWindow(t render.Table, height int) string {
	header, rows := t.Lines()
	avail := max(height-1, 1)
	start := 0
	if t.Selected >= avail {
		start = t.Selected - avail + 1
	}
	end := min(start+avail, len(rows))
	return strings.Join(append([]string{header}, rows[start:end]...), "\n")
}

func matches(filter string, fields ...string) bool {
	if filter == "" {
		return true
	}
	f := strings.ToLower(filter)
	for _, s := range fields {
		if strings.Contains(strings.ToLower(s), f) {
			return true
		}
	}
	return false
}

func sortLabel(key string, reverse bool) string {
	if reverse {
		return fmt.Sprintf("sort: %s (reversed)", key)
	}
	return "sort: " + key
}

func tick(msg tea.Msg) tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return msg })
}
