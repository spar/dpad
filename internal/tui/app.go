package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"dockernext/internal/docker"
	"dockernext/internal/render"
	"dockernext/internal/sortx"
)

type actionDoneMsg struct {
	text  string
	err   error
	image *docker.ImageRemoval
}

// pending is an action waiting for y/n confirmation.
type pending struct {
	verb, id, name string
	run            func(context.Context, string) error
	question       string
	image          *docker.ImageRemoval
}

var (
	tabActive   = lipgloss.NewStyle().Bold(true).Reverse(true).Padding(0, 1)
	tabInactive = lipgloss.NewStyle().Padding(0, 1).Foreground(render.Gray)
	keyStyle    = lipgloss.NewStyle().Bold(true)
)

// App is the full-screen TUI with Containers / Images / Stats / Projects / Events / Volumes / Networks tabs.
type App struct {
	ctx             context.Context
	client          dockerClient
	tabs            []tab
	active          int
	width, height   int
	filter          textinput.Model // edits the active tab's filter
	filtering       bool
	confirm         *pending
	logs            *LogsView
	details         *DetailsView
	imageDetails    *ImageDetailsView
	volumeDetails   *VolumeDetailsView
	networkDetails  *NetworkDetailsView
	projectAction   *projectActionView
	composeUpdate   *composeUpdateView
	shell           *shellPrompt
	contextPicker   *contextPicker
	nextContext     string
	actionsInFlight int
	help            bool
	flash           string
	flashErr        bool
	daemon          daemonConnection
}

// Run starts the TUI and blocks until the user quits.
func Run(ctx context.Context, c dockerClient) error {
	return runSessions(ctx, c, func(app *App) error {
		_, err := tea.NewProgram(app, programOptions(app.ctx)...).Run()
		return err
	}, func(ctx context.Context, name string) (dockerClient, error) {
		return docker.NewForContext(ctx, name)
	})
}

func newApp(ctx context.Context, c dockerClient) *App {
	fi := textinput.New()
	fi.Prompt = "/"
	stats := NewStatsView(ctx, c, sortx.CPU, false)
	stats.SetGrouped(true)
	tabs := []tab{
		NewContainersView(ctx, c),
		NewImagesView(ctx, c),
		stats,
		NewProjectsView(),
		NewEventsView(ctx, c),
		NewVolumesView(ctx, c),
		NewNetworksView(ctx, c),
	}
	app := &App{ctx: ctx, client: c, tabs: tabs, filter: fi, daemon: daemonConnection{label: c.ConnectionLabel()}}
	app.resize(120, 30)
	return app
}

// Each daemon gets its own program queue as well as its own request context.
// Late replies and timers from a previous session cannot enter the next one.
func runSessions(ctx context.Context, c dockerClient, run func(*App) error, connect func(context.Context, string) (dockerClient, error)) error {
	defer func() { _ = c.Close() }()
	active, flash := 0, ""
	for {
		sessionCtx, cancel := context.WithCancel(ctx)
		app := newApp(sessionCtx, c)
		app.active, app.flash, app.flashErr = active, flash, flash != ""
		err := run(app)
		cancel()
		if err != nil || app.nextContext == "" {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		active = app.active
		next, err := connect(ctx, app.nextContext)
		if err != nil {
			flash = fmt.Sprintf("cannot switch to %s: %s", app.nextContext, connectionError(err))
			continue
		}
		_ = c.Close()
		c, flash = next, ""
	}
}

func (a *App) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(a.tabs)+1)
	for _, t := range a.tabs {
		cmds = append(cmds, t.Init())
	}
	cmds = append(cmds, a.probeDaemon())
	return tea.Batch(append(cmds, a.tabs[a.active].Focus())...)
}

func (a *App) bodyHeight() int { return max(a.height-4, 1) }

