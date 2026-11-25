package tui

import (
	"context"
	"fmt"
	"image/color"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"dpad/internal/config"
	"dpad/internal/docker"
	"dpad/internal/render"
	"dpad/internal/sortx"
)

var keyStyle = lipgloss.NewStyle().Bold(true)

func brand() string { return render.Brand.Render("▍") + render.Bold.Render("dpad") }

// Each tab has its own accent from the theme, carried into its headers and
// selection.
func tabAccent(i int) color.Color {
	accents := render.Theme().TabAccents()
	return accents[i%len(accents)]
}

// tabLabel renders a tab: a filled accent block when active, otherwise its
// number in the accent and its name dimmed. label is "N" or "N Title".
func tabLabel(i int, label string, active bool) string {
	if active {
		st := lipgloss.NewStyle().Reverse(true)
		if c := tabAccent(i); c != nil && !render.ColorDisabled() {
			st = render.OnAccent(c)
		}
		return st.Bold(true).Padding(0, 1).Render(label)
	}
	num, name, _ := strings.Cut(label, " ")
	s := lipgloss.NewStyle().Bold(true).Foreground(tabAccent(i)).Render(num)
	if name != "" {
		s += " " + render.Dim.Render(name)
	}
	return " " + s + " "
}

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
	prune           *pruneView
	composeUpdate   *composeUpdateView
	shell           *shellPrompt
	contextPicker   *contextPicker
	themePicker     *themePicker
	nextContext     string
	actionsInFlight int
	help            bool
	flash           string
	flashErr        bool
	daemon          daemonConnection
	mouse           bool
	click           lastClick
	commands        []config.CustomCommand
	composeProjects map[string]config.ComposeProject
}

