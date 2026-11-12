package tui

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/moby/moby/api/types/network"

	"dockernext/internal/render"
	"dockernext/internal/sortx"
)

type networkDetailsMsg struct {
	v    *NetworkDetailsView
	gen  int
	info network.Inspect
	err  error
}
type networkEndpoint struct {
	id   string
	info network.EndpointResource
}

type NetworkDetailsView struct {
	ctx                   context.Context
	cancel, requestCancel context.CancelFunc
	client                dockerClient
	id, name              string
	info                  network.Inspect
	vp                    viewport.Model
	endpoints             list[networkEndpoint]
	showEndpoints         bool
	width, height, gen    int
	loading, loaded       bool
	err                   error
}

func NewNetworkDetailsView(ctx context.Context, c dockerClient, id, name string, width, height int) (*NetworkDetailsView, tea.Cmd) {
	ctx, cancel := context.WithCancel(ctx)
	v := &NetworkDetailsView{ctx: ctx, cancel: cancel, client: c, id: id, name: name, vp: viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))}
	v.endpoints = list[networkEndpoint]{
		keys: []sortx.Key{sortx.Name}, key: sortx.Name,
		match: func(networkEndpoint, string) bool { return true },
		sort: func(items []networkEndpoint, _ sortx.Key, reverse bool) {
			slices.SortFunc(items, func(a, b networkEndpoint) int {
				c := cmp.Or(cmp.Compare(a.info.Name, b.info.Name), cmp.Compare(a.id, b.id))
				if reverse {
					return -c
				}
				return c
			})
		},
		rows: func(e networkEndpoint) []row {
			mac := e.info.MacAddress.String()
			if mac == "" {
				mac = "—"
			}
			cells := []string{e.info.Name, render.NetworkPrefix(e.info.IPv4Address), render.NetworkPrefix(e.info.IPv6Address), mac}
			if v.width < 100 {
				cells = []string{e.info.Name, render.NetworkPrefix(e.info.IPv4Address), mac}
			}
			return []row{{key: e.id, id: e.id, name: e.info.Name, cells: cells}}
		}, empty: "no container endpoints reported in this snapshot",
	}
	v.SetSize(width, height)
	v.vp.SetContent("loading network details…")
	return v, v.load()
}
func (v *NetworkDetailsView) load() tea.Cmd {
	if v.ctx.Err() != nil {
		return nil
	}
	if v.requestCancel != nil {
		v.requestCancel()
	}
	ctx, cancel := context.WithTimeout(v.ctx, 5*time.Second)
	v.requestCancel = cancel
	v.gen++
	v.loading = true
	gen := v.gen
	return func() tea.Msg {
		defer cancel()
		info, err := v.client.Network(ctx, v.id)
		return networkDetailsMsg{v: v, gen: gen, info: info, err: err}
	}
}
func (v *NetworkDetailsView) Close() { v.cancel() }
func (v *NetworkDetailsView) SetSize(width, height int) {
	v.width, v.height = width, height
	v.vp.SetWidth(width)
	v.vp.SetHeight(height)
	endpointHeight := height
	if width < 100 {
		endpointHeight = max(height-2, 1)
		v.endpoints.columns = []render.Column{{Header: "CONTAINER", Max: 28}, {Header: "IPv4", Max: 19}, {Header: "MAC", Max: 17}}
	} else {
		v.endpoints.columns = []render.Column{{Header: "CONTAINER", Max: 24}, {Header: "IPv4", Max: 19}, {Header: "IPv6", Max: 43}, {Header: "MAC", Max: 17}}
	}
	v.endpoints.SetSize(width, endpointHeight)
	v.endpoints.rowCache = nil
}
func (v *NetworkDetailsView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case networkDetailsMsg:
		if msg.v != v || msg.gen != v.gen || v.ctx.Err() != nil {
			return nil
		}
		v.loading = false
		v.loaded = true
		v.err = msg.err
		if msg.err == nil {
			v.info = msg.info
			v.vp.SetContent(render.NetworkDetails(v.info))
			items := make([]networkEndpoint, 0, len(v.info.Containers))
			for id, e := range v.info.Containers {
				items = append(items, networkEndpoint{id: id, info: e})
			}
			v.endpoints.setItems(items, nil)
		}
		return nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+r":
			return tea.Batch(v.load(), func() tea.Msg { return refreshMsg{} })
		case "tab", "shift+tab":
			v.showEndpoints = !v.showEndpoints
			return nil
		}
		if v.showEndpoints {
			return v.endpoints.HandleKey(msg.String())
		}
		switch msg.String() {
		case "g", "home":
			v.vp.GotoTop()
			return nil
		case "G", "end":
			v.vp.GotoBottom()
			return nil
		}
	}
	var cmd tea.Cmd
	v.vp, cmd = v.vp.Update(msg)
	return cmd
}
func (v *NetworkDetailsView) SelectedEndpoint() (string, string, bool) {
	if !v.showEndpoints || !v.loaded || v.loading || v.err != nil {
		return "", "", false
	}
	return v.endpoints.Selected()
}
func (v *NetworkDetailsView) View() string {
	if v.err != nil {
		return render.Error.Render("error: " + connectionError(v.err))
	}
	if !v.loaded {
		return render.Dim.Render("loading network details…")
	}
	if !v.showEndpoints {
		return v.vp.View()
	}
	body := v.endpoints.View()
	if v.width < 100 {
		if id, _, ok := v.endpoints.Selected(); ok {
			body += "\n\nIPv6: " + render.NetworkPrefix(v.info.Containers[id].IPv6Address)
		}
	}
	return body
}
func (v *NetworkDetailsView) Status() string {
	s := "network: " + render.Accent.Render(v.name)
	if v.loading {
		return s + " · loading…"
	}
	if v.err != nil {
		return s + " · inspect failed (ctrl+r to retry)"
	}
	mode := "metadata"
	if v.showEndpoints {
		mode = "endpoints"
	}
	return fmt.Sprintf("%s · %s snapshot · %d endpoints", s, mode, len(v.info.Containers))
}
func (a *App) openSelectedNetwork() tea.Cmd {
	source, ok := a.tabs[a.active].(*NetworksView)
	if !ok {
		return nil
	}
	id, name, ok := source.SelectedNetwork()
	if !ok {
		return nil
	}
	var cmd tea.Cmd
	a.networkDetails, cmd = NewNetworkDetailsView(a.ctx, a.client, id, name, a.width, a.bodyHeight())
	return cmd
}