func (a *App) resize(width, height int) {
	a.width, a.height = width, height
	for _, t := range a.tabs {
		t.SetSize(a.width, a.bodyHeight())
	}
	if a.logs != nil {
		a.logs.SetSize(a.width, a.bodyHeight())
	}
	if a.details != nil {
		a.details.SetSize(a.width, a.bodyHeight())
	}
	if a.imageDetails != nil {
		a.imageDetails.SetSize(a.width, a.bodyHeight())
	}
	if a.volumeDetails != nil {
		a.volumeDetails.SetSize(a.width, a.bodyHeight())
	}
	if a.networkDetails != nil {
		a.networkDetails.SetSize(a.width, a.bodyHeight())
	}
	if a.projectAction != nil {
		a.projectAction.SetSize(a.width, a.bodyHeight())
	}
	if a.composeUpdate != nil {
		a.composeUpdate.SetSize(a.width, a.bodyHeight())
	}
	if a.shell != nil {
		a.shell.input.SetWidth(max(width-38, 1))
	}
	if a.contextPicker != nil {
		a.contextPicker.SetSize(width, a.bodyHeight())
	}
}

// setFilter sets the active tab's filter and the input showing it.
func (a *App) setFilter(f string) {
	a.filter.SetValue(f)
	a.tabs[a.active].SetFilter(f)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case contextsMsg:
		if a.contextPicker == msg.picker {
			a.contextPicker.setItems(msg.items, msg.err)
			for _, c := range msg.items {
				if c.Name+" @ "+c.Endpoint == a.daemon.label {
					a.contextPicker.cur.key = c.Name
				}
			}
		}
		return a, nil
	case daemonProbeMsg:
		return a, a.applyDaemonProbe(msg)
	case daemonProbeTickMsg:
		if msg.generation != a.daemon.generation {
			return a, nil
		}
		return a, a.probeDaemon()
	case tea.WindowSizeMsg:
		a.resize(msg.Width, msg.Height)
		return a, nil
	case tea.KeyPressMsg:
		return a, a.handleKey(msg)
	case shellReadyMsg:
		if a.shell == nil || a.shell != msg.prompt {
			return a, nil
		}
		if msg.err != nil {
			a.shell.launching = false
			a.flash, a.flashErr = msg.err.Error(), true
			return a, nil
		}
		p := a.shell
		a.shell = nil
		return a, tea.Exec(msg.process, func(err error) tea.Msg {
			return shellDoneMsg{name: p.name, executable: p.input.Value(), err: err}
		})
	case shellDoneMsg:
		a.flash = fmt.Sprintf("shell %s in %s: exited", msg.executable, msg.name)
		a.flashErr = msg.err != nil
		if msg.err != nil {
			a.flash += ": " + msg.err.Error()
		}
		cmds := []tea.Cmd{func() tea.Msg { return refreshMsg{} }}
		if a.details != nil {
			cmds = append(cmds, a.details.load())
		}
		return a, tea.Batch(cmds...)
	case actionDoneMsg:
		a.actionsInFlight = max(a.actionsInFlight-1, 0)
		a.flash, a.flashErr = msg.text, msg.err != nil
		if msg.err != nil {
			a.flash = msg.err.Error()
		}
		if msg.err == nil && msg.image != nil && a.imageDetails != nil && a.imageDetails.removal.ID == msg.image.ID && a.imageDetails.removal.Reference == msg.image.Reference {
			a.imageDetails.Close()
			a.imageDetails = nil
		}
		return a, func() tea.Msg { return refreshMsg{} }
	}

	var cmds []tea.Cmd
	if a.contextPicker != nil && a.contextPicker.filtering {
		var cmd tea.Cmd
		a.contextPicker.search, cmd = a.contextPicker.search.Update(msg)
		cmds = append(cmds, cmd)
	}
	if a.shell != nil && !a.shell.launching {
		var cmd tea.Cmd
		a.shell.input, cmd = a.shell.input.Update(msg)
		cmds = append(cmds, cmd)
	}
	for _, t := range a.tabs {
		cmds = append(cmds, t.Update(msg))
	}
	if a.logs != nil {
		cmds = append(cmds, a.logs.Update(msg))
	}
	if a.details != nil {
		cmds = append(cmds, a.details.Update(msg))
	}
	if a.imageDetails != nil {
		cmds = append(cmds, a.imageDetails.Update(msg))
	}
	if a.volumeDetails != nil {
		cmds = append(cmds, a.volumeDetails.Update(msg))
	}
	if a.networkDetails != nil {
		cmds = append(cmds, a.networkDetails.Update(msg))
	}
	if a.projectAction != nil {
		cmds = append(cmds, a.projectAction.Update(msg))
	}
	if a.composeUpdate != nil {
		cmds = append(cmds, a.composeUpdate.Update(msg))
	}
	return a, tea.Batch(cmds...)
}

