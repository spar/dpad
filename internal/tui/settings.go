package tui

import (
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"dpad/internal/config"
	"dpad/internal/render"
	"dpad/internal/sortx"
)

// settingsMenu lists what can be changed from the TUI, so the keys for it
// can be found from one footer hint. It stays open under the pickers it
// opens, so esc there comes back here.
type settingsMenu struct {
	list[setting]
}

type setting struct {
	name, key string
	value     func() string
	open      func() tea.Cmd // nil when the setting is changed in config.yaml
}

func (a *App) openSettings() {
	m := &settingsMenu{}
	m.list = list[setting]{
		columns: []render.Column{{Header: "SETTING"}, {Header: "KEY"}, {Header: "VALUE"}},
		keys:    []sortx.Key{sortx.Name}, key: sortx.Name,
		match: func(setting, string) bool { return true },
		sort:  func([]setting, sortx.Key, bool) {},
		rows: func(s setting) []row {
			return []row{{key: s.name, cells: []string{s.name, keyStyle.Render(s.key), s.value()}}}
		},
	}
	m.setItems([]setting{
		{name: "Theme", key: "C", value: func() string { return render.Theme().Name }, open: a.chooseTheme},
		{name: "Docker context", key: "c", value: a.contextName, open: a.chooseContext},
		{name: "Editor", value: a.editorName, open: a.openEditorPicker},
		{name: "CPU graph", value: func() string {
			return a.containers().graph + " " + render.Dim.Render("· Containers tab; enter cycles "+strings.Join(render.GraphStyles, "/"))
		}, open: a.cycleCPUGraph},
		{name: "Icons", value: a.iconsValue, open: func() tea.Cmd { a.cycleIcons(); return nil }},
		{name: "Mouse", value: func() string {
			if a.mouse {
				return "on " + render.Dim.Render("· set mouse: false in config.yaml")
			}
			return "off " + render.Dim.Render("· set mouse: true in config.yaml")
		}},
		{name: "Config file", value: configPath},
	}, nil)
	m.SetSize(a.width, a.bodyHeight())
	a.settings = m
}

// defaultCPUGraph is the Containers CPU graph when config.yaml names none.
const defaultCPUGraph = render.GraphBraille

func (a *App) containers() *ContainersView {
	for _, t := range a.tabs {
		if v, ok := t.(*ContainersView); ok {
			return v
		}
	}
	return nil
}

func (a *App) cycleCPUGraph() tea.Cmd {
	v := a.containers()
	i := slices.Index(render.GraphStyles, v.graph)
	style := render.GraphStyles[(i+1)%len(render.GraphStyles)]
	v.setGraph(style)
	if err := config.SaveCPUGraph(style); err != nil {
		a.flash, a.flashErr = "CPU graph: "+style+" for now; could not save: "+oneLine(err.Error()), true
	}
	a.settings.rowCache = nil
	return v.watchUsage()
}

func (a *App) contextName() string {
	name, _, _ := strings.Cut(a.daemon.label, " @ ")
	if name == "" {
		return "docker"
	}
	return name
}

func configPath() string {
	path, err := config.Path()
	if err != nil {
		return render.Error.Render(oneLine(err.Error()))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

func (m *settingsMenu) Status() string { return "Settings" }

func (m *settingsMenu) Footer() string {
	return hints("↑↓/j/k", "move", "enter", "change", "esc", "close")
}

func (m *settingsMenu) Update(tea.Msg) tea.Cmd { return nil }

func (m *settingsMenu) handleKey(a *App, msg tea.KeyPressMsg) tea.Cmd {
	switch key := msg.String(); key {
	case "esc", "q", "o":
		a.settings = nil
	case "enter":
		_, keys := m.visible()
		if s := m.items[m.cur.index(keys)]; s.open != nil {
			return s.open()
		}
	default:
		return m.HandleKey(key)
	}
	return nil
}
