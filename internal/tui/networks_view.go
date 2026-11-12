package tui

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"

	"dockernext/internal/docker"
	"dockernext/internal/render"
	"dockernext/internal/sortx"
)

type networksMsg struct {
	v     *NetworksView
	items []network.Summary
	err   error
}

// NetworksView is read-only and shares the dashboard's container lookup.
type NetworksView struct {
	list[network.Summary]
	ctx                         context.Context
	client                      dockerClient
	requested, loading, pending bool
	containers                  []container.Summary
	usage                       map[string][]docker.NetworkUse
	usageLoaded                 bool
	usageErr                    error
}

func NewNetworksView(ctx context.Context, c dockerClient) *NetworksView {
	v := &NetworksView{ctx: ctx, client: c}
	v.list = list[network.Summary]{
		keys: []sortx.Key{sortx.Name}, key: sortx.Name, sortCol: map[sortx.Key]int{sortx.Name: 0},
		match: func(n network.Summary, f string) bool {
			if matches(f, n.ID, n.Name, n.Driver, n.Scope, n.Labels[composeProjectLabel]) {
				return true
			}
			for _, u := range v.usage[n.ID] {
				if matches(f, u.Name, u.ID) {
					return true
				}
			}
			return false
		},
		sort: func(items []network.Summary, _ sortx.Key, reverse bool) {
			slices.SortFunc(items, func(a, b network.Summary) int {
				if reverse {
					return cmp.Compare(b.Name, a.Name)
				}
				return cmp.Compare(a.Name, b.Name)
			})
		},
		rows: func(n network.Summary) []row {
			usage := render.NetworkUsage(v.usage[n.ID], v.usageKnown())
			cells := []string{n.Name, n.Driver, n.Scope, n.Labels[composeProjectLabel], usage}
			if v.width < 100 {
				cells = []string{n.Name, n.Driver, usage}
			}
			return []row{{key: n.ID, name: n.Name, cells: cells}}
		}, loading: "loading networks…", empty: "no networks match",
	}
	v.SetSize(120, 28)
	return v
}

func (v *NetworksView) SetSize(width, height int) {
	v.list.SetSize(width, height)
	if width < 100 {
		v.columns = []render.Column{{Header: "NAME", Max: 28}, {Header: "DRIVER", Max: 12}, {Header: "ATTACHED / CONFIGURED"}}
	} else {
		v.columns = []render.Column{{Header: "NAME", Max: 32}, {Header: "DRIVER", Max: 12}, {Header: "SCOPE", Max: 8}, {Header: "PROJECT", Max: 20}, {Header: "ATTACHED / CONFIGURED"}}
	}
	v.rowCache = nil
}
func (v *NetworksView) Title() string { return "Networks" }
func (v *NetworksView) Init() tea.Cmd { return nil }
func (v *NetworksView) Focus() tea.Cmd {
	if !v.requested {
		v.requested = true
		v.indexUsage()
	}
	return v.load()
}
func (v *NetworksView) usageKnown() bool { return v.usageLoaded && v.usageErr == nil }
func (v *NetworksView) indexUsage() {
	clear(v.usage)
	if v.requested && v.usageKnown() {
		v.usage = docker.NetworkUsers(v.containers, v.items)
	}
	v.rowCache = nil
}
func (v *NetworksView) load() tea.Cmd {
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
		items, err := v.client.Networks(ctx)
		return networksMsg{v: v, items: items, err: err}
	}
}
func (v *NetworksView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case networksMsg:
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
			v.indexUsage()
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
func (v *NetworksView) SelectedNetwork() (string, string, bool) {
	if !v.loaded || v.err != nil {
		return "", "", false
	}
	rows, keys := v.visible()
	if len(rows) == 0 {
		return "", "", false
	}
	r := rows[v.cur.index(keys)]
	return r.key, r.name, true
}
func (v *NetworksView) Status() string {
	s := fmt.Sprintf("%d networks · %s", len(v.items), sortLabel(v.key, v.reverse))
	if !v.usageKnown() {
		s += " · network attachments unknown"
	}
	if v.usageErr != nil {
		s += ": " + connectionError(v.usageErr)
	}
	return s
}
func (v *NetworksView) Help() [][2]string {
	return append(v.list.Help(), [2]string{"d / enter", "inspect network configuration and endpoints"}, [2]string{"/", "filter by network name/ID, driver, scope, project, or configured container"})
}
