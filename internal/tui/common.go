// Package tui implements the interactive full-screen interface and the live
// `dn stats` view.
package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"

	"dockernext/internal/docker"
	"dockernext/internal/render"
	"dockernext/internal/sortx"
)

// pollInterval is how often the containers tab reloads besides on daemon
// events: it keeps relative times ("Up 5 minutes") current, and is the only
// refresh if events are unavailable.
const pollInterval = 10 * time.Second

// dockerClient is what the TUI needs from docker.Client, so tests can fake it.
type dockerClient interface {
	ConnectionLabel() string
	Ping(context.Context) error
	Containers(ctx context.Context, all bool) ([]container.Summary, error)
	Images(ctx context.Context) ([]image.Summary, error)
	ImageDetails(ctx context.Context, id string) (docker.ImageInfo, error)
	RemoveImage(ctx context.Context, removal docker.ImageRemoval) error
	Inspect(ctx context.Context, id string) (container.InspectResponse, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Restart(ctx context.Context, id string) error
	Logs(ctx context.Context, id string, opts docker.LogOptions, out chan<- docker.LogLine) error
	WatchStats(ctx context.Context) (<-chan docker.StatsUpdate, error)
	ContainerChanges(ctx context.Context) (<-chan struct{}, error)
}

var _ dockerClient = (*docker.Client)(nil)

// tab is one top-level view of the App.
type tab interface {
	Title() string
	Init() tea.Cmd
	// Focus is called whenever the tab becomes active, so expensive work can
	// wait until the tab is first shown.
	Focus() tea.Cmd
	// Update handles data messages; every tab sees every non-key message.
	Update(msg tea.Msg) tea.Cmd
	// HandleKey handles keys the App doesn't when the tab is active.
	HandleKey(key string) tea.Cmd
	// SetSize sets the space the tab's View has; SetFilter narrows its rows.
	SetSize(width, height int)
	SetFilter(filter string)
	Filter() string
	View() string
	// Selected returns the highlighted container, if the tab lists containers.
	Selected() (id, name string, ok bool)
	Status() string
	SortKey() sortx.Key
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

// navKey applies standard list navigation keys and ignores others.
func (c *cursor) navKey(keys []string, key string, page int) {
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
	}
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

func sortLabel(key sortx.Key, reverse bool) string {
	if reverse {
		return fmt.Sprintf("sort: %s (reversed)", key)
	}
	return "sort: " + string(key)
}

// programOptions are the Bubble Tea options shared by the TUI and dn stats.
func programOptions(ctx context.Context) []tea.ProgramOption {
	opts := []tea.ProgramOption{tea.WithContext(ctx)}
	if render.ColorDisabled() {
		// Ascii keeps bold and reverse, so the selected row stays visible.
		opts = append(opts, tea.WithColorProfile(colorprofile.Ascii))
	}
	return opts
}

func tick(msg tea.Msg) tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return msg })
}
