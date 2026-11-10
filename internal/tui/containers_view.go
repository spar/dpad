package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
	containersTickMsg  struct{}
	containersWatchMsg struct {
		ch  <-chan struct{}
		err error
	}
	containersChangedMsg struct{ ch <-chan struct{} }
	// refreshMsg asks list tabs to reload now (e.g. after start/stop).
	refreshMsg struct{}
)

// ContainersView lists all containers with state colors.
type ContainersView struct {
	list[container.Summary]
	ctx    context.Context
	client dockerClient
}

func NewContainersView(ctx context.Context, c dockerClient) *ContainersView {
	return &ContainersView{ctx: ctx, client: c, list: list[container.Summary]{
		columns: render.ContainerColumns,
		keys:    sortx.ContainerKeys,
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
	return func() tea.Msg {
		items, err := v.client.Containers(v.ctx, true)
		return containersMsg{items, err}
	}
}

// Init follows daemon events, which also trigger the first load.
func (v *ContainersView) Init() tea.Cmd { return tea.Batch(v.watch(), tick(containersTickMsg{})) }

func (v *ContainersView) watch() tea.Cmd {
	return func() tea.Msg {
		ch, err := v.client.ContainerChanges(v.ctx)
		return containersWatchMsg{ch, err}
	}
}

// changeDebounce gives a burst of events (e.g. compose up) time to finish
// before reloading.
const changeDebounce = 100 * time.Millisecond

func waitChange(ch <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		if _, ok := <-ch; !ok {
			return nil
		}
		time.Sleep(changeDebounce)
		select {
		case <-ch:
		default:
		}
		return containersChangedMsg{ch}
	}
}

func (v *ContainersView) Focus() tea.Cmd { return nil }

func (v *ContainersView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case containersMsg:
		v.setItems(msg.items, msg.err)
	case containersWatchMsg:
		if msg.err != nil {
			return v.load() // keep going on the poll alone
		}
		return waitChange(msg.ch)
	case containersChangedMsg:
		return tea.Batch(v.load(), waitChange(msg.ch))
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
