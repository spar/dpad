package tui

import (
	"cmp"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"

	"dpad/internal/docker"
	"dpad/internal/render"
	"dpad/internal/sortx"
)

const (
	composeProjectLabel = docker.ComposeProjectLabel
	composeServiceLabel = docker.ComposeServiceLabel
)

type composeProject struct {
	name               string
	containers         []container.Summary
	running, unhealthy int
	dir                string
	files, envFiles    []string
	created            int64    // the oldest container's creation time
	missing            []string // Compose files gone from this machine
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
	// sshHost names the SSH host the daemon runs on; Compose paths are there.
	sshHost string
	// localFiles is set when the daemon shares this machine's files, so a
	// missing Compose file can be reported.
	localFiles bool
	home       string
}

type projectContainerSnapshot struct {
	id, name, project, service, image, status string
	state                                     container.ContainerState
	health                                    container.HealthStatus
}

func NewProjectsView() *ProjectsView {
	v := &ProjectsView{collapsed: make(map[string]bool)}
	v.list = list[composeProject]{
		columns: []render.Column{{Header: "PROJECT / SERVICE", Max: 35, Min: 16}, {Header: "CONTAINER", Max: 35, Min: 16}, {Header: "STATE"}, {Header: "HEALTH"}, {Header: "RUNNING"}},
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
				p = &composeProject{name: name, dir: c.Labels[docker.ComposeWorkingDirLabel], created: c.Created,
					files: splitLabel(c.Labels[docker.ComposeConfigLabel]), envFiles: splitLabel(c.Labels[composeProjectLabel+".environment_file"])}
				for _, f := range p.files {
					if v.localFiles {
						if _, err := os.Stat(f); err != nil {
							p.missing = append(p.missing, f)
						}
					}
				}
				projects[name] = p
			}
			if c.Created < p.created {
				p.created = c.Created
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

// cursorProject is the project of the highlighted row, a header or a container.
func (v *ProjectsView) cursorProject() *composeProject {
	if !v.loaded || v.err != nil {
		return nil
	}
	rows, keys := v.visible()
	if len(rows) == 0 {
		return nil
	}
	key := rows[v.cur.index(keys)].key
	for i, p := range v.items {
		if key == "project:"+p.name || slices.ContainsFunc(p.containers, func(c container.Summary) bool { return c.ID == key }) {
			return &v.items[i]
		}
	}
	return nil
}

// infoLines is the height of the strip under the list.
const infoLines = 3

func (v *ProjectsView) SetSize(width, height int) {
	v.list.SetSize(width, height)
	v.page = max(height-2-infoLines, 1)
}

// View adds a strip under the list that says where the highlighted project's
// Compose files are, since nothing else in the list does.
func (v *ProjectsView) View() string {
	p := v.cursorProject()
	if p == nil || v.height < 4*infoLines {
		return v.list.View()
	}
	body := v.view(v.height - infoLines)
	if pad := v.height - infoLines - lipgloss.Height(body); pad > 0 {
		body += strings.Repeat("\n", pad)
	}
	return body + "\n" + v.infoStrip(*p)
}

func (v *ProjectsView) infoStrip(p composeProject) string {
	title := render.Header.Render(" " + p.name + " ")
	rule := render.Dim.Render("──") + title + render.Dim.Render(strings.Repeat("─", max(v.width-2-lipgloss.Width(title), 0)))
	label := func(s string) string { return render.Bold.Render(fmt.Sprintf(" %-5s ", s)) }

	file := label("file") + render.Dim.Render("unknown: the containers have no Compose file labels")
	for i, f := range p.files {
		name, style := v.showPath(f), render.Dim
		if i == 0 {
			file, style = label("file"), lipgloss.NewStyle()
		} else {
			name = path.Base(filepath.ToSlash(f))
			file += render.Dim.Render(" + ")
		}
		if slices.Contains(p.missing, f) {
			style = render.Warn
		}
		file += style.Render(name)
	}
	if len(p.missing) > 0 {
		file += render.Warn.Render("  ✗ not found")
	}

	facts := []string{}
	if len(p.envFiles) > 0 {
		names := make([]string, len(p.envFiles))
		for i, f := range p.envFiles {
			names[i] = path.Base(filepath.ToSlash(f))
		}
		facts = append(facts, "env "+strings.Join(names, ", "))
	}
	if p.created > 0 {
		facts = append(facts, "created "+render.Ago(p.created))
	}
	where := label("dir") + v.showPath(p.dir)
	if v.sshHost != "" {
		where = label("host") + render.Accent.Render(v.sshHost) + render.Dim.Render(" (paths are on this host)")
	}
	if len(p.missing) > 0 {
		where = label("note") + render.Dim.Render("files are gone; ") + render.Bold.Render("D") + render.Dim.Render(" down still works by project name")
		facts = facts[len(facts)-min(len(facts), 1):]
	}
	if len(facts) > 0 {
		where += render.Dim.Render(" · " + strings.Join(facts, " · "))
	}
	return strings.Join([]string{rule, ansi.Truncate(file, v.width, "…"), ansi.Truncate(where, v.width, "…")}, "\n")
}

// showPath shortens the home folder to ~ for paths on this machine.
func (v *ProjectsView) showPath(p string) string {
	if v.localFiles && v.home != "" && (p == v.home || strings.HasPrefix(p, v.home+string(filepath.Separator))) {
		return "~" + strings.TrimPrefix(p, v.home)
	}
	return p
}

func splitLabel(raw string) []string {
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
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
	return append(v.list.Help(), [2]string{"space", "expand/collapse selected project"}, [2]string{"enter", "follow selected project's or container's logs"}, [2]string{"e on project", "edit a Compose file in your editor (set in o), check it, then save and update"}, [2]string{"S/x/R/D on project", "preview start/stop/restart of its containers, or D docker compose down"}, [2]string{"u / U", "update the selected / all projects: docker compose pull, then up -d"})
}