func (a *App) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if key == "ctrl+c" {
		return tea.Quit
	}
	a.flash = ""

	switch {
	case a.composeUpdate != nil:
		v := a.composeUpdate
		if !v.showOutput && (key == "esc" || key == "q" || key == "n") {
			if v.busy() {
				v.operationCancel()
				v.stage = "cancelling"
			} else {
				v.Close()
				a.composeUpdate = nil
			}
			return nil
		}
		return v.HandleKey(msg)
	case a.contextPicker != nil:
		return a.updateContextPicker(msg)
	case a.projectAction != nil:
		v := a.projectAction
		if key == "esc" || key == "q" || key == "n" {
			if v.busy() {
				v.operationCancel()
				v.stage = "cancelling"
			} else {
				v.Close()
				a.projectAction = nil
			}
			return nil
		}
		return v.HandleKey(key)
	case a.shell != nil:
		return a.updateShell(msg)
	case a.confirm != nil:
		p := a.confirm
		a.confirm = nil
		if key != "y" && key != "Y" {
			return nil
		}
		a.flash = fmt.Sprintf("%s %s…", p.verb, p.name)
		a.actionsInFlight++
		return func() tea.Msg {
			err := p.run(a.ctx, p.id)
			return actionDoneMsg{text: fmt.Sprintf("%s %s: done", p.verb, p.name), err: err, image: p.image}
		}
	case a.filtering:
		switch key {
		case "enter":
			a.filtering = false
			a.filter.Blur()
		case "esc":
			a.filtering = false
			a.filter.Blur()
			a.setFilter("")
		default:
			var cmd tea.Cmd
			a.filter, cmd = a.filter.Update(msg)
			a.tabs[a.active].SetFilter(a.filter.Value())
			return cmd
		}
		return nil
	case a.help:
		a.help = false
		return nil
	case a.logs != nil:
		if a.logs.searching {
			return a.logs.Update(msg)
		}
		if key == "esc" || key == "q" {
			a.logs.Close()
			a.logs = nil
			return nil
		}
		return a.logs.Update(msg)
	case a.details != nil:
		switch key {
		case "e":
			return a.openShell(a.details.id, a.details.name)
		case "esc", "q":
			a.details.Close()
			a.details = nil
			return nil
		case "enter":
			var cmd tea.Cmd
			a.logs, cmd = NewLogsView(a.ctx, a.client, a.details.id, a.details.name, a.width, a.bodyHeight())
			return cmd
		}
		return a.details.Update(msg)
	case a.networkDetails != nil:
		switch key {
		case "esc", "q":
			a.networkDetails.Close()
			a.networkDetails = nil
			return nil
		case "enter", "d":
			if id, name, ok := a.networkDetails.SelectedEndpoint(); ok {
				var cmd tea.Cmd
				a.details, cmd = NewDetailsView(a.ctx, a.client, id, name, a.width, a.bodyHeight())
				return cmd
			}
			return nil
		}
		return a.networkDetails.Update(msg)

	case a.volumeDetails != nil:
		if key == "esc" || key == "q" {
			a.volumeDetails.Close()
			a.volumeDetails = nil
			return nil
		}
		return a.volumeDetails.Update(msg)

	case a.imageDetails != nil:
		switch key {
		case "esc", "q":
			a.imageDetails.Close()
			a.imageDetails = nil
			return nil
		case "x":
			if !a.imageDetails.loading && a.imageDetails.err == nil {
				a.askImageRemoval(a.imageDetails.removal)
			}
			return nil
		}
		return a.imageDetails.Update(msg)
	}

	if _, ok := a.tabs[a.active].(*EventsView); ok {
		switch key {
		case "e", "S", "x", "R":
			return nil
		}
	}

	switch key {
	case "u", "U":
		return a.openComposeUpdate(key == "U")
	case "c":
		if a.actionsInFlight > 0 {
			a.flash, a.flashErr = "wait for the container action to finish before switching contexts", true
			return nil
		}
		return a.openContextPicker()
	case "q":
		return tea.Quit
	case "tab", "right", "l":
		return a.switchTab((a.active + 1) % len(a.tabs))
	case "shift+tab", "left", "h":
		return a.switchTab((a.active + len(a.tabs) - 1) % len(a.tabs))
	case "/":
		a.filtering = true
		return a.filter.Focus()
	case "esc":
		a.setFilter("")
	case "?":
		a.help = true
	case "ctrl+r":
		return tea.Batch(func() tea.Msg { return refreshMsg{} }, a.probeDaemon())
	case "enter":
		if stats, ok := a.tabs[a.active].(*StatsView); ok && stats.toggleStatsProject(false) {
			return nil
		}
		if _, ok := a.tabs[a.active].(*NetworksView); ok {
			return a.openSelectedNetwork()
		}
		if _, ok := a.tabs[a.active].(*VolumesView); ok {
			return a.openSelectedVolume()
		}
		if _, ok := a.tabs[a.active].(*EventsView); ok {
			return a.openSelectedContainer()
		}
		if _, ok := a.tabs[a.active].(*ImagesView); ok {
			return a.openSelectedImage()
		}
		if projects, ok := a.tabs[a.active].(*ProjectsView); ok {
			if p := projects.selectedProject(); p != nil {
				var cmd tea.Cmd
				a.logs, cmd = newProjectLogsView(a.ctx, a.client, p.name, p.containers, a.width, a.bodyHeight())
				return cmd
			}
		}
		if id, name, ok := a.tabs[a.active].Selected(); ok {
			var cmd tea.Cmd
			a.logs, cmd = NewLogsView(a.ctx, a.client, id, name, a.width, a.bodyHeight())
			return cmd
		}
	case "d":
		if _, ok := a.tabs[a.active].(*NetworksView); ok {
			return a.openSelectedNetwork()
		}
		if _, ok := a.tabs[a.active].(*VolumesView); ok {
			return a.openSelectedVolume()
		}
		if _, ok := a.tabs[a.active].(*ImagesView); ok {
			return a.openSelectedImage()
		}
		return a.openSelectedContainer()
	case "e":
		if id, name, ok := a.tabs[a.active].Selected(); ok {
			return a.openShell(id, name)
		}
	case "S":
		if cmd, ok := a.openProjectAction("start"); ok {
			return cmd
		}
		a.ask("start", a.client.Start)
	case "x":
		if images, ok := a.tabs[a.active].(*ImagesView); ok {
			if removal, ok := images.SelectedImage(); ok {
				a.askImageRemoval(removal)
			}
			return nil
		}
		if cmd, ok := a.openProjectAction("stop"); ok {
			return cmd
		}
		a.ask("stop", a.client.Stop)
	case "R":
		if cmd, ok := a.openProjectAction("restart"); ok {
			return cmd
		}
		a.ask("restart", a.client.Restart)
	default:
		if len(key) == 1 && key[0] >= '1' && int(key[0]-'1') < len(a.tabs) {
			return a.switchTab(int(key[0] - '1'))
		}
		return a.tabs[a.active].HandleKey(key)
	}
	return nil
}

