package tui

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"dockernext/internal/compose"
	"dockernext/internal/render"
	"dockernext/internal/sortx"
)

type composePlanMsg struct {
	view     *composeUpdateView
	projects []compose.Project
	checking bool
	err      error
}

type composeEventsMsg struct {
	view   *composeUpdateView
	events []compose.Event
	done   bool
}

type composeUpdateRow struct {
	project       compose.Project
	state, output string
}

type composeUpdateView struct {
	list[composeUpdateRow]
	ctx                     context.Context
	cancel, operationCancel context.CancelFunc
	client                  dockerClient
	env                     []string
	selected, stage, note   string
	projects                []compose.Project
	events                  <-chan compose.Event
	showOutput              bool
	vp                      viewport.Model
	locations               map[string]string
}

func newComposeUpdate(ctx context.Context, c dockerClient, selected string, width, height int) (*composeUpdateView, tea.Cmd) {
	ctx, cancel := context.WithCancel(ctx)
	v := &composeUpdateView{ctx: ctx, cancel: cancel, client: c, env: c.CLIEnvironment(), selected: selected, stage: "loading", vp: viewport.New(), locations: map[string]string{}}
	v.list = list[composeUpdateRow]{
		columns: []render.Column{{Header: "PROJECT", Max: 35}, {Header: "RUN ON"}, {Header: "SERVICES"}, {Header: "RESULT"}},
		sort:    func([]composeUpdateRow, sortx.Key, bool) {},
		rows: func(r composeUpdateRow) []row {
			where := "local"
			if r.project.Remote {
				where = "SSH host"
			}
			return []row{{key: r.project.Name, cells: []string{r.project.Name, where, fmt.Sprint(len(r.project.Services)), r.state}}}
		},
		empty: "no discovered Compose projects", loaded: true,
	}
	v.SetSize(width, height)
	return v, v.plan(false)
}

func (v *composeUpdateView) plan(checking bool) tea.Cmd {
	locations := maps.Clone(v.locations)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(v.ctx, 5*time.Second)
		items, err := v.client.Containers(ctx, true)
		cancel()
		if err != nil {
			return composePlanMsg{view: v, checking: checking, err: err}
		}
		projects := compose.Discover(items, v.selected)
		for i := range projects {
			projects[i].Location = locations[projects[i].Name]
		}
		projects = compose.Prepare(v.ctx, v.env, projects)
		return composePlanMsg{view: v, projects: projects, checking: checking}
	}
}

func (v *composeUpdateView) ready() int {
	n := 0
	for _, p := range v.projects {
		if p.Issue == "" {
			n++
		}
	}
	return n
}

func (v *composeUpdateView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case composePlanMsg:
		if msg.view != v || v.ctx.Err() != nil {
			return nil
		}
		if msg.err != nil {
			v.stage, v.note = "error", connectionError(msg.err)
			return nil
		}
		same := compose.Same(v.projects, msg.projects)
		v.projects = msg.projects
		rows := make([]composeUpdateRow, len(msg.projects))
		for i, p := range msg.projects {
			state := "ready"
			if p.Issue != "" {
				state = "blocked"
			}
			rows[i] = composeUpdateRow{project: p, state: state, output: p.Issue}
		}
		v.setItems(rows, nil)
		if !msg.checking || !same || v.ready() == 0 {
			v.stage = "preview"
			if msg.checking && !same {
				v.note = "Projects or configuration changed. Review and confirm this new preview."
			}
			return nil
		}
		v.stage = "running"
		ctx, cancel := context.WithCancel(v.ctx)
		v.operationCancel = cancel
		v.events = compose.Run(ctx, v.ctx, v.env, v.projects)
		return v.waitEvents()
	case composeEventsMsg:
		if msg.view != v || v.ctx.Err() != nil {
			return nil
		}
		for _, event := range msg.events {
			for i := range v.items {
				if v.items[i].project.Name == event.Project {
					v.items[i].state, v.items[i].output = event.State, event.Output
					break
				}
			}
		}
		v.rowCache = nil
		if v.showOutput {
			v.updateOutput(false)
		}
		if msg.done {
			v.stage = "done"
			v.operationCancel()
			return func() tea.Msg { return refreshMsg{} }
		}
		return v.waitEvents()
	}
	return nil
}

