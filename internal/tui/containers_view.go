package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/moby/moby/api/types/container"

	"dockernext/internal/docker"
	"dockernext/internal/render"
	"dockernext/internal/sortx"
)

type (
	containersMsg struct {
		items []container.Summary
		err   error
	}
	containersTickMsg struct{}
	// refreshMsg asks list tabs to reload now (e.g. after start/stop).
	refreshMsg struct{}
)

// ContainersView lists all containers with state colors.
type ContainersView struct {
	list[container.Summary]
	ctx              context.Context
	client           dockerClient
	loading, pending bool
}

func NewContainersView(ctx context.Context, c dockerClient) *ContainersView {
	return &ContainersView{ctx: ctx, client: c, list: list[container.Summary]{
		columns: render.ContainerColumns,
		keys:    sortx.ContainerKeys,
		sortCol: map[sortx.Key]int{sortx.Name: 1, sortx.Status: 3},
		key:     sortx.Status,
		match: func(c container.Summary, f string) bool {
			return matches(f, docker.ContainerName(c.Names), c.Image, c.ID, string(c.State))
		},
		sort: sortx.Containers,
		rows: func(c container.Summary) []row {
			name := docker.ContainerName(c.Names)
			return []row{{key: c.ID, cells: render.ContainerRow(c), id: c.ID, name: name}}
		},
		loading: "loading…",
		empty:   "no containers",
	}}
}

func (v *ContainersView) Title() string { return "Containers" }

func (v *ContainersView) load() tea.Cmd {
	if v.loading {
		v.pending = true
		return nil
	}
	v.loading = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(v.ctx, 5*time.Second)
		defer cancel()
		items, err := v.client.Containers(ctx, true)
		return containersMsg{items, err}
	}
}

// Event batches from EventsView also refresh this list, sharing its subscription.
func (v *ContainersView) Init() tea.Cmd { return tea.Batch(v.load(), tick(containersTickMsg{})) }

func (v *ContainersView) Focus() tea.Cmd { return nil }

func (v *ContainersView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case containersMsg:
		v.loading = false
		v.setItems(msg.items, msg.err)
		if v.pending {
			v.pending = false
			return v.load()
		}
	case eventsBatchMsg:
		for _, u := range msg.updates {
			if u.Subscribed || u.Event.ID != "" {
				return v.load()
			}
		}
	case containersTickMsg:
		return tea.Batch(v.load(), tick(containersTickMsg{}))
	case refreshMsg:
		return v.load()
	}
	return nil
}

func (v *ContainersView) Status() string {
	running := 0
	for _, c := range v.items {
		if c.State == container.StateRunning {
			running++
		}
	}
	return fmt.Sprintf("%d containers · %d running · %s", len(v.items), running, sortLabel(v.key, v.reverse))
}