func (a *App) openSelectedContainer() tea.Cmd {
	if id, name, ok := a.tabs[a.active].Selected(); ok {
		var cmd tea.Cmd
		a.details, cmd = NewDetailsView(a.ctx, a.client, id, name, a.width, a.bodyHeight())
		return cmd
	}
	return nil
}

func (a *App) openProjectAction(verb string) (tea.Cmd, bool) {
	v, ok := a.tabs[a.active].(*ProjectsView)
	if !ok {
		return nil, false
	}
	if !v.loaded || v.err != nil {
		return nil, true
	}
	if p := v.selectedProject(); p != nil {
		var cmd tea.Cmd
		a.projectAction, cmd = newProjectAction(a.ctx, a.client, p.name, verb, a.width, a.bodyHeight())
		return cmd, true
	}
	return nil, false
}

func (a *App) switchTab(i int) tea.Cmd {
	a.active = i
	a.filter.SetValue(a.tabs[i].Filter())
	return a.tabs[i].Focus()
}

func (a *App) ask(verb string, run func(context.Context, string) error) {
	if id, name, ok := a.tabs[a.active].Selected(); ok {
		a.confirm = &pending{verb: verb, id: id, name: name, run: run}
	}
}

func (a *App) View() tea.View {
	v := tea.NewView(a.render())
	v.AltScreen = true
	return v
}

