package tui

import (
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"

	"dpad/internal/config"
	"dpad/internal/render"
	"dpad/internal/sortx"
)

// knownEditors are offered in the editor picker when found on PATH. Editors
// that open a window need their wait flag, or the edit would end at once.
var knownEditors = []string{
	"nano", "vim", "nvim", "vi", "emacs", "micro", "hx",
	"code --wait", "cursor --wait", "zed --wait", "subl --wait", "notepad",
}

var lookPath = exec.LookPath

// editorPicker chooses the editor for Compose edits and saves it to
// config.yaml.
type editorPicker struct {
	list[editorItem]
}

type editorItem struct {
	command string // empty for $VISUAL/$EDITOR
	name    string
	note    string
}

func (a *App) openEditorPicker() tea.Cmd {
	items := []editorItem{
		{command: builtinEditor, name: "built-in", note: "in dpad: YAML colors, find, go to line"},
		{name: "$VISUAL / $EDITOR", note: "now " + editorLabel(resolveEditor("")) + " · " + envEditorSource()},
	}
	configuredFound := a.editor == "" || a.editor == builtinEditor
	for _, command := range knownEditors {
		path, err := lookPath(strings.Fields(command)[0])
		if err != nil {
			continue
		}
		items = append(items, editorItem{command: command, name: command, note: path})
		configuredFound = configuredFound || command == a.editor
	}
	if !configuredFound {
		items = append(items, editorItem{command: a.editor, name: a.editor, note: "from config.yaml"})
	}
	p := &editorPicker{}
	p.list = list[editorItem]{
		columns: []render.Column{{Header: "EDITOR"}, {Header: "DETAILS"}},
		keys:    []sortx.Key{sortx.Name}, key: sortx.Name,
		match: func(editorItem, string) bool { return true },
		sort:  func([]editorItem, sortx.Key, bool) {},
		rows: func(it editorItem) []row {
			name := it.name
			if it.command == a.editor {
				name += " (current)"
			}
			return []row{{key: it.name, cells: []string{name, render.Dim.Render(it.note)}}}
		},
	}
	p.setItems(items, nil)
	for _, it := range items {
		if it.command == a.editor {
			p.cur.key = it.name
		}
	}
	p.SetSize(a.width, a.bodyHeight())
	a.editorPicker = p
	return nil
}

// envEditorSource names where the editor comes from when none is chosen.
func envEditorSource() string {
	switch {
	case strings.TrimSpace(os.Getenv("VISUAL")) != "":
		return "from $VISUAL"
	case strings.TrimSpace(os.Getenv("EDITOR")) != "":
		return "from $EDITOR"
	}
	return "neither is set"
}

func (a *App) editorName() string {
	if a.editor != "" {
		return editorLabel(strings.Fields(a.editor))
	}
	return editorLabel(resolveEditor("")) + " " + render.Dim.Render("· "+envEditorSource())
}

func editorLabel(editor []string) string {
	if isBuiltinEditor(editor) {
		return "built-in"
	}
	return strings.Join(editor, " ")
}

func (p *editorPicker) Status() string {
	return "Choose the editor for Compose edits · other commands: editor: in config.yaml"
}

func (p *editorPicker) Footer() string {
	return hints("↑↓/j/k", "move", "enter", "use and save", "esc", "cancel")
}

func (p *editorPicker) Update(tea.Msg) tea.Cmd { return nil }

func (p *editorPicker) handleKey(a *App, msg tea.KeyPressMsg) tea.Cmd {
	switch key := msg.String(); key {
	case "esc", "q":
		a.editorPicker = nil
	case "enter":
		_, keys := p.visible()
		it := p.items[p.cur.index(keys)]
		a.editorPicker = nil
		if it.command == a.editor {
			return nil
		}
		a.editor = it.command
		label := it.name
		if it.command != "" {
			label = editorLabel(strings.Fields(it.command))
		}
		if err := config.SaveEditor(it.command); err != nil {
			a.flash, a.flashErr = "editor "+label+" for this session; not saved: "+oneLine(err.Error()), true
			return nil
		}
		a.flash = "editor " + label + " saved to config.yaml"
	default:
		return p.HandleKey(key)
	}
	return nil
}
