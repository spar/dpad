package tui

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"

	"dpad/internal/compose"
	"dpad/internal/config"
	"dpad/internal/render"
	"dpad/internal/sortx"
)

// composeDiscoveredMsg lists projects from container labels so the preview can
// show them while their Compose files are still being read.
type composeDiscoveredMsg struct {
	view       *composeUpdateView
	gen        int
	projects   []compose.Project
	containers []container.Summary
	err        error
}

type composePlanMsg struct {
	view     *composeUpdateView
	gen      int
	projects []compose.Project
}

type composeEventsMsg struct {
	view   *composeUpdateView
	events []compose.Event
	done   bool
}

type composeTickMsg struct{ view *composeUpdateView }

type composeUpdateRow struct {
	project       compose.Project
	state, output string
	progress      composeProgress
	began, ended  time.Time
}

type composeUpdateView struct {
	list[composeUpdateRow]
	ctx                     context.Context
	cancel, operationCancel context.CancelFunc
	client                  dockerClient
	env                     []string
	selected, stage, note   string
	gen                     int
	projects                []compose.Project
	events                  <-chan compose.Event
	showOutput              bool
	rawOutput               bool
	followActive            bool
	activeProject           string
	outputProject           string
	vp                      viewport.Model
	locations               map[string]string
	started, finished       time.Time
	ticking                 bool
	now                     func() time.Time
	projectConfig           map[string]config.ComposeProject
}

func newComposeUpdate(ctx context.Context, c dockerClient, selected string, width, height int) (*composeUpdateView, tea.Cmd) {
	ctx, cancel := context.WithCancel(ctx)
	v := &composeUpdateView{ctx: ctx, cancel: cancel, client: c, env: c.CLIEnvironment(), selected: selected, stage: "loading", vp: viewport.New(), locations: map[string]string{}, now: time.Now}
	v.vp.SoftWrap = true
	v.list = list[composeUpdateRow]{
		columns: []render.Column{{Header: "PROJECT", Max: 35}, {Header: "RUN ON"}, {Header: "SERVICES"}, {Header: "RESULT", Max: 80}},
		sort:    func([]composeUpdateRow, sortx.Key, bool) {},
		rows: func(r composeUpdateRow) []row {
			where := "local"
			if r.project.Remote {
				where = "SSH host"
			}
			services := fmt.Sprint(len(r.project.Services))
			if r.state == "reading" {
				where, services = "", ""
			}
			if r.project.Issue != "" {
				services = "–"
			}
			return []row{{key: r.project.Name, cells: []string{r.project.Name, where, services, v.result(r)}}}
		},
		empty: "no Compose projects with containers", loaded: true,
	}
	v.SetSize(width, height)
	return v, v.plan()
}

func (v *composeUpdateView) plan() tea.Cmd {
	v.gen++
	gen := v.gen
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(v.ctx, 5*time.Second)
		items, err := v.client.Containers(ctx, true)
		cancel()
		if err != nil {
			return composeDiscoveredMsg{view: v, gen: gen, err: err}
		}
		return composeDiscoveredMsg{view: v, gen: gen, projects: compose.Discover(items, v.selected), containers: items}
	}
}

func (v *composeUpdateView) prepare(projects []compose.Project, containers []container.Summary) tea.Cmd {
	locations := maps.Clone(v.locations)
	gen := v.gen
	return func() tea.Msg {
		for i := range projects {
			p := &projects[i]
			p.Location = locations[p.Name]
			env := v.projectConfig[p.Name].Env
			for _, name := range slices.Sorted(maps.Keys(env)) {
				p.Env = append(p.Env, name+"="+env[name])
				p.EnvSources = append(p.EnvSources, name+"="+env[name]+" from config.yaml")
			}
		}
		lookup := containerEnvLookup(v.ctx, v.client, containers)
		return composePlanMsg{view: v, gen: gen, projects: compose.Prepare(v.ctx, v.env, projects, lookup)}
	}
}

