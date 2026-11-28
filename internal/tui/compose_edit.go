package tui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/spar/dpad/internal/compose"
	"github.com/spar/dpad/internal/render"
)

// composeEditView edits one of a project's Compose files in the person's
// editor. The editor works on a private copy; the original is only replaced
// after Compose accepts the copy and the person confirms the change.
type composeEditView struct {
	ctx      context.Context
	cancel   context.CancelFunc
	client   dockerClient
	env      []string
	editor   string
	name     string
	stage    string
	note     string
	project  compose.Project
	file     string
	choice   int
	original []byte
	dir      string // holds the copy the editor opens
	copyPath string
	edited   []byte
	changes  []string
	discard  bool
	ed       *yamlEditor // the built-in editor while it is open
	vp       viewport.Model
	width    int
	height   int
}

// Stages of an edit, in the order they normally happen.
const (
	editReading   = "reading"
	editChoosing  = "choosing"
	editEditing   = "editing"
	editInDpad    = "editing in dpad"
	editChecking  = "checking"
	editReview    = "review"
	editInvalid   = "invalid"
	editUnchanged = "unchanged"
	editSaving    = "saving"
	editBlocked   = "blocked"
)

type composeEditPreparedMsg struct {
	view    *composeEditView
	project compose.Project
	err     error
}

type composeEditCopiedMsg struct {
	view          *composeEditView
	original      []byte
	dir, copyPath string
	err           error
}

type composeEditedMsg struct {
	view *composeEditView
	err  error
}

type composeEditCheckedMsg struct {
	view   *composeEditView
	edited compose.Project
	err    error
}

type composeEditSavedMsg struct {
	view   *composeEditView
	update bool
	err    error
}

func (a *App) openComposeEdit(name string) tea.Cmd {
	ctx, cancel := context.WithCancel(a.ctx)
	v := &composeEditView{ctx: ctx, cancel: cancel, client: a.client, env: a.client.CLIEnvironment(), editor: a.editor, name: name, stage: editReading, vp: viewport.New()}
	v.vp.SoftWrap = true
	v.SetSize(a.width, a.bodyHeight())
	a.composeEdit = v
	config := a.composeProjects
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(v.ctx, 5*time.Second)
		items, err := v.client.Containers(ctx, true)
		cancel()
		if err != nil {
			return composeEditPreparedMsg{view: v, err: err}
		}
		projects := compose.Discover(items, name)
		if len(projects) == 0 {
			return composeEditPreparedMsg{view: v, err: fmt.Errorf("no containers of project %s are left", name)}
		}
		return composeEditPreparedMsg{view: v, project: prepareComposeProjects(v.ctx, v.client, v.env, projects, items, config)[0]}
	}
}

// resolveEditor returns the editor chosen in settings, else $VISUAL, else
// $EDITOR, else the built-in one. Each may carry flags, such as
// "code --wait".
func resolveEditor(configured string) []string {
	for _, e := range []string{configured, os.Getenv("VISUAL"), os.Getenv("EDITOR")} {
		if fields := strings.Fields(e); len(fields) > 0 {
			return fields
		}
	}
	return []string{builtinEditor}
}

func isBuiltinEditor(editor []string) bool { return len(editor) == 1 && editor[0] == builtinEditor }

