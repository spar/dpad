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
	progress      composeProgress
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
	rawOutput               bool
	followActive            bool
	activeProject           string
	outputProject           string
	vp                      viewport.Model
	locations               map[string]string
}

func newComposeUpdate(ctx context.Context, c dockerClient, selected string, width, height int) (*composeUpdateView, tea.Cmd) {
	ctx, cancel := context.WithCancel(ctx)
	v := &composeUpdateView{ctx: ctx, cancel: cancel, client: c, env: c.CLIEnvironment(), selected: selected, stage: "loading", vp: viewport.New(), locations: map[string]string{}}
	v.vp.SoftWrap = true
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
		v.showOutput, v.followActive = true, true
		for _, p := range v.projects {
			if p.Issue == "" {
				v.cur.key, v.activeProject = p.Name, p.Name
				break
			}
		}
		v.SetSize(v.width, v.height)
		v.updateOutput(true)
		ctx, cancel := context.WithCancel(v.ctx)
		v.operationCancel = cancel
		v.events = compose.Run(ctx, v.ctx, v.env, v.projects)
		return v.waitEvents()
	case composeEventsMsg:
		if msg.view != v || v.ctx.Err() != nil {
			return nil
		}
		dirty := map[int]bool{}
		for _, event := range msg.events {
			if event.State == "checking" || event.State == "pulling" || event.State == "starting" {
				v.activeProject = event.Project
			}
			for i := range v.items {
				if v.items[i].project.Name == event.Project {
					v.items[i].state, v.items[i].output = event.State, event.Output
					dirty[i] = true
					break
				}
			}
		}
		for i := range dirty {
			r := &v.items[i]
			final := r.state == "succeeded" || r.state == "failed" || r.state == "unknown" || r.state == "cancelled" || r.state == "skipped"
			r.progress.apply(r.output, final)
		}
		v.rowCache = nil
		changedProject := false
		if v.followActive && v.activeProject != "" && v.cur.key != v.activeProject {
			v.cur.key, changedProject = v.activeProject, true
		}
		if v.showOutput {
			v.updateOutput(changedProject)
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
		if !v.progressMode() && (key == "esc" || key == "q") || key == "tab" {
			v.showOutput = false
			return nil
		}
		if v.progressMode() {
			switch key {
			case "l":
				v.rawOutput = !v.rawOutput
				v.updateOutput(true)
				return nil
			case "[", "left", "]", "right":
				_, keys := v.visible()
				if len(keys) > 0 {
					delta := 1
					if key == "[" || key == "left" {
						delta = -1
					}
					v.cur.move(keys, delta)
					v.followActive = false
					v.updateOutput(true)
				}
				return nil
			case "G", "f":
				v.followActive = true
				if v.activeProject != "" {
					v.cur.key = v.activeProject
				}
				v.updateOutput(true)
				return nil
			case "end":
				v.followActive = false
				v.updateOutput(true)
				return nil
			case "g", "home":
				v.followActive = false
				v.vp.GotoTop()
				return nil
			}
		}
		previous := v.vp.YOffset()
		var cmd tea.Cmd
		v.vp, cmd = v.vp.Update(msg)
		if v.vp.YOffset() < previous {
			v.followActive = false
		}
		if v.progressMode() && v.vp.AtBottom() && v.vp.YOffset() > previous {
			v.updateOutput(true)
		}
		return cmd
	}
	switch key {
	case "tab":
		if v.progressMode() {
			v.showOutput = true
			v.updateOutput(false)
		}
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
			if v.progressMode() {
				v.followActive = v.cur.key == v.activeProject
			}
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
		previous := v.cur.key
		v.cur.navKey(keys, key, v.page)
		if v.cur.key != previous {
			v.followActive = false
		}
	}
	return nil
}

func (v *composeUpdateView) updateOutput(top bool) {
	r := v.selectedRow()
	if r == nil {
		return
	}
	p := r.project
	changed := v.outputProject != p.Name
	if v.progressMode() && !top && !changed && !v.vp.AtBottom() {
		return
	}
	v.outputProject = p.Name
	where := "this machine"
	if p.Remote {
		where = "the bound SSH host"
	}
	content := r.output
	if v.progressMode() {
		if !v.rawOutput {
			content = r.progress.render(v.width, r.state)
		} else if content == "" {
			content = "Waiting for Compose output…"
		}
	} else {
		content = fmt.Sprintf("Project: %s\nRun on: %s\nDirectory: %s\nCompose files:\n  %s\nEnvironment files:\n  %s\nServices: %s\nImages (configured tags):\n  %s\n\nCommands: pull → up -d --no-build --pull never\nMay recreate or start services; named volumes are retained.\n\n%s\n%s", p.Name, where, p.Directory, strings.Join(p.Files, "\n  "), strings.Join(p.EnvFiles, "\n  "), strings.Join(p.Services, ", "), strings.Join(p.Images, "\n  "), r.state, r.output)
	}
	bottom := v.vp.AtBottom()
	if !v.progressMode() || v.rawOutput {
		content = composeOutputText(content)
	}
	v.vp.SetContent(content)
	if top && !v.progressMode() {
		v.vp.GotoTop()
	} else if top || changed || bottom {
		v.vp.GotoBottom()
	}
}

