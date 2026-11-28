package tui

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/spar/dpad/internal/config"
	"github.com/spar/dpad/internal/render"
	"github.com/spar/dpad/internal/sortx"
	"github.com/spar/dpad/internal/theme"
)

// themePicker previews each theme on the whole screen as the cursor moves.
// enter saves the choice to config.yaml; esc puts back the theme that was
// active when it opened.
type themePicker struct {
	list[themeItem]
	app        *App
	original   *theme.Theme
	bodyHeight int
}

type themeItem struct {
	name  string
	theme *theme.Theme
	err   error
}

func (a *App) openThemePicker() {
	dir, _ := theme.Dir()
	var items []themeItem
	for _, t := range theme.All(dir) {
		items = append(items, themeItem{name: t.Name, theme: t.Theme, err: t.Err})
	}
	p := &themePicker{app: a, original: render.Theme()}
	p.list = list[themeItem]{
		columns: []render.Column{{Header: "THEME"}, {Header: "PREVIEW"}, {Header: "DESCRIPTION"}},
		keys:    []sortx.Key{sortx.Name}, key: sortx.Name,
		match: func(themeItem, string) bool { return true },
		sort:  func([]themeItem, sortx.Key, bool) {},
		rows: func(it themeItem) []row {
			if it.err != nil {
				return []row{{key: it.name, cells: []string{it.name, "", render.Error.Render(oneLine(it.err.Error()))}}}
			}
			name := it.name
			if it.theme.Name == p.original.Name && it.theme.Path == p.original.Path {
				name += " (current)"
			}
			return []row{{key: it.name, cells: []string{name, render.Swatch(it.theme), it.theme.Description}}}
		},
		empty: "no themes",
	}
	p.setItems(items, nil)
	p.cur.key = p.original.Name
	a.themePicker = p
	p.SetSize(a.width, a.bodyHeight())
}

// SetSize gives the list at most half the body, leaving the rest to the preview.
func (p *themePicker) SetSize(width, height int) {
	p.bodyHeight = height
	p.list.SetSize(width, min(len(p.items)+1, max(height/2, 3)))
}

func (p *themePicker) Status() string {
	return "Choose a color theme · the screen previews the highlighted one"
}

func (p *themePicker) Footer() string {
	return hints("↑↓/j/k", "preview", "enter", "use and save", "esc", "cancel")
}

func (p *themePicker) Update(tea.Msg) tea.Cmd { return nil }

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func (p *themePicker) selected() themeItem {
	_, keys := p.visible()
	return p.items[p.cur.index(keys)]
}

func (p *themePicker) handleKey(a *App, msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	switch key {
	case "esc", "q":
		render.ApplyTheme(p.original)
		a.themePicker = nil
	case "enter":
		it := p.selected()
		if it.err != nil {
			a.flash, a.flashErr = oneLine(it.err.Error()), true
			return nil
		}
		a.themePicker = nil
		if it.theme.Name == p.original.Name && it.theme.Path == p.original.Path {
			return nil
		}
		if err := config.SaveTheme(it.name); err != nil {
			a.flash, a.flashErr = "theme "+it.name+" for this session; not saved: "+oneLine(err.Error()), true
			return nil
		}
		a.flash = "theme " + it.name + " saved to config.yaml"
		if env := os.Getenv("DPAD_THEME"); env != "" && env != it.name {
			a.flash += " · DPAD_THEME=" + env + " still wins at startup"
		}
	default:
		p.HandleKey(key)
		if it := p.selected(); it.err == nil {
			render.ApplyTheme(it.theme)
		}
	}
	return nil
}

// View shows the themes above the active tab, which previews the highlighted
// theme on real data.
func (p *themePicker) View() string {
	top := p.list.View()
	rest := p.bodyHeight - strings.Count(top, "\n") - 2
	if rest < 1 {
		return top
	}
	tab := p.app.tabs[p.app.active]
	label := " preview: " + tab.Title() + " "
	rule := render.Dim.Render("──" + label + strings.Repeat("─", max(p.width-len(label)-2, 0)))
	lines := strings.Split(tab.View(), "\n")
	lines = lines[:min(len(lines), rest)]
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, p.width, "")
	}
	return top + "\n" + rule + "\n" + strings.Join(lines, "\n")
}
