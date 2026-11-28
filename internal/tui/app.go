package tui

import (
	"context"
	"fmt"
	"image/color"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"dpad/internal/config"
	"dpad/internal/debuglog"
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
	composeDown     *composeDownView
	prune           *pruneView
	composeUpdate   *composeUpdateView
	composeEdit     *composeEditView
	shell           *shellPrompt
	contextPicker   *contextPicker
	themePicker     *themePicker
	editorPicker    *editorPicker
	settings        *settingsMenu
	systemView      *systemView
	nextContext     string
	actionsInFlight int
	help            bool
	flash           string
	flashErr        bool
	flashGen        int
	helpTop         int // first help row shown when the help is taller than the body
	daemon          daemonConnection
	mouse           bool
	click           lastClick
	commands        []config.CustomCommand
	composeProjects map[string]config.ComposeProject
	editor          string // the editor: setting; empty uses $VISUAL/$EDITOR
	icons           string // the icons: setting
	terminal        string // the terminal's name and version, for icons: auto
	tabCache        tabBarCache
	system          systemState
	// containersHeight and containersWidth are the size last given to the
	// Containers tab, which loses rows to the host panel.
	containersHeight, containersWidth int
}

// Run starts the TUI and blocks until the user quits. cfgErr and themeErr,
// when set, are shown once: what is wrong in the config file, and why the
// chosen theme could not be used.
func Run(ctx context.Context, c dockerClient, cfg config.Config, cfgErr, themeErr error) error {
	return runSessions(ctx, c, func(app *App) error {
		app.terminal = terminalFromEnv()
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
	containers := NewContainersView(ctx, c)
	containers.usage = stats
	projects := NewProjectsView()
	projects.sshHost, projects.localFiles = daemonHost(c.CLIEnvironment())
	projects.home, _ = os.UserHomeDir()
	tabs := []tab{
		containers,
		NewImagesView(ctx, c),
		stats,
		projects,
		NewEventsView(ctx, c),
		NewVolumesView(ctx, c),
		NewNetworksView(ctx, c),
	}
	app := &App{ctx: ctx, client: c, tabs: tabs, filter: fi, daemon: daemonConnection{label: c.ConnectionLabel()}, mouse: true}
	app.resize(120, 30)
	return app
}

// daemonHost reports the SSH host a CLI environment's daemon runs on, or
// whether it runs on this machine, from its DOCKER_HOST.
func daemonHost(env []string) (sshHost string, local bool) {
	host := ""
	for _, entry := range env {
		if key, value, _ := strings.Cut(entry, "="); strings.EqualFold(key, "DOCKER_HOST") {
			host = value
		}
	}
	u, err := url.Parse(host)
	if err != nil {
		return "", false
	}
	switch u.Scheme {
	case "ssh":
		return u.Hostname(), false
	case "", "unix", "npipe":
		return "", true
	}
	return "", false
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
	cmds = append(cmds, a.probeDaemon(), a.loadSystemInfo(), tea.RequestTerminalVersion)
	return tea.Batch(append(cmds, a.tabs[a.active].Focus())...)
}

// bodyHeight leaves rows for the tab bar, its rule, the daemon line, the rule
// above the status line, the status line and the footer.
func (a *App) bodyHeight() int { return max(a.height-6, 1) }

func (a *App) resize(width, height int) {
	a.width, a.height = width, height
	for _, t := range a.tabs {
		t.SetSize(a.width, a.bodyHeight())
	}
	for _, s := range a.screens() {
		s.SetSize(a.width, a.bodyHeight())
	}
	if a.shell != nil {
		a.shell.input.SetWidth(max(width-38, 1))
	}
	a.containersHeight = 0
	a.layout()
}

// setFilter sets the active tab's filter and the input showing it.
func (a *App) setFilter(f string) {
	a.filter.SetValue(f)
	a.tabs[a.active].SetFilter(f)
}

// flashTTL is how long a result stays in the status line. Errors stay until
// the next key so they aren't missed.
const flashTTL = 8 * time.Second

type flashExpiredMsg struct{ generation int }

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(flashExpiredMsg); ok {
		if msg.generation == a.flashGen && !a.flashErr && a.actionsInFlight == 0 {
			a.flash = ""
		}
		return a, nil
	}
	previous := a.flash
	cmd := a.update(msg)
	a.layout()
	// A flash for work in flight is replaced by its result, which starts the timer.
	if a.flash == "" || a.flash == previous || a.flashErr || a.actionsInFlight > 0 {
		return a, cmd
	}
	a.flashGen++
	gen := a.flashGen
	return a, tea.Batch(cmd, tea.Tick(flashTTL, func(time.Time) tea.Msg { return flashExpiredMsg{gen} }))
}

func (a *App) update(msg tea.Msg) tea.Cmd {
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
		return nil
	case daemonProbeMsg:
		return a.applyDaemonProbe(msg)
	case systemInfoMsg, systemInfoTickMsg, hostStartedMsg, hostUpdateMsg, hostRetryMsg:
		return a.updateSystem(msg)
	case daemonProbeTickMsg:
		if msg.generation != a.daemon.generation {
			return nil
		}
		return a.probeDaemon()
	case tea.WindowSizeMsg:
		a.resize(msg.Width, msg.Height)
		return nil
	case tea.TerminalVersionMsg:
		if msg.Name != "" {
			a.terminal = msg.Name
			a.applyIcons()
		}
		return nil
	case tea.KeyPressMsg:
		return a.handleKey(msg)
	case tea.MouseClickMsg, tea.MouseWheelMsg:
		return a.handleMouse(msg.(tea.MouseMsg))
	case commandDoneMsg:
		a.flash = fmt.Sprintf("%s on %s: exited", msg.name, msg.target)
		a.flashErr = msg.err != nil
		if msg.err != nil {
			a.flash += ": " + msg.err.Error()
		}
		return func() tea.Msg { return refreshMsg{} }
	case shellReadyMsg:
		if a.shell == nil || a.shell != msg.prompt {
			return nil
		}
		if msg.err != nil {
			a.shell.launching = false
			a.flash, a.flashErr = msg.err.Error(), true
			return nil
		}
		p := a.shell
		a.shell = nil
		return tea.Exec(msg.process, func(err error) tea.Msg {
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
		return tea.Batch(cmds...)
	case composeEditSavedMsg:
		return a.composeEditSaved(msg)
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
		return func() tea.Msg { return refreshMsg{} }
	}

	var cmds []tea.Cmd
	if a.shell != nil && !a.shell.launching {
		var cmd tea.Cmd
		a.shell.input, cmd = a.shell.input.Update(msg)
		cmds = append(cmds, cmd)
	}
	for _, t := range a.tabs {
		cmds = append(cmds, t.Update(msg))
	}
	for _, s := range a.screens() {
		cmds = append(cmds, s.Update(msg))
	}
	return tea.Batch(cmds...)
}

func (a *App) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if key == "ctrl+c" {
		if a.composeEdit != nil && a.composeEdit.holdQuit() {
			return nil
		}
		return tea.Quit
	}
	a.flash = ""

	switch {
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
		if over := a.helpOverflow(); over > 0 {
			switch key {
			case "up", "k":
				a.helpTop = max(a.helpTop-1, 0)
				return nil
			case "down", "j":
				a.helpTop = min(a.helpTop+1, over)
				return nil
			case "pgup":
				a.helpTop = max(a.helpTop-a.bodyHeight(), 0)
				return nil
			case "pgdown", "space":
				a.helpTop = min(a.helpTop+a.bodyHeight(), over)
				return nil
			}
		}
		a.help, a.helpTop = false, 0
		return nil
	}
	// Prompts above only open from the tabs or a pane, never over an operation
	// or picker, so they are safe to check before the top screen.
	if s := a.screen(); s != nil {
		return s.handleKey(a, msg)
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
		return a.chooseContext()
	case "C":
		return a.chooseTheme()
	case "o":
		a.openSettings()
	case "I":
		return a.openSystemView()
	case "t":
		if _, ok := a.tabs[a.active].(*ContainersView); ok {
			return a.toggleHostPanel()
		}
		return a.tabs[a.active].HandleKey(key)
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
		if projects, ok := a.tabs[a.active].(*ProjectsView); ok {
			if p := projects.selectedProject(); p != nil {
				return a.openComposeEdit(p.name)
			}
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
		if projects, ok := a.tabs[a.active].(*ProjectsView); ok {
			if p := projects.selectedProject(); p != nil {
				var cmd tea.Cmd
				a.composeDown, cmd = newComposeDown(a.ctx, a.client, p.name, a.width, a.bodyHeight())
				return cmd
			}
		}
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
	a.layout()
	return tea.Batch(a.tabs[i].Focus(), a.watchHost())
}

// slowFrame is how long a frame may take before the debug log notes it.
const slowFrame = 16 * time.Millisecond

func (a *App) View() tea.View {
	began := time.Now()
	v := tea.NewView(a.render())
	if took := time.Since(began); took > slowFrame {
		debuglog.Warn("slow frame", "ms", took.Milliseconds(), "tab", a.tabs[a.active].Title())
	}
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
	switch s := a.screen(); {
	case a.help:
		body = a.helpView()
	case s != nil:
		body, status = s.View(), s.Status()
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

	header, bodyHeight := a.connectionBar(), a.bodyHeight()
	if a.panelShown() {
		header, bodyHeight = strings.Join(a.hostPanel(), "\n"), a.bodyHeight()+1-panelRows
	}
	// Pad the body so the status and footer stay pinned to the bottom.
	if n := bodyHeight - lipgloss.Height(body); n > 0 {
		body += strings.Repeat("\n", n)
	}
	bar, rule := a.tabLines()
	statusRule := render.Dim.Render(strings.Repeat("─", a.width))
	return strings.Join([]string{bar, rule, header, body, statusRule, render.StatusLine(status), ansi.Truncate(a.footer(), a.width, "…")}, "\n")
}

// tabBarCache holds the tab bar and the rule under it, which only change
// with the width, the active tab, a tab title or the theme.
type tabBarCache struct {
	width, active, theme int
	titles               []string
	bar, rule            string
}

// tabLines returns the tab bar and its rule, drawing them only when one of
// their inputs changed: styling each label costs about half a frame.
func (a *App) tabLines() (string, string) {
	c := &a.tabCache
	titles := make([]string, len(a.tabs))
	for i, t := range a.tabs {
		titles[i] = t.Title()
	}
	if c.bar == "" || c.width != a.width || c.active != a.active || c.theme != render.ThemeGeneration() || !slices.Equal(c.titles, titles) {
		*c = tabBarCache{width: a.width, active: a.active, theme: render.ThemeGeneration(), titles: titles, bar: a.tabBar(), rule: a.tabRule()}
	}
	return c.bar, c.rule
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

func (a *App) chooseContext() tea.Cmd {
	if a.actionsInFlight > 0 {
		a.flash, a.flashErr = "wait for the container action to finish before switching contexts", true
		return nil
	}
	return a.openContextPicker()
}

func (a *App) chooseTheme() tea.Cmd {
	if render.ColorDisabled() {
		a.flash, a.flashErr = "colors are off (NO_COLOR or --no-color), so themes don't apply", true
		return nil
	}
	a.openThemePicker()
	return nil
}

func (a *App) footer() string {
	switch {
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
	}
	if s := a.screen(); s != nil {
		return s.Footer()
	}
	if _, ok := a.tabs[a.active].(*EventsView); ok {
		return fitHints(a.width, []string{"tab", "switch", "o", "settings", "/", "filter", "r", "reverse time", "g", "top/follow", "d/enter", "container details", "?", "help", "q", "quit"}, generalHints, 2)
	}

	keys := []string{"tab", "switch"}
	if projects, ok := a.tabs[a.active].(*ProjectsView); ok {
		if projects.selectedProject() != nil {
			keys = append(keys, "e", "edit compose", "D", "down", "u", "update (pull + up)")
		} else {
			keys = append(keys, "u", "update project (pull + up)")
		}
		keys = append(keys, "U", "update all")
	}
	keys = append(keys, "o", "settings", "/", "filter", a.tabs[a.active].sortKeys(), "sort: "+string(a.tabs[a.active].SortKey()))
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
	if _, ok := a.tabs[a.active].(*ContainersView); ok {
		keys = append(keys, "I", "system")
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
		keys = append(keys, "enter", "project logs", "space", "expand/collapse", "S/x/R", "project start/stop/restart")
	}
	keys = append(keys, "?", "help", "q", "quit")
	lowFirst := append(slices.Clone(generalHints), a.tabs[a.active].sortKeys())
	footer := fitHints(a.width, keys, lowFirst, 2)
	if _, ok := a.tabs[a.active].(*ProjectsView); ok && !strings.Contains(ansi.Strip(footer), "o settings") {
		// Narrow Projects footers give U's room to e and o; help lists U.
		footer = fitHints(a.width, keys, append(lowFirst, "U"), 2)
	}
	return footer
}

// generalHints are dropped first when a footer is too wide: they work on
// every tab and the help screen lists them.
var generalHints = []string{"tab", "/"}

// fitHints renders key/label pairs like hints, leaving some out until the
// line fits width. Keys named in lowFirst go first, in that order, then pairs
// from the end, but never the last keepTail pairs.
func fitHints(width int, pairs, lowFirst []string, keepTail int) string {
	total := func() int {
		w := max(len(pairs)/2-1, 0) * 3
		for _, p := range pairs {
			w += ansi.StringWidth(p)
		}
		return w + len(pairs)/2
	}
	for width > 0 && total() > width {
		body := max(len(pairs)-2*keepTail, 0)
		drop := body - 2
		for _, k := range lowFirst {
			if i := slices.Index(pairs[:body], k); i >= 0 && i%2 == 0 {
				drop = i
				break
			}
		}
		if drop < 0 {
			break
		}
		pairs = slices.Delete(slices.Clone(pairs), drop, drop+2)
	}
	return hints(pairs...)
}

func hints(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, keyStyle.Render(pairs[i])+" "+render.Dim.Render(pairs[i+1]))
	}
	return strings.Join(parts, render.Dim.Render(" · "))
}

// helpOverflow is how many help rows don't fit the body, 0 when all do.
func (a *App) helpOverflow() int {
	_, help := a.helpRows()
	if len(help)+3 <= a.bodyHeight() {
		return 0
	}
	return len(help) - max(a.bodyHeight()-2, 1)
}

// helpView shows the keys for the current screen. When they don't fit, the
// rows scroll under the header.
func (a *App) helpView() string {
	title, help := a.helpRows()
	lines := strings.Split(helpTable(title, help, a.width), "\n")
	over := a.helpOverflow()
	if over == 0 {
		return strings.Join(lines, "\n") + "\n\n" + render.Dim.Render("press any key to close")
	}
	top := min(a.helpTop, over)
	rows := lines[1+top : min(1+top+max(a.bodyHeight()-2, 1), len(lines))]
	hint := fmt.Sprintf("↑↓ scroll · %d more below · any other key closes", over-top)
	if top == over {
		hint = "↑↓ scroll · any other key closes"
	}
	return lines[0] + "\n" + strings.Join(rows, "\n") + "\n" + render.Dim.Render(hint)
}

func (a *App) helpRows() (string, [][2]string) {
	if a.logs != nil {
		return "Logs", logsHelp
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
		{"o", "settings: theme, Docker context, editor, mouse, config file location"},
		{"I", "system: the daemon's host, engine versions, usage and docker system df"},
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
	return a.tabs[a.active].Title(), append(append(global, a.tabs[a.active].Help()...), a.customCommandHelp()...)
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

func helpTable(title string, help [][2]string, width int) string {
	rows := make([][]string, len(help))
	for i, h := range help {
		rows[i] = []string{keyStyle.Render(h[0]), h[1]}
	}
	t := render.Table{Columns: []render.Column{{Header: "KEY"}, {Header: title + " — ACTION"}}, Rows: rows, Width: width}
	return t.String()
}
