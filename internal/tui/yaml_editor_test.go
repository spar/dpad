package tui

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// edKey builds a key press for the editor: a named key, a ctrl+ or shift+
// combination, or text to type.
func edKey(k string) tea.KeyPressMsg {
	named := map[string]rune{
		"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
		"home": tea.KeyHome, "end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "backspace": tea.KeyBackspace,
		"delete": tea.KeyDelete,
	}
	var mod tea.KeyMod
	if rest, ok := strings.CutPrefix(k, "ctrl+"); ok {
		mod, k = tea.ModCtrl, rest
	} else if rest, ok := strings.CutPrefix(k, "shift+"); ok {
		mod, k = tea.ModShift, rest
	}
	if code, ok := named[k]; ok {
		return tea.KeyPressMsg{Code: code, Mod: mod}
	}
	if mod != 0 {
		return tea.KeyPressMsg{Code: rune(k[0]), Mod: mod}
	}
	return tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
}

func edPress(e *yamlEditor, keys ...string) editorAction {
	var last editorAction
	for _, k := range keys {
		last = e.HandleKey(edKey(k))
	}
	return last
}

func edType(e *yamlEditor, text string) {
	for _, r := range text {
		e.HandleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func newTestEditor(text string) *yamlEditor {
	e := newYAMLEditor(text)
	e.SetSize(60, 10)
	return e
}

func TestYAMLEditorKeepsLineEndingsAndFinalNewline(t *testing.T) {
	for _, text := range []string{"a: 1\nb: 2\n", "a: 1\r\nb: 2\r\n", "a: 1", ""} {
		e := newTestEditor(text)
		if got := e.Value(); got != text {
			t.Errorf("Value() = %q, want %q", got, text)
		}
		if e.dirty {
			t.Errorf("%q is dirty before any edit", text)
		}
	}
	e := newTestEditor("a: 1\r\n")
	edPress(e, "end", "enter")
	edType(e, "b: 2")
	if got := e.Value(); got != "a: 1\r\nb: 2\r\n" {
		t.Fatalf("Value() = %q", got)
	}
}

func TestYAMLEditorIndentsAndNeverTypesTabs(t *testing.T) {
	e := newTestEditor("services:\n")
	edPress(e, "end", "enter")
	edType(e, "web:")
	edPress(e, "enter")
	edType(e, "image: nginx")
	edPress(e, "enter", "tab")
	edType(e, "x")
	if got, want := e.Value(), "services:\n  web:\n    image: nginx\n      x\n"; got != want {
		t.Fatalf("Value() = %q, want %q", got, want)
	}
	edPress(e, "shift+tab")
	if got := string(e.lines[e.row]); got != "    x" {
		t.Fatalf("shift+tab left %q", got)
	}
}

func TestYAMLEditorUndoGroupsTypingAndRedoes(t *testing.T) {
	e := newTestEditor("name: a\n")
	edPress(e, "end")
	edType(e, "bc")
	edPress(e, "enter")
	edType(e, "x: 1")
	edPress(e, "ctrl+z")
	if got := e.Value(); got != "name: abc\n" && got != "name: abc\n\n" {
		t.Fatalf("one undo left %q", got)
	}
	edPress(e, "ctrl+z", "ctrl+z")
	if got := e.Value(); got != "name: a\n" || e.dirty {
		t.Fatalf("undoing everything left %q, dirty %v", got, e.dirty)
	}
	edPress(e, "ctrl+y", "ctrl+y", "ctrl+y")
	if got := e.Value(); got != "name: abc\nx: 1\n" {
		t.Fatalf("redo left %q", got)
	}
}

func TestYAMLEditorMovesToTopBottomAndLine(t *testing.T) {
	var lines []string
	for i := range 50 {
		lines = append(lines, "k"+strings.Repeat("x", i%7)+": 1")
	}
	e := newTestEditor(strings.Join(lines, "\n") + "\n")
	edPress(e, "ctrl+end")
	if e.row != 49 || e.top != 49-e.height+1 {
		t.Fatalf("ctrl+end: row %d top %d", e.row, e.top)
	}
	edPress(e, "ctrl+home")
	if e.row != 0 || e.col != 0 || e.top != 0 {
		t.Fatalf("ctrl+home: row %d col %d top %d", e.row, e.col, e.top)
	}
	edPress(e, "ctrl+g")
	edType(e, "20")
	edPress(e, "enter")
	if e.row != 19 || e.col != 0 || e.prompt != "" {
		t.Fatalf("go to line 20: row %d col %d", e.row, e.col)
	}
	edPress(e, "ctrl+g")
	edType(e, "$")
	edPress(e, "enter")
	if e.row != 49 {
		t.Fatalf("go to line $: row %d", e.row)
	}
	if !strings.Contains(ansi.Strip(e.View()), "Ln 50, Col 1") {
		t.Fatal("status line lacks the position")
	}
}

func TestYAMLEditorFindsCaseInsensitivelyAndWraps(t *testing.T) {
	e := newTestEditor("a: Python\nb: x\nc: python python\n")
	edPress(e, "ctrl+f")
	edType(e, "PYTHON")
	edPress(e, "enter")
	if e.row != 0 || e.col != 3 {
		t.Fatalf("first match at %d:%d", e.row, e.col)
	}
	edPress(e, "ctrl+n")
	if e.row != 2 || e.col != 3 {
		t.Fatalf("second match at %d:%d", e.row, e.col)
	}
	edPress(e, "ctrl+n")
	if e.row != 2 || e.col != 10 {
		t.Fatalf("third match at %d:%d", e.row, e.col)
	}
	edPress(e, "ctrl+n")
	if e.row != 0 || e.col != 3 || !strings.Contains(e.note, "wrapped") {
		t.Fatalf("wrap: %d:%d note %q", e.row, e.col, e.note)
	}
	edPress(e, "ctrl+p")
	if e.row != 2 || e.col != 10 {
		t.Fatalf("previous match at %d:%d", e.row, e.col)
	}
	edPress(e, "ctrl+f", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace")
	edType(e, "nothing")
	edPress(e, "enter")
	if !strings.Contains(e.note, "no match") {
		t.Fatalf("note %q", e.note)
	}
}

func TestYAMLEditorPastesLinesInOneStep(t *testing.T) {
	e := newTestEditor("a: 1\n")
	edPress(e, "end", "enter")
	e.Paste("b: 2\r\nc: 3")
	if got := e.Value(); got != "a: 1\nb: 2\nc: 3\n" || e.row != 2 || e.col != 4 {
		t.Fatalf("paste left %q at %d:%d", got, e.row, e.col)
	}
	edPress(e, "ctrl+z")
	if got := e.Value(); got != "a: 1\n\n" {
		t.Fatalf("undoing the paste left %q", got)
	}
}

func TestYAMLEditorAsksBeforeDroppingChanges(t *testing.T) {
	e := newTestEditor("a: 1\n")
	if edPress(e, "esc") != editorClose {
		t.Fatal("esc without changes did not close")
	}
	edType(e, "x")
	if edPress(e, "esc") != editorNone || !strings.Contains(e.note, "esc again") {
		t.Fatal("esc with changes closed at once")
	}
	if edPress(e, "esc") != editorClose {
		t.Fatal("second esc did not close")
	}
	if edPress(e, "ctrl+s") != editorSave {
		t.Fatal("ctrl+s did not save")
	}
}

func TestYAMLEditorMarksTheLineYAMLRejects(t *testing.T) {
	e := newTestEditor("services:\n  web:\n    image: a\n")
	if e.lintErr != "" {
		t.Fatal("valid YAML reported", e.lintErr)
	}
	edPress(e, "down", "down", "down")
	edPress(e, "home")
	edType(e, "   x: 1")
	if e.lintErr == "" || e.lintRow < 0 {
		t.Fatal("broken indentation not reported")
	}
	if !strings.Contains(ansi.Strip(e.View()), "YAML: ") {
		t.Fatal("status line lacks the YAML error")
	}
}

func TestYAMLEditorScrollsLongLinesSideways(t *testing.T) {
	long := "command: " + strings.Repeat("abcdefghij", 12)
	e := newTestEditor(long + "\nb: 1\n")
	edPress(e, "end")
	view := strings.Split(ansi.Strip(e.View()), "\n")
	if !strings.HasSuffix(strings.TrimRight(view[0], " "), "hij") {
		t.Fatalf("the end of a long line is not shown: %q", view[0])
	}
	for _, l := range view {
		if ansi.StringWidth(l) > 60 {
			t.Fatalf("line wider than the editor: %q", l)
		}
	}
	edPress(e, "home")
	if first := ansi.Strip(strings.Split(e.View(), "\n")[0]); !strings.Contains(first, "command:") || !strings.HasSuffix(first, "…") {
		t.Fatalf("home did not scroll back or mark the cut: %q", first)
	}
}

func TestHighlightYAMLKeepsTheText(t *testing.T) {
	for _, line := range []string{
		"services:",
		"  - \"8080:80\" # web",
		"    image: ${IMAGE:-nginx}:latest",
		"x-python: &python",
		"  <<: *python",
		"    test: [\"CMD\", \"wget\", 'x # y']",
		"    retries: 3",
		"# comment: here",
		"key: 'it''s' # quoted",
		"    command: >-",
	} {
		if got := ansi.Strip(highlightYAML(line, "py")); got != line {
			t.Errorf("highlight changed %q to %q", line, got)
		}
	}
}

func TestComposeEditUsesTheBuiltinEditorWhenNoneIsSet(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	a, model, _ := composeUIFixture(t)
	file := composeFile(t, a)
	if err := os.WriteFile(file, []byte("services:\n  web:\n    image: web:latest\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := a.handleKey(key("e"))
	v := a.composeEdit
	for range 2 {
		_, cmd = a.Update(cmd())
	}
	if v.stage != editInDpad || v.ed == nil || cmd != nil {
		t.Fatal("the built-in editor did not open", v.stage, v.note)
	}
	if !strings.Contains(ansi.Strip(a.render()), "image: web:latest") || !strings.Contains(ansi.Strip(a.footer()), "ctrl+s save") {
		t.Fatal("the editor is not shown", ansi.Strip(a.render()))
	}

	if a.handleKey(edKey("ctrl+g")); v.ed.prompt != "line" {
		t.Fatal("ctrl+g did not reach the editor")
	}
	a.handleKey(edKey("3"))
	a.handleKey(edKey("enter"))
	for range 3 {
		a.handleKey(edKey("ctrl+right"))
	}
	for range len("latest") {
		a.handleKey(edKey("backspace"))
	}
	a.Update(tea.PasteMsg{Content: "1.2"})
	if a.handleKey(edKey("ctrl+c")); !strings.Contains(v.ed.note, "ctrl+c again") {
		t.Fatal("ctrl+c would drop unsaved changes without asking")
	}

	if err := os.WriteFile(model, []byte(`{"services":{"web":{"image":"web:1.2"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if cmd = a.handleKey(edKey("ctrl+s")); cmd == nil || v.stage != editChecking {
		t.Fatal("ctrl+s did not check the edit", v.stage)
	}
	a.Update(cmd())
	if b, _ := os.ReadFile(v.copyPath); string(b) != "services:\n  web:\n    image: web:1.2\n" {
		t.Fatalf("copy = %q", b)
	}
	if v.stage != editReview || !strings.Contains(ansi.Strip(v.View()), "web: web:latest → web:1.2") {
		t.Fatal("the edit did not reach the review", v.stage, ansi.Strip(v.View()))
	}
	if b, _ := os.ReadFile(file); !strings.Contains(string(b), "web:latest") {
		t.Fatal("the original changed before saving")
	}

	a.handleKey(key("e"))
	if v.stage != editInDpad || !strings.Contains(v.ed.Value(), "web:1.2") {
		t.Fatal("e did not reopen the editor with the changes")
	}
	v.Close()
}

func TestComposeEditClosesWhenTheBuiltinEditorLeavesNoChanges(t *testing.T) {
	a, _, _ := composeUIFixture(t)
	a.editor = builtinEditor
	t.Setenv("EDITOR", "vim")
	cmd := a.handleKey(key("e"))
	for range 2 {
		_, cmd = a.Update(cmd())
	}
	if a.composeEdit == nil || a.composeEdit.stage != editInDpad {
		t.Fatal("editor: builtin did not win over $EDITOR")
	}
	dir := a.composeEdit.dir
	press(a, "esc")
	if a.composeEdit != nil {
		t.Fatal("esc without changes left the edit open")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("the copy was left behind")
	}
}
