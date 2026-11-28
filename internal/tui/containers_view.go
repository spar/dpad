package tui

import (
	"context"
	"fmt"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/moby/moby/api/types/container"

	"github.com/spar/dpad/internal/docker"
	"github.com/spar/dpad/internal/render"
	"github.com/spar/dpad/internal/sortx"
)

type (
	containersMsg struct {
		items []container.Summary
		err   error
	}
	containersTickMsg         struct{}
	containersEventRefreshMsg struct{ generation uint64 }
	// refreshMsg asks list tabs to reload now (e.g. after start/stop).
	refreshMsg struct{}
)

// ContainersView lists all containers with state colors.
type ContainersView struct {
	list[container.Summary]
	loader
	ctx             context.Context
	client          dockerClient
	eventPending    bool
	eventGeneration uint64
	// usage supplies the CPU and memory columns from the Stats tab's stream;
	// graph is the CPU graph style, or render.GraphOff to hide both.
	usage *StatsView
	graph string
	// cells caches each container's columns other than CPU and MEM, so a
	// stats tick only draws those two; it is reset with the item list and
	// the theme.
	cells      map[string][]string
	cellsTheme int
}

func NewContainersView(ctx context.Context, c dockerClient) *ContainersView {
	v := &ContainersView{ctx: ctx, client: c, graph: render.GraphOff, list: list[container.Summary]{
		columns: render.ContainerColumns,
		keys:    sortx.ContainerKeys,
		sortCol: map[sortx.Key]int{sortx.Name: 1, sortx.Status: 3},
		key:     sortx.Status,
		match: func(c container.Summary, f string) bool {
			return matches(f, docker.ContainerName(c.Names), c.Image, c.ID, string(c.State))
		},
		sort:    sortx.Containers,
		loading: "loading…",
		empty:   "no containers",
	}}
	v.rows = v.containerRows
	return v
}

func (v *ContainersView) containerRows(c container.Summary) []row {
	if v.cells == nil || v.cellsTheme != render.ThemeGeneration() {
		v.cells, v.cellsTheme = map[string][]string{}, render.ThemeGeneration()
	}
	cells, ok := v.cells[c.ID]
	if !ok {
		cells = render.ContainerRow(c)
		cells[1] = render.ImageIcon(c.Image) + cells[1]
		v.cells[c.ID] = cells
	}
	if v.showUsage() {
		cpu, mem := v.usageCells(c.ID, time.Now())
		cells = slices.Insert(slices.Clip(cells), len(cells)-1, cpu, mem)
	}
	return []row{{key: c.ID, cells: cells, id: c.ID, name: docker.ContainerName(c.Names)}}
}

func (v *ContainersView) showUsage() bool { return v.usage != nil && v.graph != render.GraphOff }

func (v *ContainersView) usageCells(id string, now time.Time) (string, string) {
	s, ok := v.usage.data[id]
	h := v.usage.history[id]
	if !ok || h == nil || v.usage.health[id].err != nil {
		return "", ""
	}
	cpu, present := h.recent(h.endSecond(now), render.GraphSamples)
	return render.CPUGraph(v.graph, cpu, present), render.CompactSize(s.MemUsage)
}

// setGraph switches the CPU graph style; watchUsage starts the stream it needs.
func (v *ContainersView) setGraph(style string) {
	v.graph = style
	v.columns = render.ContainerColumns
	if v.showUsage() {
		v.columns = render.ContainerUsageColumns
	}
	v.rowCache = nil
}

func (v *ContainersView) watchUsage() tea.Cmd {
	if !v.showUsage() {
		return nil
	}
	return v.usage.Focus()
}

func (v *ContainersView) Title() string { return "Containers" }

func (v *ContainersView) Help() [][2]string {
	return append(v.list.Help(), [2]string{"t", "show or hide the host panel: the host's and the containers' CPU and memory"})
}

func (v *ContainersView) load() tea.Cmd {
	v.eventPending = false
	v.eventGeneration++

	if !v.begin() {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(v.ctx, 5*time.Second)
		defer cancel()
		items, err := v.client.Containers(ctx, true)
		return containersMsg{items, err}
	}
}

// Event batches from EventsView also refresh this list, sharing its subscription.
func (v *ContainersView) Init() tea.Cmd { return tea.Batch(v.load(), tick(containersTickMsg{})) }

func (v *ContainersView) Focus() tea.Cmd { return v.watchUsage() }

func (v *ContainersView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case containersMsg:
		again := v.end()
		v.cells = nil
		v.setItems(msg.items, msg.err)
		if again {
			return v.load()
		}
	case eventsBatchMsg:
		for _, u := range msg.updates {
			if u.Subscribed {
				return v.load()
			}
		}
		if !v.eventPending {
			for _, u := range msg.updates {
				if u.Event.ID != "" {
					v.eventPending = true
					v.eventGeneration++
					gen := v.eventGeneration
					return tea.Tick(time.Second, func(time.Time) tea.Msg { return containersEventRefreshMsg{generation: gen} })
				}
			}
		}
	case containersEventRefreshMsg:
		if v.eventPending && msg.generation == v.eventGeneration && v.ctx.Err() == nil {
			return v.load()
		}
	case containersTickMsg:
		return tea.Batch(v.load(), tick(containersTickMsg{}))
	case refreshMsg:
		return v.load()
	case statsSourceBatchMsg, statsTickMsg, statsStartedMsg, statsClosedMsg, statsErrMsg:
		// Redraw with the samples StatsView takes from the same message.
		if v.showUsage() {
			v.rowCache = nil
		}
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
	status := fmt.Sprintf("%d containers · %d running", len(v.items), running)
	if v.showUsage() && len(v.usage.data) > 0 {
		var cpu float64
		var mem uint64
		for id, s := range v.usage.data {
			if v.usage.health[id].err == nil {
				cpu += s.CPUPercent
				mem += s.MemUsage
			}
		}
		status += fmt.Sprintf(" · CPU %.1f%% · MEM %s", cpu, render.BinSize(mem))
	}
	return status + " · " + sortLabel(v.key, v.reverse)
}