func (v *composeUpdateView) waitEvents() tea.Cmd {
	return func() tea.Msg {
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		batch := []compose.Event{}
		for {
			select {
			case <-v.ctx.Done():
				return nil
			case event, ok := <-v.events:
				if !ok {
					return composeEventsMsg{view: v, events: batch, done: true}
				}
				batch = append(batch, event)
				if len(batch) == 64 {
					return composeEventsMsg{view: v, events: batch}
				}
			case <-timer.C:
				if len(batch) != 0 {
					return composeEventsMsg{view: v, events: batch}
				}
				timer.Reset(100 * time.Millisecond)
			}
		}
	}
}

func (v *composeUpdateView) selectedRow() *composeUpdateRow {
	_, keys := v.visible()
	if len(keys) == 0 {
		return nil
	}
	name := keys[v.cur.index(keys)]
	for i := range v.items {
		if v.items[i].project.Name == name {
			return &v.items[i]
		}
	}
	return nil
}

func (v *composeUpdateView) HandleKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if v.showOutput {
		if key == "esc" || key == "q" {
			v.showOutput = false
			return nil
		}
		var cmd tea.Cmd
		v.vp, cmd = v.vp.Update(msg)
		return cmd
	}
	switch key {
	case "h":
		if v.stage == "preview" && v.hasSSH() {
			if r := v.selectedRow(); r != nil {
				where := "ssh"
				if r.project.Remote {
					where = "local"
				}
				v.locations[r.project.Name] = where
				v.stage, v.note = "loading", ""
				return v.plan(false)
			}
		}
	case "enter":
		if v.selectedRow() != nil {
			v.showOutput = true
			v.updateOutput(true)
		}
	case "y", "Y":
		if v.stage == "preview" && v.ready() > 0 {
			v.stage, v.note = "checking", ""
			return v.plan(true)
		}
	case "ctrl+r":
		if v.stage == "preview" || v.stage == "error" {
			v.stage, v.note = "loading", ""
			return v.plan(false)
		}
	default:
		_, keys := v.visible()
		v.cur.navKey(keys, key, v.page)
	}
	return nil
}

func (v *composeUpdateView) updateOutput(top bool) {
	r := v.selectedRow()
	if r == nil {
		return
	}
	p := r.project
	where := "this machine"
	if p.Remote {
		where = "the bound SSH host"
	}
	content := fmt.Sprintf("Project: %s\nRun on: %s\nDirectory: %s\nCompose files:\n  %s\nEnvironment files:\n  %s\nServices: %s\nImages (configured tags):\n  %s\n\nCommands: pull → up -d --no-build --pull never\nMay recreate or start services; named volumes are retained.\n\n%s\n%s", p.Name, where, p.Directory, strings.Join(p.Files, "\n  "), strings.Join(p.EnvFiles, "\n  "), strings.Join(p.Services, ", "), strings.Join(p.Images, "\n  "), r.state, r.output)
	bottom := v.vp.AtBottom()
	v.vp.SetContent(ansi.Strip(strings.ReplaceAll(content, "\r", "\n")))
	if top {
		v.vp.GotoTop()
	} else if bottom {
		v.vp.GotoBottom()
	}
}

func (v *composeUpdateView) busy() bool { return v.stage == "running" || v.stage == "cancelling" }
func (v *composeUpdateView) Close() {
	v.cancel()
	if v.operationCancel != nil {
		v.operationCancel()
	}
}
func (v *composeUpdateView) SetSize(width, height int) {
	v.list.SetSize(width, height)
	v.page = max(height-5, 1)
	v.vp.SetWidth(width)
	v.vp.SetHeight(max(height-1, 1))
}