// containerEnvLookup recovers a variable a wrapper script set when the project
// was started, from the environment of the project's containers. It only
// answers when every container that sets the variable agrees on one value.
func containerEnvLookup(ctx context.Context, c dockerClient, containers []container.Summary) compose.Lookup {
	var mu sync.Mutex
	envs := map[string][]string{}
	inspect := func(id string) []string {
		mu.Lock()
		defer mu.Unlock()
		if env, ok := envs[id]; ok {
			return env
		}
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		info, err := c.Inspect(ctx, id)
		if err == nil && info.Config != nil {
			envs[id] = info.Config.Env
		} else {
			envs[id] = nil
		}
		return envs[id]
	}
	return func(project, name string) (string, string, bool) {
		values := map[string]bool{}
		for _, item := range containers {
			if item.Labels[composeProjectLabel] != project {
				continue
			}
			for _, entry := range inspect(item.ID) {
				if key, value, _ := strings.Cut(entry, "="); key == name {
					values[value] = true
				}
			}
		}
		if len(values) != 1 {
			return "", "", false
		}
		for value := range values {
			if value != "" {
				return value, "the project's containers", true
			}
		}
		return "", "", false
	}
}

func (v *composeUpdateView) tick() tea.Cmd {
	if v.ticking {
		return nil
	}
	v.ticking = true
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return composeTickMsg{v} })
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
	case composeTickMsg:
		if msg.view != v || v.ctx.Err() != nil {
			return nil
		}
		v.ticking = false
		if v.stage != "reading" && !v.busy() {
			return nil
		}
		v.rowCache = nil
		return v.tick()
	case composeDiscoveredMsg:
		if msg.view != v || msg.gen != v.gen || v.ctx.Err() != nil {
			return nil
		}
		if msg.err != nil {
			v.stage, v.note = "error", connectionError(msg.err)
			return nil
		}
		v.projects = nil
		rows := make([]composeUpdateRow, len(msg.projects))
		for i, p := range msg.projects {
			rows[i] = composeUpdateRow{project: p, state: "reading"}
		}
		v.setItems(rows, nil)
		if len(rows) == 0 {
			v.stage = "preview"
			return nil
		}
		v.stage, v.started = "reading", v.now()
		return tea.Batch(v.prepare(msg.projects, msg.containers), v.tick())
	case composePlanMsg:
		if msg.view != v || msg.gen != v.gen || v.ctx.Err() != nil {
			return nil
		}
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
		v.stage = "preview"
		return nil
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
					r := &v.items[i]
					if r.began.IsZero() && event.State != "skipped" && event.State != "cancelled" {
						r.began = v.now()
					}
					r.state, r.output = event.State, event.Output
					dirty[i] = true
					break
				}
			}
		}
		for i := range dirty {
			r := &v.items[i]
			final := finalComposeState(r.state)
			r.progress.apply(r.output, final)
			if final && r.ended.IsZero() {
				r.ended = v.now()
			}
		}
		v.rowCache = nil
		changedProject := false
		if v.followActive && v.activeProject != "" && v.cur.key != v.activeProject {
			v.cur.key, changedProject = v.activeProject, true
		}
		if msg.done {
			v.stage, v.finished = "done", v.now()
			v.operationCancel()
			v.SetSize(v.width, v.height)
			if len(v.items) > 1 {
				// The summary table answers "what happened" better than the
				// last project's log; a failed project is the one to read next.
				v.showOutput = false
				for _, r := range v.items {
					if r.state == "failed" || r.state == "unknown" {
						v.cur.key = r.project.Name
						break
					}
				}
			}
		}
		if v.showOutput {
			v.updateOutput(changedProject)
		}
		if msg.done {
			return func() tea.Msg { return refreshMsg{} }
		}
		return v.waitEvents()
	}
	return nil
}

func finalComposeState(state string) bool {
	switch state {
	case "succeeded", "failed", "unknown", "cancelled", "skipped":
		return true
	}
	return false
}

func (v *composeUpdateView) start() tea.Cmd {
	v.stage, v.note, v.started = "running", "", v.now()
	v.showOutput, v.followActive = true, true
	for _, p := range v.projects {
		if p.Issue == "" {
			v.cur.key, v.activeProject = p.Name, p.Name
			break
		}
	}
	for i := range v.items {
		if v.items[i].state == "ready" {
			v.items[i].state = "waiting"
		}
	}
	v.rowCache = nil
	v.SetSize(v.width, v.height)
	v.updateOutput(true)
	ctx, cancel := context.WithCancel(v.ctx)
	v.operationCancel = cancel
	// compose.Run re-reads each project's configuration before every step and
	// fails that project if it no longer matches this preview.
	v.events = compose.Run(ctx, v.ctx, v.env, v.projects)
	return tea.Batch(v.waitEvents(), v.tick())
}