func (v *composeEditView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		if v.ed != nil {
			v.ed.Paste(msg.Content)
		}
	case composeEditPreparedMsg:
		if msg.view != v || v.ctx.Err() != nil {
			return nil
		}
		if msg.err != nil {
			v.stage, v.note = editBlocked, connectionError(msg.err)
			return nil
		}
		v.project = msg.project
		if err := compose.Editable(v.project); err != nil {
			v.stage, v.note = editBlocked, err.Error()
			return nil
		}
		if len(v.project.Files) > 1 {
			v.stage = editChoosing
			return nil
		}
		return v.choose(v.project.Files[0])
	case composeEditCopiedMsg:
		if msg.view != v || v.ctx.Err() != nil {
			return nil
		}
		if msg.err != nil {
			v.stage, v.note = editBlocked, msg.err.Error()
			return nil
		}
		v.original, v.dir, v.copyPath = msg.original, msg.dir, msg.copyPath
		return v.edit()
	case composeEditedMsg:
		if msg.view != v || v.ctx.Err() != nil {
			return nil
		}
		return v.afterEdit(msg.err)
	case composeEditCheckedMsg:
		if msg.view != v || v.ctx.Err() != nil {
			return nil
		}
		if msg.err != nil {
			v.stage, v.note = editInvalid, msg.err.Error()
			v.setContent([]string{
				render.Error.Render("docker compose config rejected the edit:"),
				"",
				msg.err.Error(),
				"",
				render.Dim.Render("The file on disk has not been changed."),
			})
			return nil
		}
		v.stage, v.note = editReview, ""
		v.changes = compose.Changes(v.project, msg.edited)
		lines := []string{render.Bold.Render("What changes")}
		for _, line := range v.changes {
			lines = append(lines, "  "+line)
		}
		lines = append(lines, "")
		lines = append(lines, unifiedDiff(string(v.original), string(v.edited), 3)...)
		v.setContent(lines)
		return nil
	case composeEditSavedMsg:
		if msg.view != v || v.ctx.Err() != nil || msg.err == nil {
			return nil
		}
		v.stage, v.note = editReview, msg.err.Error()
		return nil
	}
	return nil
}

// choose copies file somewhere private for the editor, under its own name
// so the editor picks the right syntax.
func (v *composeEditView) choose(file string) tea.Cmd {
	v.file, v.stage = file, editReading
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(v.ctx, 30*time.Second)
		defer cancel()
		original, err := compose.ReadSource(ctx, v.env, v.project, file)
		if err != nil {
			return composeEditCopiedMsg{view: v, err: err}
		}
		dir, err := os.MkdirTemp("", "dpad-edit-")
		if err != nil {
			return composeEditCopiedMsg{view: v, err: err}
		}
		copyPath := filepath.Join(dir, path.Base(filepath.ToSlash(file)))
		if err := os.WriteFile(copyPath, original, 0o600); err != nil {
			_ = os.RemoveAll(dir)
			return composeEditCopiedMsg{view: v, err: err}
		}
		return composeEditCopiedMsg{view: v, original: original, dir: dir, copyPath: copyPath}
	}
}

func (v *composeEditView) edit() tea.Cmd {
	v.stage, v.note, v.discard = editEditing, "", false
	editor := resolveEditor(v.editor)
	if isBuiltinEditor(editor) {
		text, err := os.ReadFile(v.copyPath)
		if err != nil {
			v.stage, v.note = editBlocked, err.Error()
			return nil
		}
		v.stage, v.ed = editInDpad, newYAMLEditor(string(text))
		v.ed.SetSize(v.width, v.height-2)
		return nil
	}
	return tea.ExecProcess(exec.Command(editor[0], append(editor[1:], v.copyPath)...), func(err error) tea.Msg {
		return composeEditedMsg{view: v, err: err}
	})
}

// closeBuiltin leaves the built-in editor, saving its text to the copy when
// asked. Closing it on the first round without changes ends the edit.
func (v *composeEditView) closeBuiltin(a *App, save bool) tea.Cmd {
	if save {
		if err := os.WriteFile(v.copyPath, []byte(v.ed.Value()), 0o600); err != nil {
			v.ed.note = "not saved: " + oneLine(err.Error())
			return nil
		}
	}
	v.ed = nil
	if !save {
		if copied, err := os.ReadFile(v.copyPath); err == nil && bytes.Equal(copied, v.original) {
			v.Close()
			a.composeEdit = nil
			return nil
		}
	}
	return v.afterEdit(nil)
}

