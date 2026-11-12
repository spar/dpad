package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"dockernext/internal/render"
	"dockernext/internal/sortx"
)

type actionDoneMsg struct {
	text string
	err  error
}

// pending is a container action waiting for y/n confirmation.
type pending struct {
	verb, id, name string
	run            func(context.Context, string) error
}

var (
	tabActive   = lipgloss.NewStyle().Bold(true).Reverse(true).Padding(0, 1)
	tabInactive = lipgloss.NewStyle().Padding(0, 1).Foreground(render.Gray)
	keyStyle    = lipgloss.NewStyle().Bold(true)
)

// App is the full-screen TUI with Containers / Images / Stats tabs.
type App struct {
	ctx           context.Context
	client        dockerClient
	tabs          []tab
	active        int
	width, height int
	filter        textinput.Model // edits the active tab's filter
	filtering     bool
	confirm       *pending
	logs          *LogsView
	details       *DetailsView
	help          bool
	flash         string
	flashErr      bool
}

// Run starts the TUI and blocks until the user quits.
func Run(ctx context.Context, c dockerClient) error {
	fi := textinput.New()
	fi.Prompt = "/"
	tabs := []tab{
		NewContainersView(ctx, c),
		NewImagesView(ctx, c),
		NewStatsView(ctx, c, sortx.CPU, false),
	}
	app := &App{ctx: ctx, client: c, tabs: tabs, filter: fi}
	app.resize(120, 30)
	_, err := tea.NewProgram(app, programOptions(ctx)...).Run()
	return err
}

func (a *App) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(a.tabs)+1)
	for _, t := range a.tabs {
		cmds = append(cmds, t.Init())
	}
	return tea.Batch(append(cmds, a.tabs[a.active].Focus())...)
}

func (a *App) bodyHeight() int { return max(a.height-3, 1) }

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
}

// setFilter sets the active tab's filter and the input showing it.
func (a *App) setFilter(f string) {
	a.filter.SetValue(f)
	a.tabs[a.active].SetFilter(f)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.resize(msg.Width, msg.Height)
		return a, nil
	case tea.KeyPressMsg:
		return a, a.handleKey(msg)
	case actionDoneMsg:
		a.flash, a.flashErr = msg.text, msg.err != nil
		if msg.err != nil {
			a.flash = msg.err.Error()
		}
		return a, func() tea.Msg { return refreshMsg{} }
	}

	var cmds []tea.Cmd
	for _, t := range a.tabs {
		cmds = append(cmds, t.Update(msg))
	}
	if a.logs != nil {
		cmds = append(cmds, a.logs.Update(msg))
	}
	if a.details != nil {
		cmds = append(cmds, a.details.Update(msg))
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
	case a.confirm != nil:
		p := a.confirm
		a.confirm = nil
		if key != "y" && key != "Y" {
			return nil
		}
		a.flash = fmt.Sprintf("%s %s…", p.verb, p.name)
		return func() tea.Msg {
			err := p.run(a.ctx, p.id)
			return actionDoneMsg{text: fmt.Sprintf("%s %s: done", p.verb, p.name), err: err}
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
		if key == "esc" || key == "q" {
			a.logs.Close()
			a.logs = nil
			return nil
		}
		return a.logs.Update(msg)
	case a.details != nil:
		switch key {
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
	}

	switch key {
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
		return func() tea.Msg { return refreshMsg{} }
	case "enter":
		if id, name, ok := a.tabs[a.active].Selected(); ok {
			var cmd tea.Cmd
			a.logs, cmd = NewLogsView(a.ctx, a.client, id, name, a.width, a.bodyHeight())
			return cmd
		}
	case "d":
		if id, name, ok := a.tabs[a.active].Selected(); ok {
			var cmd tea.Cmd
			a.details, cmd = NewDetailsView(a.ctx, a.client, id, name, a.width, a.bodyHeight())
			return cmd
		}
	case "S":
		a.ask("start", a.client.Start)
	case "x":
		a.ask("stop", a.client.Stop)
	case "R":
		a.ask("restart", a.client.Restart)
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
	case a.help:
		body = a.helpView()
	case a.logs != nil:
		body, status = a.logs.View(), a.logs.Status()
	case a.details != nil:
		body, status = a.details.View(), a.details.Status()
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
	return strings.Join([]string{a.tabBar(), body, render.Dim.Render(status), a.footer()}, "\n")
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
	return strings.Join(parts, " ")
}

func (a *App) footer() string {
	switch {
	case a.confirm != nil:
		return render.Warn.Render(fmt.Sprintf("%s container %s? (y/N)", a.confirm.verb, a.confirm.name))
	case a.filtering:
		return a.filter.View()
	case a.logs != nil:
		return hints("esc", "back", "↑↓/pgup/pgdn", "scroll", "←→", "pan", "g/G", "top/follow")
	case a.details != nil:
		return hints("esc", "back", "↑↓/pgup/pgdn", "scroll", "←→", "pan", "g/G", "top/bottom", "ctrl+r", "refresh", "enter", "logs")
	}
	keys := []string{"tab", "switch", "/", "filter", "s/r", "sort: " + string(a.tabs[a.active].SortKey())}
	if _, _, ok := a.tabs[a.active].Selected(); ok {
		keys = append(keys, "d", "details", "enter", "logs", "S/x/R", "start/stop/restart")
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
		{fmt.Sprintf("tab / shift+tab, 1-%d", len(a.tabs)), "switch tabs"},
		{"↑↓ / j k, pgup/pgdn, g/G", "move selection"},
		{"/", "filter (enter keeps, esc clears)"},
		{"enter", "follow logs of selected container"},
		{"d", "inspect selected container's details"},
		{"S / x / R", "start (or unpause) / stop / restart selected container"},
		{"ctrl+r", "refresh now"},
		{"q, ctrl+c", "quit"},
	}
	rows := [][]string{}
	for _, h := range append(global, a.tabs[a.active].Help()...) {
		rows = append(rows, []string{keyStyle.Render(h[0]), h[1]})
	}
	t := render.Table{Columns: []render.Column{{Header: "KEY"}, {Header: a.tabs[a.active].Title() + " — ACTION"}}, Rows: rows}
	return t.String() + "\n\n" + render.Dim.Render("press any key to close")
}