func (a *App) render() string {
	var body, status string
	switch {
	case a.composeUpdate != nil:
		body, status = a.composeUpdate.View(), a.composeUpdate.Status()
	case a.contextPicker != nil:
		body = a.contextPicker.View()
		status = "Choose a Docker context for this session · switching clears resource data"
	case a.projectAction != nil:
		body, status = a.projectAction.View(), a.projectAction.Status()
	case a.help:
		body = a.helpView()
	case a.logs != nil:
		body, status = a.logs.View(), a.logs.Status()
	case a.details != nil:
		body, status = a.details.View(), a.details.Status()
	case a.imageDetails != nil:
		body, status = a.imageDetails.View(), a.imageDetails.Status()
	case a.volumeDetails != nil:
		body, status = a.volumeDetails.View(), a.volumeDetails.Status()
	case a.networkDetails != nil:
		body, status = a.networkDetails.View(), a.networkDetails.Status()
	default:
		t := a.tabs[a.active]
		body, status = t.View(), t.Status()
		if f := a.filter.Value(); f != "" && !a.filtering {
			status += " · filter: " + render.Bold.Render(f)
		}
	}
	if a.flash != "" {
		st := render.Bold
		if a.flashErr {
			st = render.Error
		}
		status += "  " + st.Render(a.flash)
	}

	// Pad the body so the status and footer stay pinned to the bottom.
	if n := a.bodyHeight() - lipgloss.Height(body); n > 0 {
		body += strings.Repeat("\n", n)
	}
	return strings.Join([]string{a.tabBar(), a.connectionBar(), body, render.Dim.Render(status), a.footer()}, "\n")
}

func (a *App) tabBar() string {
	parts := []string{render.Accent.Render("dockernext") + " "}
	for i, t := range a.tabs {
		label := fmt.Sprintf("%d %s", i+1, t.Title())
		if i == a.active {
			parts = append(parts, tabActive.Render(label))
		} else {
			parts = append(parts, tabInactive.Render(label))
		}
	}
	bar := strings.Join(parts, " ")
	if a.width > 0 && lipgloss.Width(bar) > a.width {
		parts = parts[1:]
		bar = strings.Join(parts, " ")
		if lipgloss.Width(bar) > a.width {
			for i := range parts {
				if i != a.active {
					parts[i] = tabInactive.Render(fmt.Sprint(i + 1))
				}
			}
			bar = strings.Join(parts, " ")
			if lipgloss.Width(bar) > a.width {
				bar = parts[a.active]
			}
		}
	}
	return bar
}

