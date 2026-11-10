package tui

import (
	"context"
	"fmt"

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
	containersTickMsg struct{}
	// refreshMsg asks list tabs to reload now (e.g. after start/stop).
	refreshMsg struct{}
)

// ContainersView lists all containers with state colors.
type ContainersView struct {
	ctx     context.Context
	client  *docker.Client
	items   []container.Summary
	key     string
	reverse bool
	cur     cursor
	filter  string
	page    int
	loaded  bool
	err     error
}

func NewContainersView(ctx context.Context, c *docker.Client) *ContainersView {
	return &ContainersView{ctx: ctx, client: c, key: "status", page: 10}
}

func (v *ContainersView) Title() string { return "Containers" }

func (v *ContainersView) load() tea.Cmd {
	return func() tea.Msg {
		items, err := v.client.Containers(v.ctx, true)
		return containersMsg{items, err}
	}
}

func (v *ContainersView) Init() tea.Cmd { return tea.Batch(v.load(), tick(containersTickMsg{})) }

func (v *ContainersView) Focus() tea.Cmd { return nil }

func (v *ContainersView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case containersMsg:
		v.items, v.err, v.loaded = msg.items, msg.err, true
	case containersTickMsg:
		return tea.Batch(v.load(), tick(containersTickMsg{}))
	case refreshMsg:
		return v.load()
	}
	return nil
}

func (v *ContainersView) visible(filter string) []container.Summary {
	var out []container.Summary
	for _, c := range v.items {
		if matches(filter, docker.ContainerName(c.Names), c.Image, c.ID, string(c.State)) {
			out = append(out, c)
		}
	}
	sortx.Containers(out, v.key, v.reverse)
	return out
}

func containerIDs(items []container.Summary) []string {
	ids := make([]string, len(items))
	for i, c := range items {
		ids[i] = c.ID
	}
	return ids
}

func (v *ContainersView) HandleKey(key string) tea.Cmd {
	switch key {
	case "s":
		v.key = sortx.Next(v.key, sortx.ContainerKeys)
	case "r":
		v.reverse = !v.reverse
	default:
		v.cur.navKey(containerIDs(v.visible(v.filter)), key, v.page)
	}
	return nil
}

func (v *ContainersView) View(width, height int, filter string) string {
	switch {
	case v.err != nil:
		return render.Error.Render("error: " + v.err.Error())
	case !v.loaded:
		return render.Dim.Render("loading…")
	}
	v.filter = filter
	v.page = max(height-2, 1)
	items := v.visible(filter)
	if len(items) == 0 {
		return render.Dim.Render("no containers")
	}
	rows := make([][]string, len(items))
	for i, c := range items {
		rows[i] = render.ContainerRow(c)
	}
	t := render.Table{Columns: render.ContainerColumns, Rows: rows, Width: width}
	return t.Window(v.cur.index(containerIDs(items)), height)
}

func (v *ContainersView) Selected() (string, string, bool) {
	items := v.visible(v.filter)
	if len(items) == 0 {
		return "", "", false
	}
	c := items[v.cur.index(containerIDs(items))]
	return c.ID, docker.ContainerName(c.Names), true
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

func (v *ContainersView) Help() [][2]string {
	return [][2]string{
		{"s", "cycle sort: name / created / status"},
		{"r", "reverse sort"},
	}
}
