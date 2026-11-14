package tui

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

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
	detailView
	requestCancel context.CancelFunc
	client        dockerClient
	id, name      string
	info          network.Inspect
	endpoints     list[networkEndpoint]
	showEndpoints bool
	width, height int
	loaded        bool
}

func NewNetworkDetailsView(ctx context.Context, c dockerClient, id, name string, width, height int) (*NetworkDetailsView, tea.Cmd) {
	v := &NetworkDetailsView{detailView: newDetailView(ctx, width, height, "loading network details…"), client: c, id: id, name: name}
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
	gen := v.startLoad()
	return func() tea.Msg {
		defer cancel()
		info, err := v.client.Network(ctx, v.id)
		return networkDetailsMsg{v: v, gen: gen, info: info, err: err}
	}
}
func (v *NetworkDetailsView) SetSize(width, height int) {
	v.width, v.height = width, height
	v.detailView.SetSize(width, height)
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
	}
	return v.updateViewport(msg)
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
	mode := "metadata"
	if v.showEndpoints {
		mode = "endpoints"
	}
	return v.status("network: "+render.Accent.Render(v.name), fmt.Sprintf(" · %s snapshot · %d endpoints", mode, len(v.info.Containers)))
}