func (a *App) footer() string {
	switch {
	case a.composeUpdate != nil:
		return a.composeUpdate.Footer()
	case a.contextPicker != nil:
		if a.contextPicker.filtering {
			return a.contextPicker.search.View() + " " + render.Dim.Render("enter keep filter · esc cancel filter")
		}
		return hints("↑↓/j/k", "move", "/", "filter", "enter", "switch context", "esc", "cancel")
	case a.projectAction != nil:
		return a.projectAction.Footer()
	case a.shell != nil:
		if a.shell.launching {
			return render.Dim.Render("checking container… · esc cancel")
		}
		return a.shell.input.View() + " " + render.Dim.Render("enter open · esc cancel")
	case a.confirm != nil:
		if a.confirm.question != "" {
			return render.Warn.Render(a.confirm.question + " (y/N)")
		}
		return render.Warn.Render(fmt.Sprintf("%s container %s? (y/N)", a.confirm.verb, a.confirm.name))
	case a.filtering:
		return a.filter.View()
	case a.logs != nil:
		if a.logs.searching {
			return a.logs.search.View() + " " + render.Dim.Render(logSearchHints)
		}
		return hints("esc", "back", "/", "regex filter", "ctrl+l", "clear filter", "↑↓/pgup/pgdn", "scroll", "←→", "pan", "g/G", "top/follow")
	case a.details != nil:
		return hints("esc", "back", "↑↓/pgup/pgdn", "scroll", "←→", "pan", "g/G", "top/bottom", "ctrl+r", "refresh", "enter", "logs", "e", "shell")
	case a.networkDetails != nil:
		if a.networkDetails.showEndpoints {
			return hints("esc", "back", "tab", "metadata", "↑↓/pgup/pgdn", "move", "d/enter", "container details", "ctrl+r", "refresh")
		}
		return hints("esc", "back", "tab", "endpoints", "↑↓/pgup/pgdn", "scroll", "←→", "pan", "g/G", "top/bottom", "ctrl+r", "refresh")

	case a.volumeDetails != nil:
		return hints("esc", "back", "↑↓/pgup/pgdn", "scroll", "←→", "pan", "g/G", "top/bottom", "ctrl+r", "refresh")

	case a.imageDetails != nil:
		return hints("esc", "back", "↑↓/pgup/pgdn", "scroll", "←→", "pan", "g/G", "top/bottom", "ctrl+r", "refresh", "x", "remove tag/image")
	}
	if _, ok := a.tabs[a.active].(*EventsView); ok {
		return hints("tab", "switch", "c", "context", "/", "filter", "r", "reverse time", "g", "top/follow", "d/enter", "container details", "?", "help", "q", "quit")
	}

	keys := []string{"tab", "switch", "c", "context", "/", "filter", "s/r", "sort: " + string(a.tabs[a.active].SortKey())}
	if _, ok := a.tabs[a.active].(*ProjectsView); ok {
		keys = append(keys, "u/U", "update project/all")
	}
	if stats, ok := a.tabs[a.active].(*StatsView); ok {
		view := "projects"
		if stats.grouped {
			view = "containers"
		}
		keys = append(keys, "v", view)
		mode := "totals"
		if stats.totals {
			mode = "rates"
		}
		keys = append(keys, "t", mode)
	}
	if _, _, ok := a.tabs[a.active].Selected(); ok {
		keys = append(keys, "d", "details", "enter", "logs", "e", "shell", "S/x/R", "start/stop/restart")
	} else if stats, ok := a.tabs[a.active].(*StatsView); ok && stats.selectedStatsProject() != nil {
		keys = append(keys, "space/enter", "expand/collapse")
	} else if images, ok := a.tabs[a.active].(*ImagesView); ok {
		if _, ok := images.SelectedImage(); ok {
			keys = append(keys, "d/enter", "image details", "x", "remove tag/image")
		}
	} else if volumes, ok := a.tabs[a.active].(*VolumesView); ok {
		if _, ok := volumes.SelectedVolume(); ok {
			keys = append(keys, "d/enter", "volume details")
		}
	} else if networks, ok := a.tabs[a.active].(*NetworksView); ok {
		if _, _, ok := networks.SelectedNetwork(); ok {
			keys = append(keys, "d/enter", "network details")
		}
	} else if projects, ok := a.tabs[a.active].(*ProjectsView); ok && projects.selectedProject() != nil {
		keys = append(keys, "space", "expand/collapse", "enter", "project logs", "S/x/R", "project start/stop/restart")
	}
	return hints(append(keys, "?", "help", "q", "quit")...)
}

func hints(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, keyStyle.Render(pairs[i])+" "+render.Dim.Render(pairs[i+1]))
	}
	return strings.Join(parts, render.Dim.Render(" · "))
}

func (a *App) helpView() string {
	global := [][2]string{
		{"u / U (Projects)", "preview pull + up -d for selected/all Compose projects"},
		{"c", "choose Docker context for this session (from a list tab)"},
		{fmt.Sprintf("tab / shift+tab, 1-%d", len(a.tabs)), "switch tabs"},
		{"↑↓ / j k, pgup/pgdn, g/G", "move selection"},
		{"/", "filter (enter keeps, esc clears)"},
		{"enter", "follow logs, expand a Stats project, or inspect resource details"},
		{"d", "inspect selected container, image, volume, or network"},
		{"e", "open a shell in the selected container (choose executable, enter to open)"},
		{"S / x / R", "start (or unpause) / stop / restart selected container"},
		{"ctrl+r", "refresh now"},
		{"q, ctrl+c", "quit"},
	}
	_, eventsTab := a.tabs[a.active].(*EventsView)
	_, volumesTab := a.tabs[a.active].(*VolumesView)
	_, networksTab := a.tabs[a.active].(*NetworksView)
	if eventsTab || volumesTab || networksTab {
		global = slices.DeleteFunc(global, func(h [2]string) bool { return h[0] == "e" || h[0] == "S / x / R" })
	}
	rows := [][]string{}
	for _, h := range append(global, a.tabs[a.active].Help()...) {
		rows = append(rows, []string{keyStyle.Render(h[0]), h[1]})
	}
	t := render.Table{Columns: []render.Column{{Header: "KEY"}, {Header: a.tabs[a.active].Title() + " — ACTION"}}, Rows: rows}
	return t.String() + "\n\n" + render.Dim.Render("press any key to close")
}