// Run starts the TUI and blocks until the user quits. themeErr, when set,
// is shown once: why the chosen theme could not be used.
func Run(ctx context.Context, c dockerClient, themeErr error) error {
	cfg, cfgErr := config.Load()
	return runSessions(ctx, c, func(app *App) error {
		app.configure(cfg, cfgErr)
		if themeErr != nil && app.flash == "" {
			app.flash, app.flashErr = oneLine(themeErr.Error())+"; using the default theme", true
			themeErr = nil
		}
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
	app := &App{ctx: ctx, client: c, tabs: tabs, filter: fi, daemon: daemonConnection{label: c.ConnectionLabel()}, mouse: true}
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

func (a *App) bodyHeight() int { return max(a.height-5, 1) }

func (a *App) resize(width, height int) {
	a.width, a.height = width, height
	for _, t := range a.tabs {
		t.SetSize(a.width, a.bodyHeight())
	}
	if a.logs != nil {
		a.logs.SetSize(a.width, a.bodyHeight())
	}
	for _, v := range a.detailScreens() {
		v.SetSize(a.width, a.bodyHeight())
	}
	if a.projectAction != nil {
		a.projectAction.SetSize(a.width, a.bodyHeight())
	}
	if a.composeUpdate != nil {
		a.composeUpdate.SetSize(a.width, a.bodyHeight())
	}
	if a.prune != nil {
		a.prune.SetSize(a.width, a.bodyHeight())
	}
	if a.shell != nil {
		a.shell.input.SetWidth(max(width-38, 1))
	}
	if a.contextPicker != nil {
		a.contextPicker.SetSize(width, a.bodyHeight())
	}
	a.sizeThemePicker()
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
	case tea.MouseClickMsg, tea.MouseWheelMsg:
		return a, a.handleMouse(msg.(tea.MouseMsg))
	case commandDoneMsg:
		a.flash = fmt.Sprintf("%s on %s: exited", msg.name, msg.target)
		a.flashErr = msg.err != nil
		if msg.err != nil {
			a.flash += ": " + msg.err.Error()
		}
		return a, func() tea.Msg { return refreshMsg{} }
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
	for _, v := range a.detailScreens() {
		cmds = append(cmds, v.Update(msg))
	}
	if a.projectAction != nil {
		cmds = append(cmds, a.projectAction.Update(msg))
	}
	if a.composeUpdate != nil {
		cmds = append(cmds, a.composeUpdate.Update(msg))
	}
	if a.prune != nil {
		cmds = append(cmds, a.prune.Update(msg))
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
		if (!v.showOutput || v.progressMode()) && (key == "esc" || key == "q" || key == "n") {
			if v.busy() {
				v.operationCancel()
				v.stage = "cancelling"
			} else {
				if v.stage == "done" {
					a.flash, a.flashErr = v.summary()
				}
				v.Close()
				a.composeUpdate = nil
			}
			return nil
		}
		return v.HandleKey(msg)
	case a.contextPicker != nil:
		return a.updateContextPicker(msg)
	case a.themePicker != nil:
		return a.updateThemePicker(key)
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
	case a.prune != nil:
		v := a.prune
		if key == "esc" || key == "q" || key == "n" {
			if v.busy() {
				v.operationCancel()
				v.stage = "cancelling"
			} else {
				v.Close()
				a.prune = nil
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
		if p.start != nil {
			return p.start()
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
		switch key {
		case "esc", "q":
			a.logs.Close()
			a.logs = nil
			return nil
		case "?":
			a.help = true
			return nil
		}
		return a.logs.Update(msg)
	case a.activeDetails() != nil:
		return a.handleDetailsKey(msg)
	}

	if _, ok := a.tabs[a.active].(*EventsView); ok {
		switch key {
		case "e", "S", "x", "R", "P", "K", "D", "X":
			return nil
		}
	}
	if cmd, ok := a.runCustomCommand(key); ok {
		return cmd
	}

	switch key {
	case "X":
		var cmd tea.Cmd
		a.prune, cmd = newPruneView(a.ctx, a.client, pruneKindFor(a.tabs[a.active]), a.width, a.bodyHeight())
		return cmd
	case "u", "U":
		return a.openComposeUpdate(key == "U")
	case "c":
		if a.actionsInFlight > 0 {
			a.flash, a.flashErr = "wait for the container action to finish before switching contexts", true
			return nil
		}
		return a.openContextPicker()
	case "C":
		if render.ColorDisabled() {
			a.flash, a.flashErr = "colors are off (NO_COLOR or --no-color), so themes don't apply", true
			return nil
		}
		a.openThemePicker()
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
		if _, ok := a.tabs[a.active].(*EventsView); ok {
			return a.openSelectedContainer()
		}
		if cmd, handled := a.openSelectedResource(); handled {
			return cmd
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
		if cmd, handled := a.openSelectedResource(); handled {
			return cmd
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
	case "P":
		a.ask("pause", a.client.Pause)
	case "K":
		a.ask("kill", a.client.Kill)
	case "D":
		a.ask("remove", a.client.Remove)
		if a.confirm != nil {
			a.confirm.question = fmt.Sprintf("Remove container %s? It is killed first if running.", a.confirm.name)
		}
	default:
		if len(key) == 1 && key[0] >= '1' && int(key[0]-'1') < len(a.tabs) {
			return a.switchTab(int(key[0] - '1'))
		}
		return a.tabs[a.active].HandleKey(key)
	}
	return nil
}

func (a *App) switchTab(i int) tea.Cmd {
	a.active = i
	a.filter.SetValue(a.tabs[i].Filter())
	return a.tabs[i].Focus()
}

func (a *App) View() tea.View {
	v := tea.NewView(a.render())
	v.AltScreen = true
	if t := render.Theme(); !render.ColorDisabled() {
		// Bubble Tea restores the terminal's own colors on exit.
		v.BackgroundColor, v.ForegroundColor = t.Terminal.Background.C(), t.Terminal.Foreground.C()
	}
	if a.mouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

func (a *App) render() string {
	render.SetAccent(tabAccent(a.active))
	var body, status string
	details := a.detailScreens()
	switch {
	case a.composeUpdate != nil:
		body, status = a.composeUpdate.View(), a.composeUpdate.Status()
	case a.contextPicker != nil:
		body = a.contextPicker.View()
		status = "Choose a Docker context for this session · switching clears resource data"
	case a.themePicker != nil:
		body = a.themePickerView()
		status = "Choose a color theme · the screen previews the highlighted one"
	case a.projectAction != nil:
		body, status = a.projectAction.View(), a.projectAction.Status()
	case a.prune != nil:
		body, status = a.prune.View(), a.prune.Status()
	case a.help:
		body = a.helpView()
	case a.logs != nil:
		body, status = a.logs.View(), a.logs.Status()
	case len(details) > 0:
		body, status = details[0].View(), details[0].Status()
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
	return strings.Join([]string{a.tabBar(), a.tabRule(), a.connectionBar(), body, render.Dim.Render(status), a.footer()}, "\n")
}

func (a *App) tabBar() string {
	parts, _ := a.tabBarParts()
	return strings.Join(parts, " ")
}

// tabBarParts returns the tab bar's space-separated parts and the tab each
// shows (-1 for the logo), narrowing labels until the bar fits.
func (a *App) tabBarParts() ([]string, []int) {
	parts := []string{brand() + " "}
	owners := []int{-1}
	for i, t := range a.tabs {
		label := fmt.Sprintf("%d %s", i+1, t.Title())
		parts = append(parts, tabLabel(i, label, i == a.active))
		owners = append(owners, i)
	}
	fits := func() bool { return a.width <= 0 || lipgloss.Width(strings.Join(parts, " ")) <= a.width }
	if fits() {
		return parts, owners
	}
	parts, owners = parts[1:], owners[1:]
	if fits() {
		return parts, owners
	}
	for i := range parts {
		if i != a.active {
			parts[i] = tabLabel(i, fmt.Sprint(i+1), false)
		}
	}
	if fits() {
		return parts, owners
	}
	return parts[a.active : a.active+1], owners[a.active : a.active+1]
}

// tabRule underlines the active tab with a heavy accent line and the rest of
// the row with a faint one.
func (a *App) tabRule() string {
	parts, owners := a.tabBarParts()
	pos := 0
	for i, p := range parts {
		w := lipgloss.Width(p)
		if owners[i] == a.active {
			return render.Dim.Render(strings.Repeat("─", pos)) +
				lipgloss.NewStyle().Foreground(tabAccent(a.active)).Render(strings.Repeat("━", w)) +
				render.Dim.Render(strings.Repeat("─", max(a.width-pos-w, 0)))
		}
		pos += w + 1
	}
	return render.Dim.Render(strings.Repeat("─", a.width))
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
	case a.themePicker != nil:
		return hints("↑↓/j/k", "preview", "enter", "use for this session", "esc", "cancel")
	case a.projectAction != nil:
		return a.projectAction.Footer()
	case a.prune != nil:
		return a.prune.Footer()
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
		return hints("esc", "back", "/", "regex filter", "w", "wrap", "t", "timestamps", "←→", "pan", "g/G", "top/follow", "?", "help")
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

	keys := []string{"tab", "switch"}
	if _, ok := a.tabs[a.active].(*ProjectsView); ok {
		keys = append(keys, "u", "update project (pull + up)", "U", "update all")
	}
	keys = append(keys, "c", "context", "/", "filter", "s/r", "sort: "+string(a.tabs[a.active].SortKey()))
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
		keys = append(keys, "d", "details", "enter", "logs", "e", "shell", "S/x/R", "start/stop/restart", "P/K/D", "pause/kill/remove")
	} else if stats, ok := a.tabs[a.active].(*StatsView); ok && stats.selectedStatsProject() != nil {
		keys = append(keys, "space/enter", "expand/collapse")
	} else if images, ok := a.tabs[a.active].(*ImagesView); ok {
		if _, ok := images.SelectedImage(); ok {
			keys = append(keys, "d/enter", "image details", "x", "remove tag/image")
		}
		keys = append(keys, "X", "prune")
	} else if volumes, ok := a.tabs[a.active].(*VolumesView); ok {
		if _, ok := volumes.SelectedVolume(); ok {
			keys = append(keys, "d/enter", "volume details")
		}
		keys = append(keys, "X", "prune")
	} else if networks, ok := a.tabs[a.active].(*NetworksView); ok {
		if _, _, ok := networks.SelectedNetwork(); ok {
			keys = append(keys, "d/enter", "network details")
		}
		keys = append(keys, "X", "prune")
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
	if a.logs != nil {
		return helpTable("Logs", logsHelp)
	}
	global := [][2]string{
		{"c", "choose Docker context for this session (from a list tab)"},
		{fmt.Sprintf("tab / shift+tab, 1-%d", len(a.tabs)), "switch tabs"},
		{"↑↓ / j k, pgup/pgdn, g/G", "move selection"},
		{"/", "filter (enter keeps, esc clears)"},
		{"enter", "follow logs, expand a Stats project, or inspect resource details"},
		{"d", "inspect selected container, image, volume, or network"},
		{"e", "open a shell in the selected container (choose executable, enter to open)"},
		{"S / x / R", "start (or unpause) / stop / restart selected container"},
		{"P / K / D", "pause / kill / remove selected container (remove kills it first if running)"},
		{"X", "prune: preview, then remove stopped containers or unused images, volumes, networks, build cache"},
		{"C", "choose a color theme, previewed as you move"},
		{"ctrl+r", "refresh now"},
		{"q, ctrl+c", "quit"},
	}
	if a.mouse {
		global = append(global, [2]string{"mouse", "click a tab or row to select it, double-click a row for enter, wheel to move"})
	}
	_, eventsTab := a.tabs[a.active].(*EventsView)
	_, volumesTab := a.tabs[a.active].(*VolumesView)
	_, networksTab := a.tabs[a.active].(*NetworksView)
	if eventsTab || volumesTab || networksTab {
		global = slices.DeleteFunc(global, func(h [2]string) bool { return h[0] == "e" || h[0] == "S / x / R" || h[0] == "P / K / D" })
	}
	if eventsTab {
		global = slices.DeleteFunc(global, func(h [2]string) bool { return h[0] == "X" })
	}
	return helpTable(a.tabs[a.active].Title(), append(append(global, a.tabs[a.active].Help()...), a.customCommandHelp()...))
}

var logsHelp = [][2]string{
	{"/", "filter buffered lines by regular expression (enter keeps, esc cancels)"},
	{"ctrl+l", "clear the filter"},
	{"w", "wrap long lines (off: ←→ pans)"},
	{"t", "show timestamps"},
	{"↑↓ / j k", "scroll a line"},
	{"pgup/pgdn, b/space, u/d", "scroll a page or half a page"},
	{"g / G", "top / bottom, following new lines"},
	{"esc, q", "back"},
}

func helpTable(title string, help [][2]string) string {
	rows := make([][]string, len(help))
	for i, h := range help {
		rows[i] = []string{keyStyle.Render(h[0]), h[1]}
	}
	t := render.Table{Columns: []render.Column{{Header: "KEY"}, {Header: title + " — ACTION"}}, Rows: rows}
	return t.String() + "\n\n" + render.Dim.Render("press any key to close")
}