func (v *composeUpdateView) waitEvents() tea.Cmd {
	return waitActionBatch(v.ctx, v.events, func(batch []compose.Event, done bool) tea.Msg {
		return composeEventsMsg{view: v, events: batch, done: done}
	})
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

func (v *composeUpdateView) canStart() bool { return v.stage == "preview" && v.ready() > 0 }

func (v *composeUpdateView) HandleKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if (key == "y" || key == "Y") && v.canStart() {
		return v.start()
	}
	if v.showOutput {
		if !v.progressMode() && (key == "esc" || key == "q") || key == "tab" {
			v.showOutput = false
			v.SetSize(v.width, v.height)
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
			v.SetSize(v.width, v.height)
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
				return v.plan()
			}
		}
	case "enter":
		if v.selectedRow() != nil && v.stage != "loading" && v.stage != "reading" {
			v.showOutput = true
			v.SetSize(v.width, v.height)
			if v.progressMode() {
				v.followActive = v.cur.key == v.activeProject
			}
			v.updateOutput(true)
		}
	case "ctrl+r":
		if v.stage == "preview" || v.stage == "error" {
			v.stage, v.note = "loading", ""
			return v.plan()
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
			if r.state == "waiting" {
				content = "Waiting for earlier projects to finish. Projects update one at a time."
			}
		} else if content == "" {
			content = "Waiting for Compose output…"
		}
	} else {
		content = fmt.Sprintf("Project: %s\nRun on: %s\nDirectory: %s\nCompose files:\n  %s\nEnvironment files:\n  %s\nServices: %s\nImages (configured tags):\n  %s\n", p.Name, where, p.Directory, strings.Join(p.Files, "\n  "), strings.Join(p.EnvFiles, "\n  "), strings.Join(p.Services, ", "), strings.Join(p.Images, "\n  "))
		if len(p.EnvSources) > 0 {
			content += "Variables set for Compose:\n  " + strings.Join(p.EnvSources, "\n  ") + "\n"
		}
		if len(p.Unset) > 0 {
			content += "Not set, Compose uses a blank string: " + strings.Join(p.Unset, ", ") + "\n"
		}
		content += "\nCommands: docker compose pull, then docker compose up -d\nServices whose image changed are recreated; named volumes are kept.\n"
		if p.Issue != "" {
			content += "\nBlocked: " + p.Issue + "\n"
			if name, ok := strings.CutPrefix(p.Issue, "needs "); ok {
				name, _, _ = strings.Cut(name, ":")
				name, _, _ = strings.Cut(name, ",")
				content += fmt.Sprintf("\nIf a wrapper script normally sets it, add it to config.yaml:\n\n  composeProjects:\n    %s:\n      env:\n        %s: <value>\n", p.Name, name)
			}
		}
	}
	bottom := v.vp.AtBottom()
	if !v.progressMode() || v.rawOutput {
		content = plainComposeLog(composeOutputText(content))
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

// queueLines is the height of the project table shown above live progress
// when several projects update, so the whole queue stays in view.
func (v *composeUpdateView) queueLines() int {
	if !v.progressMode() || len(v.items) < 2 || v.height < 12 {
		return 0
	}
	return min(len(v.items)+1, max((v.height-3)/3, 3))
}

func (v *composeUpdateView) SetSize(width, height int) {
	bottom := v.vp.AtBottom()
	v.list.SetSize(width, height)
	v.page = max(height-5, 1)
	v.vp.SetWidth(width)
	header := 1
	if v.progressMode() {
		header = 3
		if q := v.queueLines(); q > 0 {
			header += q + 1
		}
	}
	v.vp.SetHeight(max(height-header, 1))
	if v.progressMode() && bottom {
		v.vp.GotoBottom()
	}
}

func (v *composeUpdateView) title() string {
	scope := v.selected
	if scope == "" {
		scope = "all projects"
		if v.stage != "loading" {
			scope = fmt.Sprintf("all %d projects", len(v.items))
		}
	}
	action := "Update "
	switch v.stage {
	case "running", "cancelling":
		action = "Updating "
	case "done":
		action = "Finished updating "
	}
	return ansi.Truncate(render.Bold.Render(action+scope+" · docker compose pull, then up -d"), v.width, "…")
}

func (v *composeUpdateView) View() string {
	title := v.title()
	if v.showOutput {
		header := []string{title}
		if v.progressMode() {
			header = append(header, ansi.Truncate(render.Dim.Render(v.overall()), v.width, "…"))
			if q := v.queueLines(); q > 0 {
				header = append(header, strings.Split(v.view(q), "\n")...)
				header = append(header, "")
			}
			header = append(header, v.progressHeading())
		}
		lines := append(header, strings.Split(v.vp.View(), "\n")...)
		return strings.Join(lines[:min(len(lines), max(v.height, 1))], "\n")
	}
	note := v.note
	if note == "" {
		switch v.stage {
		case "loading":
			note = "Looking for Compose projects…"
		case "reading":
			note = fmt.Sprintf("Reading Compose files for %d projects… %s", len(v.items), clock(v.now().Sub(v.started)))
		case "running", "cancelling", "done":
			note = v.overall()
		case "preview":
			switch {
			case len(v.items) == 0:
			case v.ready() == 0:
				note = "Nothing can be updated. Each project's RESULT says why."
			default:
				note = "Press y to pull new images and recreate changed services. Enter shows files and images."
				if v.ready() < len(v.items) {
					note += " Blocked projects are skipped."
				}
			}
		}
	}
	lines := []string{title, ansi.Truncate(render.Dim.Render(strings.Join(strings.Fields(note), " ")), v.width, "…")}
	if v.height > 2 {
		lines = append(lines, strings.Split(v.view(max(v.height-3, 1)), "\n")...)
	}
	if r := v.selectedRow(); r != nil {
		detail := r.project.Directory
		switch {
		case r.state == "failed" || r.state == "unknown":
			if reason := r.progress.failure(); reason != "" {
				detail += " · " + reason
			}
		case r.project.Issue != "":
			detail += " · " + r.project.Issue
		}
		if v.stage != "loading" && v.stage != "reading" {
			detail += " · enter for details"
		}
		lines = append(lines, ansi.Truncate(strings.Join(strings.Fields(ansi.Strip(detail)), " "), v.width, "…"))
	}
	return strings.Join(lines[:min(len(lines), max(v.height, 1))], "\n")
}

// result is the RESULT cell: one line that says what is happening now or what
// happened, so the table alone answers "did it work".
func (v *composeUpdateView) result(r composeUpdateRow) string {
	elapsed := ""
	if !r.began.IsZero() {
		end := r.ended
		if end.IsZero() {
			end = v.now()
		}
		elapsed = " " + clock(end.Sub(r.began))
	}
	green := lipgloss.NewStyle().Foreground(render.Green)
	switch r.state {
	case "reading":
		return render.Dim.Render("reading Compose files…")
	case "ready":
		if len(r.project.Unset) > 0 {
			return render.Warn.Render("ready · left blank: " + strings.Join(r.project.Unset, ", "))
		}
		if len(r.project.EnvSources) > 0 {
			names := make([]string, len(r.project.EnvSources))
			for i, source := range r.project.EnvSources {
				names[i], _, _ = strings.Cut(source, "=")
			}
			return "ready · sets " + strings.Join(names, ", ")
		}
		return "ready"
	case "waiting":
		return render.Dim.Render("waiting")
	case "blocked":
		return render.Warn.Render("blocked: " + firstLine(r.project.Issue))
	case "checking":
		return render.Accent.Render("checking configuration" + elapsed)
	case "pulling":
		return render.Accent.Render("pulling images" + elapsed)
	case "starting":
		return render.Accent.Render("recreating services" + elapsed)
	case "succeeded":
		recreated, downloaded := r.progress.changes()
		switch {
		case recreated > 0:
			return green.Render(fmt.Sprintf("✓ updated, %d recreated%s", recreated, elapsed))
		case downloaded > 0:
			return green.Render("✓ new images pulled, nothing recreated" + elapsed)
		}
		return green.Render("✓ already up to date" + elapsed)
	case "failed":
		reason := r.progress.failure()
		if reason == "" {
			reason = firstLine(r.output)
		}
		return render.Error.Render("✗ failed: " + reason)
	case "unknown":
		return render.Error.Render("? interrupted, check project state")
	case "cancelled":
		return render.Dim.Render("cancelled")
	case "skipped":
		return render.Dim.Render("skipped: " + firstLine(r.output))
	}
	return r.state
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(composeOutputText(s)), "\n")
	return s
}

