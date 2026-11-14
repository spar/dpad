package tui

import (
	"cmp"
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/moby/moby/api/types/container"

	"dockernext/internal/docker"
	"dockernext/internal/render"
	"dockernext/internal/sortx"
)

const (
	composeProjectLabel = "com.docker.compose.project"
	composeServiceLabel = "com.docker.compose.service"
)

type composeProject struct {
	name               string
	containers         []container.Summary
	running, unhealthy int
}

func (p composeProject) state() string {
	if p.running == len(p.containers) {
		return "running"
	}
	if p.running == 0 {
		return "inactive"
	}
	return "partial"
}

// ProjectsView shares the Containers tab's live snapshot and event subscription.
type ProjectsView struct {
	list[composeProject]
	collapsed map[string]bool
	snapshot  []projectContainerSnapshot
}

type projectContainerSnapshot struct {
	id, name, project, service, image, status string
	state                                     container.ContainerState
	health                                    container.HealthStatus
}

func NewProjectsView() *ProjectsView {
	v := &ProjectsView{collapsed: make(map[string]bool)}
	v.list = list[composeProject]{
		columns: []render.Column{{Header: "PROJECT / SERVICE", Max: 35}, {Header: "CONTAINER", Max: 35}, {Header: "STATE"}, {Header: "HEALTH"}, {Header: "RUNNING"}},
		keys:    []sortx.Key{sortx.Name, sortx.Status},
		sortCol: map[sortx.Key]int{sortx.Name: 0, sortx.Status: 2},
		key:     sortx.Name,
		match: func(p composeProject, filter string) bool {
			if matches(filter, p.name) {
				return true
			}
			for _, c := range p.containers {
				if matches(filter, c.Labels[composeServiceLabel], docker.ContainerName(c.Names), c.ID, c.Image, string(c.State)) {
					return true
				}
			}
			return false
		},
		sort: func(items []composeProject, key sortx.Key, reverse bool) {
			slices.SortFunc(items, func(a, b composeProject) int {
				order := 0
				if key == sortx.Status {
					order = cmp.Compare(a.state(), b.state())
				}
				order = cmp.Or(order, cmp.Compare(a.name, b.name))
				if reverse {
					return -order
				}
				return order
			})
		},
		rows:    v.projectRows,
		loading: "loading…",
		empty:   "no Compose projects",
	}
	return v
}

func (v *ProjectsView) projectRows(p composeProject) []row {
	marker := "▾ "
	if v.collapsed[p.name] {
		marker = "▸ "
	}
	health := ""
	if p.unhealthy > 0 {
		health = render.Error.Render(fmt.Sprintf("%d unhealthy", p.unhealthy))
	}
	rows := []row{{key: "project:" + p.name, cells: []string{
		render.Bold.Render(marker + p.name), fmt.Sprintf("%d containers", len(p.containers)),
		p.state(), health, fmt.Sprintf("%d/%d", p.running, len(p.containers)),
	}}}
	if v.collapsed[p.name] {
		return rows
	}
	for _, c := range p.containers {
		name := docker.ContainerName(c.Names)
		service := c.Labels[composeServiceLabel]
		if service == "" {
			service = "(no service)"
		}
		health := ""
		if c.Health != nil {
			health = render.HealthBadge(string(c.Health.Status))
		}
		rows = append(rows, row{key: c.ID, id: c.ID, name: name, cells: []string{
			"  " + service, name, render.StateStyle(string(c.State), render.ExitCode(c.Status)).Render(string(c.State)), health, "",
		}})
	}
	return rows
}

func (v *ProjectsView) Title() string  { return "Projects" }
func (v *ProjectsView) Init() tea.Cmd  { return nil }
func (v *ProjectsView) Focus() tea.Cmd { return nil }

func (v *ProjectsView) Update(msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(containersMsg); ok {
		if msg.err != nil {
			v.err, v.loaded = msg.err, true
			return nil
		}
		snapshot := make([]projectContainerSnapshot, 0, len(msg.items))
		for _, c := range msg.items {
			if c.Labels[composeProjectLabel] == "" {
				continue
			}
			health := container.HealthStatus("")
			if c.Health != nil {
				health = c.Health.Status
			}
			snapshot = append(snapshot, projectContainerSnapshot{c.ID, docker.ContainerName(c.Names), c.Labels[composeProjectLabel], c.Labels[composeServiceLabel], c.Image, c.Status, c.State, health})
		}
		if v.loaded && slices.Equal(snapshot, v.snapshot) {
			v.err = nil
			return nil
		}
		v.snapshot = snapshot
		projects := make(map[string]*composeProject)
		for _, c := range msg.items {
			name := c.Labels[composeProjectLabel]
			if name == "" {
				continue
			}
			p := projects[name]
			if p == nil {
				p = &composeProject{name: name}
				projects[name] = p
			}
			p.containers = append(p.containers, c)
			if c.State == container.StateRunning {
				p.running++
			}
			if c.Health != nil && c.Health.Status == container.Unhealthy {
				p.unhealthy++
			}
		}
		items := make([]composeProject, 0, len(projects))
		for _, p := range projects {
			slices.SortFunc(p.containers, func(a, b container.Summary) int {
				return cmp.Or(cmp.Compare(a.Labels[composeServiceLabel], b.Labels[composeServiceLabel]), cmp.Compare(docker.ContainerName(a.Names), docker.ContainerName(b.Names)), cmp.Compare(a.ID, b.ID))
			})
			items = append(items, *p)
		}
		for name := range v.collapsed {
			if projects[name] == nil {
				delete(v.collapsed, name)
			}
		}
		v.setItems(items, msg.err)
	}
	return nil
}

func (v *ProjectsView) selectedProject() *composeProject {
	rows, keys := v.visible()
	if len(rows) == 0 {
		return nil
	}
	key := rows[v.cur.index(keys)].key
	for i := range v.items {
		if key == "project:"+v.items[i].name {
			return &v.items[i]
		}
	}
	return nil
}

func (v *ProjectsView) HandleKey(key string) tea.Cmd {
	if key == "space" {
		if p := v.selectedProject(); p != nil {
			v.collapsed[p.name] = !v.collapsed[p.name]
			v.cur.key = "project:" + p.name
			v.rowCache = nil
		}
		return nil
	}
	return v.list.HandleKey(key)
}

func (v *ProjectsView) Status() string {
	containers, running := 0, 0
	for _, p := range v.items {
		containers += len(p.containers)
		running += p.running
	}
	return fmt.Sprintf("%d projects · %d containers · %d running · %s", len(v.items), containers, running, sortLabel(v.key, v.reverse))
}

func (v *ProjectsView) Help() [][2]string {
	return append(v.list.Help(), [2]string{"space", "expand/collapse selected project"}, [2]string{"enter", "follow selected project's or container's logs"}, [2]string{"S/x/R on project", "preview start/stop/restart of existing project containers"}, [2]string{"u / U", "preview Compose image update for selected/all discovered projects"})
}