// holdQuit keeps ctrl+c from dropping unsaved text in the built-in editor
// until it is pressed twice.
func (v *composeEditView) holdQuit() bool {
	if v.ed == nil || !v.ed.dirty || v.ed.quitArmed {
		return false
	}
	v.ed.note, v.ed.quitArmed = "Unsaved changes: ctrl+c again quits without saving", true
	return true
}

func (v *composeEditView) afterEdit(err error) tea.Cmd {
	if err != nil {
		v.stage, v.note = editInvalid, "the editor failed: "+err.Error()
		v.setContent([]string{render.Error.Render(v.note), "", render.Dim.Render("The file on disk has not been changed.")})
		return nil
	}
	edited, err := os.ReadFile(v.copyPath)
	if err != nil {
		v.stage, v.note = editBlocked, err.Error()
		return nil
	}
	v.edited = edited
	if bytes.Equal(edited, v.original) {
		v.stage = editUnchanged
		return nil
	}
	v.stage = editChecking
	project, file, env := v.project, v.file, v.env
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(v.ctx, 30*time.Second)
		defer cancel()
		after, err := compose.Check(ctx, env, project, file, edited)
		return composeEditCheckedMsg{view: v, edited: after, err: err}
	}
}

func (v *composeEditView) save(update bool) tea.Cmd {
	v.stage, v.note = editSaving, ""
	project, file, original, edited, env := v.project, v.file, v.original, v.edited, v.env
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(v.ctx, 30*time.Second)
		defer cancel()
		return composeEditSavedMsg{view: v, update: update, err: compose.Replace(ctx, env, project, file, original, edited)}
	}
}

func (v *composeEditView) setContent(lines []string) {
	v.vp.SetContent(strings.Join(lines, "\n"))
	v.vp.GotoTop()
}

func (v *composeEditView) Close() {
	v.cancel()
	if v.dir != "" {
		_ = os.RemoveAll(v.dir)
	}
}

func (v *composeEditView) SetSize(width, height int) {
	v.width, v.height = width, height
	v.vp.SetWidth(width)
	v.vp.SetHeight(max(height-3, 1))
	if v.ed != nil {
		v.ed.SetSize(width, height-2)
	}
}

func (v *composeEditView) title() string {
	where := "local"
	if v.project.Remote {
		where = "SSH host"
	}
	return ansi.Truncate(render.Bold.Render("Edit Compose file · "+v.name+" · "+where), v.width, "…")
}

func (v *composeEditView) View() string {
	lines := []string{v.title()}
	if v.file != "" {
		// The verdict stays visible however long the path is.
		verdict := ""
		switch v.stage {
		case editReview:
			verdict = "  " + render.Success.Render("valid ✓")
		case editInvalid:
			verdict = "  " + render.Error.Render("invalid ✗")
		}
		file := v.file
		if over := ansi.StringWidth(file) + ansi.StringWidth(verdict) - v.width; over > 0 {
			file = ansi.TruncateLeft(file, over+1, "…")
		}
		lines = append(lines, file+verdict)
	}
	switch v.stage {
	case editReading:
		lines = append(lines, render.Dim.Render("Reading the project's Compose configuration…"))
	case editChoosing:
		lines = append(lines, render.Dim.Render("The project uses several Compose files. Choose one to edit."), "")
		for i, file := range v.project.Files {
			row := "  " + file
			if i == v.choice {
				row = render.Selected.Render("❯ " + file)
			}
			lines = append(lines, ansi.Truncate(row, v.width, "…"))
		}
	case editEditing:
		lines = append(lines, render.Dim.Render("Waiting for the editor to close…"))
	case editInDpad:
		lines = append(lines, strings.Split(v.ed.View(), "\n")...)
	case editChecking:
		lines = append(lines, render.Dim.Render("Checking the edit with docker compose config…"))
	case editSaving:
		lines = append(lines, render.Dim.Render("Saving…"))
	case editUnchanged:
		lines = append(lines, "", "No changes. The file was saved without edits or the editor was closed without saving.")
	case editBlocked:
		lines = append(lines, "", render.Error.Render(oneLine(v.note)))
	case editReview, editInvalid:
		note := ""
		switch {
		case v.discard:
			note = render.Warn.Render("Press esc again to discard your changes.")
		case v.stage == editReview && v.note != "":
			note = render.Error.Render(oneLine(v.note))
		}
		lines = append(lines, ansi.Truncate(note, v.width, "…"))
		lines = append(lines, strings.Split(v.vp.View(), "\n")...)
	}
	return strings.Join(lines[:min(len(lines), max(v.height, 1))], "\n")
}