func clock(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func (v *composeUpdateView) counts() map[string]int {
	counts := map[string]int{}
	for _, r := range v.items {
		state := r.state
		if state == "succeeded" {
			if recreated, _ := r.progress.changes(); recreated == 0 {
				state = "unchanged"
			}
		}
		counts[state]++
	}
	return counts
}

// overall summarizes the whole run in one line.
func (v *composeUpdateView) overall() string {
	counts := v.counts()
	total := len(v.items)
	done := counts["succeeded"] + counts["unchanged"] + counts["failed"] + counts["unknown"] + counts["cancelled"] + counts["skipped"] + counts["blocked"]
	end := v.finished
	if end.IsZero() {
		end = v.now()
	}
	var parts []string
	switch v.stage {
	case "done":
		parts = append(parts, "Finished in "+clock(end.Sub(v.started)))
	case "cancelling":
		parts = append(parts, "Cancelling after the current step")
	default:
		parts = append(parts, fmt.Sprintf("%d of %d projects done", done, total), clock(end.Sub(v.started))+" elapsed")
	}
	for _, c := range []struct{ state, label string }{
		{"succeeded", "updated"}, {"unchanged", "already up to date"}, {"failed", "failed"}, {"unknown", "interrupted"},
		{"cancelled", "cancelled"}, {"skipped", "skipped"}, {"blocked", "blocked"},
	} {
		if counts[c.state] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[c.state], c.label))
		}
	}
	if v.stage == "done" && total > 1 {
		parts = append(parts, "enter shows a project's output")
	}
	return strings.Join(parts, " · ")
}

