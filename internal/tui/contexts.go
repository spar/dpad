package tui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"dockernext/internal/docker"
	"dockernext/internal/render"
	"dockernext/internal/sortx"
)

type contextPicker struct {
	list[docker.Context]
	search    textinput.Model
	filtering bool
}

type contextsMsg struct {
	picker *contextPicker
	items  []docker.Context
	err    error
}

func (a *App) openContextPicker() tea.Cmd {
	p := &contextPicker{search: textinput.New()}
	p.search.Prompt = "/"
	p.list = list[docker.Context]{
		columns: []render.Column{{Header: "CONTEXT"}, {Header: "ENDPOINT"}},
		keys:    []sortx.Key{sortx.Name}, key: sortx.Name,
		match: func(c docker.Context, f string) bool { return matches(f, c.Name, c.Endpoint) },
		sort: func(items []docker.Context, _ sortx.Key, _ bool) {
			slices.SortFunc(items, func(a, b docker.Context) int { return strings.Compare(a.Name, b.Name) })
		},
		rows: func(c docker.Context) []row {
			name := c.Name
			if c.Name+" @ "+c.Endpoint == a.daemon.label {
				name += " (current)"
			}
			return []row{{key: c.Name, cells: []string{name, c.Endpoint}}}
		},
		loading: "loading Docker contexts…", empty: "no matching contexts",
	}
	p.SetSize(a.width, a.bodyHeight())
	a.contextPicker = p
	return func() tea.Msg {
		items, err := docker.Contexts()
		return contextsMsg{picker: p, items: items, err: err}
	}
}

func (a *App) updateContextPicker(msg tea.KeyPressMsg) tea.Cmd {
	p := a.contextPicker
	key := msg.String()
	if p.filtering {
		switch key {
		case "enter":
			p.filtering = false
			p.search.Blur()
		case "esc":
			p.filtering = false
			p.search.Blur()
			p.search.SetValue("")
			p.SetFilter("")
		default:
			var cmd tea.Cmd
			p.search, cmd = p.search.Update(msg)
			p.SetFilter(p.search.Value())
			return cmd
		}
		return nil
	}
	switch key {
	case "esc", "q":
		a.contextPicker = nil
	case "/":
		p.filtering = true
		return p.search.Focus()
	case "enter":
		if !p.loaded || p.err != nil {
			return nil
		}
		_, keys := p.visible()
		if len(keys) > 0 {
			a.nextContext = keys[p.cur.index(keys)]
			return tea.Quit
		}
	default:
		return p.HandleKey(key)
	}
	return nil
}