func (v *composeUpdateView) busy() bool         { return v.stage == "running" || v.stage == "cancelling" }
func (v *composeUpdateView) progressMode() bool { return v.busy() || v.stage == "done" }
func (v *composeUpdateView) Close() {
	v.cancel()
	if v.operationCancel != nil {
		v.operationCancel()
	}
}
func (v *composeUpdateView) SetSize(width, height int) {
	bottom := v.vp.AtBottom()
	v.list.SetSize(width, height)
	v.page = max(height-5, 1)
	v.vp.SetWidth(width)
	header := 1
	if v.progressMode() {
		header = 2
	}
	v.vp.SetHeight(max(height-header, 1))
	if v.progressMode() && bottom {
		v.vp.GotoBottom()
	}
}

func (v *composeUpdateView) View() string {
	scope := v.selected
	if scope == "" {
		scope = "all discovered projects (list filter ignored)"
	}
	action := "Update "
	if v.progressMode() {
		action = "Updating "
	}
	if v.stage == "done" {
		action = "Finished update "
	}
	title := ansi.Truncate(render.Bold.Render(action+scope+" · pull → up -d"), v.width, "…")
	if v.showOutput {
		header := title + "\n"
		if v.progressMode() {
			header += v.progressHeading() + "\n"
		}
		lines := strings.Split(header+v.vp.View(), "\n")
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
			note = "Finished. Enter opens multiline Compose output; return to Projects for current state."
		default:
			note = "May recreate/start services. Review paths and images with Enter; blocked projects are skipped."
		}
	}
	lines := []string{title, ansi.Truncate(render.Dim.Render(strings.Join(strings.Fields(note), " ")), v.width, "…")}
	if v.height > 2 {
		lines = append(lines, strings.Split(v.view(max(v.height-3, 1)), "\n")...)
	}
	if r := v.selectedRow(); r != nil {
		detail := r.project.Directory + " · Enter for output"
		if r.project.Issue != "" {
			detail = r.project.Directory + " · " + r.project.Issue
		}
		lines = append(lines, ansi.Truncate(strings.Join(strings.Fields(ansi.Strip(detail)), " "), v.width, "…"))
	}
	return strings.Join(lines[:min(len(lines), max(v.height, 1))], "\n")
}

func (v *composeUpdateView) Status() string {
	counts := map[string]int{}
	for _, r := range v.items {
		counts[r.state]++
	}
	parts := []string{v.stage, fmt.Sprintf("%d projects", len(v.items))}
	if len(v.items) == 1 {
		parts[1] = "1 project"
	}
	for _, state := range []string{"ready", "succeeded", "failed", "blocked", "skipped", "unknown", "cancelled"} {
		if counts[state] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[state], state))
		}
	}
	if v.showOutput && v.progressMode() {
		if v.followActive && v.vp.AtBottom() {
			parts = append(parts, "following active project")
		} else {
			parts = append(parts, "scrollback · G follows active project")
		}
	}
	return strings.Join(parts, " · ")
}

func (v *composeUpdateView) progressHeading() string {
	r := v.selectedRow()
	if r == nil {
		return ""
	}
	_, keys := v.visible()
	phase := map[string]string{"ready": "Waiting", "checking": "Checking configuration", "pulling": "1/2 Pulling images", "starting": "2/2 Starting updated services", "succeeded": "✓ Update complete", "failed": "✗ Update failed", "cancelled": "Cancelled", "unknown": "Interrupted; check project state", "skipped": "Skipped", "blocked": "Blocked"}[r.state]
	where := "local"
	if r.project.Remote {
		where = "SSH host"
	}
	text := fmt.Sprintf("%s · project %d/%d · %s · %s", r.project.Name, v.cur.index(keys)+1, len(keys), where, phase)
	style := render.Accent
	if r.state == "failed" || r.state == "unknown" {
		style = render.Error
	}
	return ansi.Truncate(style.Render(text), v.width, "…")
}

func (v *composeUpdateView) Footer() string {
	if v.showOutput {
		if v.progressMode() {
			closeHint := "back"
			if v.busy() {
				closeHint = "cancel remaining"
			}
			logHint := "raw log"
			if v.rawOutput {
				logHint = "compact progress"
			}
			return ansi.Truncate(hints("esc", closeHint, "l", logHint, "↑↓/pgup/pgdn", "scroll", "G", "follow active", "[/]", "project", "tab", "project list"), v.width, "…")
		}
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