func (v *composeUpdateView) View() string {
	scope := v.selected
	if scope == "" {
		scope = "all discovered projects (list filter ignored)"
	}
	title := ansi.Truncate(render.Bold.Render("Update "+scope+" · pull → up -d"), v.width, "…")
	if v.showOutput {
		lines := strings.Split(title+"\n"+v.vp.View(), "\n")
		return strings.Join(lines[:min(len(lines), max(v.height, 1))], "\n")
	}
	note := v.note
	if note == "" {
		switch v.stage {
		case "loading":
			note = "Reading Compose files and checking execution hosts…"
		case "checking":
			note = "Rechecking projects and resolved configuration before starting…"
		case "running":
			note = "Updating one project at a time. A failed pull skips that project's up step."
		case "cancelling":
			note = "Cancelling remaining updates. In-flight recreation may already have applied."
		case "done":
			note = "Finished. Enter shows each project's output; return to Projects for current state."
		default:
			note = "May recreate/start services. Review paths and images with Enter; blocked projects are skipped."
		}
	}
	lines := []string{title, ansi.Truncate(render.Dim.Render(strings.Join(strings.Fields(note), " ")), v.width, "…")}
	if v.height > 2 {
		lines = append(lines, strings.Split(v.view(max(v.height-3, 1)), "\n")...)
	}
	if r := v.selectedRow(); r != nil {
		lines = append(lines, ansi.Truncate(strings.Join(strings.Fields(ansi.Strip(r.project.Directory+" · "+r.output)), " "), v.width, "…"))
	}
	return strings.Join(lines[:min(len(lines), max(v.height, 1))], "\n")
}

func (v *composeUpdateView) Status() string {
	counts := map[string]int{}
	for _, r := range v.items {
		counts[r.state]++
	}
	return fmt.Sprintf("%s · %d ready · %d succeeded · %d failed · %d blocked/skipped · %d unknown · %d cancelled", v.stage, counts["ready"], counts["succeeded"], counts["failed"], counts["blocked"]+counts["skipped"], counts["unknown"], counts["cancelled"])
}

func (v *composeUpdateView) Footer() string {
	if v.showOutput {
		return hints("esc", "overview", "↑↓/pgup/pgdn", "scroll output", "g/G", "top/bottom")
	}
	if v.busy() {
		return hints("esc", "cancel remaining", "enter", "output", "↑↓", "projects")
	}
	if v.stage == "preview" && v.ready() > 0 {
		keys := []string{"y", "confirm update", "enter", "paths/images/output", "esc", "back", "ctrl+r", "refresh preview"}
		if v.hasSSH() {
			keys = append(keys, "h", "local/SSH host")
		}
		return hints(keys...)
	}
	keys := []string{"esc", "back", "enter", "details/output", "↑↓", "projects", "ctrl+r", "refresh preview"}
	if v.stage == "preview" && v.hasSSH() {
		keys = append(keys, "h", "local/SSH host")
	}
	return hints(keys...)
}

func (v *composeUpdateView) hasSSH() bool {
	for _, entry := range v.env {
		if strings.HasPrefix(entry, "DOCKER_HOST=ssh://") {
			return true
		}
	}
	return false
}

func (a *App) openComposeUpdate(all bool) tea.Cmd {
	v, ok := a.tabs[a.active].(*ProjectsView)
	if !ok {
		return nil
	}
	if a.actionsInFlight > 0 {
		a.flash, a.flashErr = "wait for container actions to finish before updating projects", true
		return nil
	}
	if !v.loaded || v.err != nil {
		return nil
	}
	selected := ""
	if !all {
		if p := v.selectedProject(); p != nil {
			selected = p.name
		} else if id, _, ok := v.Selected(); ok {
			for _, p := range v.items {
				for _, c := range p.containers {
					if c.ID == id {
						selected = p.name
					}
				}
			}
		}
		if selected == "" {
			return nil
		}
	}
	var cmd tea.Cmd
	a.composeUpdate, cmd = newComposeUpdate(a.ctx, a.client, selected, a.width, a.bodyHeight())
	return cmd
}
