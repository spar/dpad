package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"dpad/internal/render"
	"dpad/internal/sortx"
	"dpad/internal/theme"
)

// themePicker previews each theme on the whole screen as the cursor moves.
// esc puts back the theme that was active when it opened.
type themePicker struct {
	list[themeItem]
	original *theme.Theme
}

type themeItem struct {
	name  string
	theme *theme.Theme
	err   error
}

func (a *App) openThemePicker() {
	dir, _ := theme.Dir()
	var items []themeItem
	for _, name := range theme.Names(dir) {
		t, err := theme.Load(name, dir)
		items = append(items, themeItem{name: name, theme: t, err: err})
	}
	p := &themePicker{original: render.Theme()}
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
	a.sizeThemePicker()
}

func (a *App) sizeThemePicker() {
	if p := a.themePicker; p != nil {
		p.SetSize(a.width, min(len(p.items)+1, max(a.bodyHeight()/2, 3)))
	}
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func (p *themePicker) selected() themeItem {
	_, keys := p.visible()
	return p.items[p.cur.index(keys)]
}

func (a *App) updateThemePicker(key string) tea.Cmd {
	p := a.themePicker
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
		if it.theme.Name != p.original.Name || it.theme.Path != p.original.Path {
			a.flash = "theme " + it.name + " for this session · add theme: " + it.name + " to config.yaml to keep it"
		}
	default:
		p.HandleKey(key)
		if it := p.selected(); it.err == nil {
			render.ApplyTheme(it.theme)
		}
	}
	return nil
}

// themePickerView shows the themes above the active tab, which previews the
// highlighted theme on real data.
func (a *App) themePickerView() string {
	p := a.themePicker
	top := p.View()
	rest := a.bodyHeight() - strings.Count(top, "\n") - 2
	if rest < 1 {
		return top
	}
	label := " preview: " + a.tabs[a.active].Title() + " "
	rule := render.Dim.Render("──" + label + strings.Repeat("─", max(a.width-len(label)-2, 0)))
	lines := strings.Split(a.tabs[a.active].View(), "\n")
	lines = lines[:min(len(lines), rest)]
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, a.width, "")
	}
	return top + "\n" + rule + "\n" + strings.Join(lines, "\n")
}
