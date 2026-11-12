package tui

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/volume"

	"dockernext/internal/docker"
	"dockernext/internal/render"
	"dockernext/internal/sortx"
)

type volumesMsg struct {
	v        *VolumesView
	items    []volume.Volume
	warnings []string
	err      error
}

// VolumesView is read-only and shares the dashboard's container lookup.
type VolumesView struct {
	list[volume.Volume]
	ctx                         context.Context
	client                      dockerClient
	requested, loading, pending bool
	containers                  []container.Summary
	usage                       map[string][]docker.VolumeUse
	usageLoaded                 bool
	usageErr                    error
	warnings                    []string
}

func NewVolumesView(ctx context.Context, c dockerClient) *VolumesView {
	v := &VolumesView{ctx: ctx, client: c}
	v.list = list[volume.Volume]{
		keys: []sortx.Key{sortx.Name}, key: sortx.Name, sortCol: map[sortx.Key]int{sortx.Name: 0},
		match: func(vol volume.Volume, f string) bool {
			if matches(f, vol.Name, vol.Driver, vol.Labels[composeProjectLabel]) {
				return true
			}
			for _, u := range v.usage[vol.Name] {
				if matches(f, u.Name, u.ID) {
					return true
				}
			}
			return false
		},
		sort: func(items []volume.Volume, _ sortx.Key, reverse bool) {
			slices.SortFunc(items, func(a, b volume.Volume) int {
				if reverse {
					return cmp.Compare(b.Name, a.Name)
				}
				return cmp.Compare(a.Name, b.Name)
			})
		},
		rows: func(vol volume.Volume) []row {
			usage := render.VolumeUsage(v.usage[vol.Name], v.usageKnown())
			cells := []string{vol.Name, vol.Driver, vol.Labels[composeProjectLabel], usage}
			if v.width < 100 {
				cells = []string{vol.Name, vol.Driver, usage}
			}
			return []row{{key: vol.Name, cells: cells}}
		}, loading: "loading volumes…", empty: "no volumes match",
	}
	v.SetSize(120, 28)
	return v
}

func (v *VolumesView) SetSize(width, height int) {
	v.list.SetSize(width, height)
	if width < 100 {
		v.columns = []render.Column{{Header: "NAME", Max: 28}, {Header: "DRIVER", Max: 12}, {Header: "USAGE"}}
	} else {
		v.columns = []render.Column{{Header: "NAME", Max: 40}, {Header: "DRIVER", Max: 16}, {Header: "PROJECT", Max: 24}, {Header: "USAGE"}}
	}
	v.rowCache = nil
}
func (v *VolumesView) Title() string { return "Volumes" }
func (v *VolumesView) Init() tea.Cmd { return nil }
func (v *VolumesView) Focus() tea.Cmd {
	if !v.requested {
		v.requested = true
		v.indexUsage()
	}
	return v.load()
}
func (v *VolumesView) usageKnown() bool { return v.usageLoaded && v.usageErr == nil }
func (v *VolumesView) indexUsage() {
	clear(v.usage)
	if v.requested && v.usageKnown() {
		v.usage = docker.VolumeUsers(v.containers)
	}
	v.rowCache = nil
}
func (v *VolumesView) load() tea.Cmd {
	if !v.requested || v.ctx.Err() != nil {
		return nil
	}
	if v.loading {
		v.pending = true
		return nil
	}
	v.loading = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(v.ctx, 5*time.Second)
		defer cancel()
		items, warnings, err := v.client.Volumes(ctx)
		return volumesMsg{v: v, items: items, warnings: warnings, err: err}
	}
}
func (v *VolumesView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case volumesMsg:
		if msg.v != v || v.ctx.Err() != nil {
			return nil
		}
		v.loading = false
		if msg.err != nil {
			v.err = msg.err
			v.loaded = true
		} else {
			_, oldKeys := v.visible()
			selected := v.cur.index(oldKeys)
			v.setItems(msg.items, nil)
			v.warnings = msg.warnings
			_, keys := v.visible()
			if len(keys) > 0 && !slices.Contains(keys, v.cur.key) {
				v.cur.key = keys[min(selected, len(keys)-1)]
			}
		}
		if v.pending {
			v.pending = false
			return v.load()
		}
	case containersMsg:
		v.containers = msg.items
		v.usageLoaded = true
		v.usageErr = msg.err
		v.indexUsage()
	case refreshMsg:
		return v.load()
	}
	return nil
}
func (v *VolumesView) SelectedVolume() (string, bool) {
	if !v.loaded || v.err != nil {
		return "", false
	}
	rows, keys := v.visible()
	if len(rows) == 0 {
		return "", false
	}
	return rows[v.cur.index(keys)].key, true
}
func (v *VolumesView) Status() string {
	s := fmt.Sprintf("%d volumes · %s", len(v.items), sortLabel(v.key, v.reverse))
	if !v.usageKnown() {
		s += " · container usage unknown"
	}
	if v.usageErr != nil {
		s += ": " + connectionError(v.usageErr)
	}
	if len(v.warnings) > 0 {
		s += " · warning: " + strings.Join(strings.Fields(strings.Join(v.warnings, "; ")), " ")
	}
	return s
}
func (v *VolumesView) Help() [][2]string {
	return append(v.list.Help(), [2]string{"d / enter", "inspect volume metadata and container mount destinations"}, [2]string{"/", "filter by volume, driver, Compose project, or referencing container"})
}