func (v *composeEditView) Status() string {
	return "Edit Compose file · " + v.stage
}

func (v *composeEditView) Footer() string {
	switch v.stage {
	case editInDpad:
		return fitHints(v.width, []string{"ctrl+s", "save", "esc", "close", "ctrl+f", "find", "ctrl+n/p", "next/previous", "ctrl+g", "go to line", "ctrl+home/end", "top/bottom", "ctrl+z/y", "undo/redo", "ctrl+k", "delete line"}, []string{"ctrl+k", "ctrl+n/p", "ctrl+z/y"}, 0)
	case editChoosing:
		return hints("↑↓", "choose file", "enter", "edit", "esc", "cancel")
	case editReview:
		return ansi.Truncate(hints("enter", "save + update", "s", "save only", "e", "edit again", "esc", "discard", "↑↓", "scroll"), v.width, "…")
	case editInvalid:
		return hints("e", "edit again (keeps your changes)", "esc", "discard", "↑↓", "scroll")
	case editUnchanged:
		return hints("e", "edit again", "esc", "close")
	case editBlocked:
		return hints("esc", "close")
	}
	return hints("esc", "cancel")
}

func (v *composeEditView) handleKey(a *App, msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if v.stage == editInDpad {
		switch v.ed.HandleKey(msg) {
		case editorSave:
			return v.closeBuiltin(a, true)
		case editorClose:
			return v.closeBuiltin(a, false)
		}
		return nil
	}
	if v.stage == editEditing || v.stage == editSaving {
		return nil
	}
	if closeKey(key) {
		changed := v.stage == editReview || v.stage == editInvalid
		if changed && !v.discard {
			v.discard = true
			return nil
		}
		v.Close()
		a.composeEdit = nil
		if changed {
			a.flash = "discarded changes to " + filepath.Base(v.file)
		}
		return nil
	}
	v.discard = false
	switch v.stage {
	case editChoosing:
		switch key {
		case "up", "k":
			v.choice = max(v.choice-1, 0)
		case "down", "j":
			v.choice = min(v.choice+1, len(v.project.Files)-1)
		case "enter":
			return v.choose(v.project.Files[v.choice])
		}
	case editReview:
		switch key {
		case "enter":
			return v.save(true)
		case "s":
			return v.save(false)
		case "e":
			return v.edit()
		}
	case editInvalid, editUnchanged:
		if key == "e" {
			return v.edit()
		}
	}
	if v.stage == editReview || v.stage == editInvalid {
		var cmd tea.Cmd
		v.vp, cmd = v.vp.Update(msg)
		return cmd
	}
	return nil
}

// composeEditSaved closes the edit once its file is saved and, when asked,
// opens the update preview for the project.
func (a *App) composeEditSaved(msg composeEditSavedMsg) tea.Cmd {
	v := a.composeEdit
	if v == nil || v != msg.view {
		return nil
	}
	if msg.err != nil {
		return v.Update(msg)
	}
	v.Close()
	a.composeEdit = nil
	a.flash = "saved " + v.file
	if !msg.update {
		return nil
	}
	if a.actionsInFlight > 0 {
		a.flash, a.flashErr = "saved "+v.file+"; wait for container actions to finish, then press u to update", true
		return nil
	}
	return a.startComposeUpdate(v.name)
}