// summary is shown on Projects after the update closes.
func (v *composeUpdateView) summary() (string, bool) {
	counts := v.counts()
	failed := counts["failed"] + counts["unknown"]
	var parts []string
	for _, c := range []struct {
		n     int
		label string
	}{{counts["succeeded"], "updated"}, {counts["unchanged"], "already up to date"}, {failed, "failed"}, {counts["cancelled"], "cancelled"}} {
		if c.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c.n, c.label))
		}
	}
	return "update finished: " + strings.Join(parts, ", "), failed > 0
}

func (v *composeUpdateView) Status() string {
	counts := v.counts()
	parts := []string{v.stage, fmt.Sprintf("%d projects", len(v.items))}
	if len(v.items) == 1 {
		parts[1] = "1 project"
	}
	for _, c := range []struct{ state, label string }{
		{"ready", "ready"}, {"succeeded", "updated"}, {"unchanged", "up to date"}, {"failed", "failed"},
		{"blocked", "blocked"}, {"skipped", "skipped"}, {"unknown", "unknown"}, {"cancelled", "cancelled"},
	} {
		if counts[c.state] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[c.state], c.label))
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
	where := "local"
	if r.project.Remote {
		where = "SSH host"
	}
	text := fmt.Sprintf("%s · project %d of %d · %s · %s", r.project.Name, v.cur.index(keys)+1, len(keys), where, ansi.Strip(v.result(*r)))
	style := render.Accent
	switch r.state {
	case "failed", "unknown":
		style = render.Error
	case "succeeded":
		style = lipgloss.NewStyle().Bold(true).Foreground(render.Green)
	}
	return ansi.Truncate(style.Render(text), v.width, "…")
}

func (v *composeUpdateView) Footer() string {
	if v.showOutput {
		if v.progressMode() {
			closeHint := "back to Projects"
			if v.busy() {
				closeHint = "cancel remaining"
			}
			logHint := "raw log"
			if v.rawOutput {
				logHint = "compact progress"
			}
			return ansi.Truncate(hints("esc", closeHint, "[/]", "other project", "l", logHint, "↑↓/pgup/pgdn", "scroll", "G", "follow active", "tab", "summary table"), v.width, "…")
		}
		if v.canStart() {
			return hints("y", "start update", "esc", "back to list", "↑↓/pgup/pgdn", "scroll")
		}
		return hints("esc", "back to list", "↑↓/pgup/pgdn", "scroll output", "g/G", "top/bottom")
	}
	if v.busy() {
		return hints("esc", "cancel remaining", "enter", "live output", "↑↓", "projects")
	}
	if v.stage == "done" {
		return hints("esc", "back to Projects", "enter", "project output", "↑↓", "projects")
	}
	if v.canStart() {
		keys := []string{"y", "start update", "enter", "details", "esc", "cancel", "ctrl+r", "refresh"}
		if v.hasSSH() {
			keys = append(keys, "h", "local/SSH host")
		}
		return hints(keys...)
	}
	keys := []string{"esc", "back", "enter", "details", "↑↓", "projects", "ctrl+r", "refresh"}
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
			a.flash, a.flashErr = "select a project to update, or press U to update all projects", true
			return nil
		}
	}
	var cmd tea.Cmd
	a.composeUpdate, cmd = newComposeUpdate(a.ctx, a.client, selected, a.width, a.bodyHeight())
	a.composeUpdate.projectConfig = a.composeProjects
	return cmd
}
