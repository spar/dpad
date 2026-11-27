package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// openEdit presses e on the selected project and runs the edit up to the
// point where the editor would open.
func openEdit(t *testing.T, a *App) *composeEditView {
	t.Helper()
	a.editor = "vi"
	cmd := a.handleKey(key("e"))
	v := a.composeEdit
	if v == nil || cmd == nil {
		t.Fatal("e on a project row did not start an edit")
	}
	for range 2 {
		_, cmd = a.Update(cmd())
	}
	if v.stage != editEditing || v.copyPath == "" || cmd == nil {
		t.Fatal("the editor was not opened on a copy", v.stage, v.note)
	}
	return v
}

// finishEdit stands in for the editor: it writes content to the copy and
// reports the editor closed.
func finishEdit(t *testing.T, a *App, v *composeEditView, content string) {
	t.Helper()
	if err := os.WriteFile(v.copyPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, cmd := a.Update(composeEditedMsg{view: v})
	if cmd != nil {
		a.Update(cmd())
	}
}

func composeFile(t *testing.T, a *App) string {
	t.Helper()
	return a.client.(*fakeClient).containers[0].Labels[composeProjectLabel+".config_files"]
}

func TestComposeEditSavesAValidEditAndOpensTheUpdate(t *testing.T) {
	a, model, _ := composeUIFixture(t)
	file := composeFile(t, a)
	if err := os.WriteFile(file, []byte("services:\n  web:\n    image: web:latest\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0o644); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ansi.Strip(a.footer()), "e edit compose") {
		t.Fatal("project row footer lacks the edit key", ansi.Strip(a.footer()))
	}
	v := openEdit(t, a)
	if err := os.WriteFile(model, []byte(`{"services":{"web":{"image":"web:1.2"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	finishEdit(t, a, v, "services:\n  web:\n    image: web:1.2\n")
	plain := ansi.Strip(a.composeEdit.View())
	if v.stage != editReview || !strings.Contains(plain, "web: web:latest → web:1.2") || !strings.Contains(plain, "+    image: web:1.2") || !strings.Contains(plain, "valid ✓") {
		t.Fatal("review lacks the change summary or diff", v.stage, v.note, plain)
	}
	v.SetSize(30, 20)
	if path := strings.Split(ansi.Strip(v.View()), "\n")[1]; !strings.HasPrefix(path, "…") || !strings.HasSuffix(path, "compose.yaml  valid ✓") || ansi.StringWidth(path) != 30 {
		t.Fatalf("a long path hid the verdict: %q", path)
	}
	v.SetSize(120, 30)
	if b, _ := os.ReadFile(file); !strings.Contains(string(b), "web:latest") {
		t.Fatal("the original changed before saving")
	}
	_, cmd := a.Update(key("enter"))
	_, cmd = a.Update(cmd())
	if b, _ := os.ReadFile(file); string(b) != "services:\n  web:\n    image: web:1.2\n" {
		t.Fatalf("saved %q", b)
	}
	if info, _ := os.Stat(file); info.Mode().Perm() != 0o644 {
		t.Fatal("save changed the file's permissions", info.Mode())
	}
	if a.composeEdit != nil || a.composeUpdate == nil || a.composeUpdate.selected != "a" || cmd == nil {
		t.Fatal("save and update did not open the update for the project")
	}
	a.composeUpdate.Close()
	if _, err := os.Stat(v.dir); !os.IsNotExist(err) {
		t.Fatal("the editor's copy was left behind")
	}
}

func TestComposeEditKeepsInvalidEditsAndDiscardsOnlyOnSecondEsc(t *testing.T) {
	a, _, _ := composeUIFixture(t)
	file := composeFile(t, a)
	v := openEdit(t, a)
	finishEdit(t, a, v, "INVALID: [\n")
	if v.stage != editInvalid || !strings.Contains(ansi.Strip(v.View()), "mapping values are not allowed") || !strings.Contains(v.Footer(), "keeps your changes") {
		t.Fatal("invalid edit not reported", v.stage, ansi.Strip(v.View()))
	}
	if b, _ := os.ReadFile(file); string(b) != "services: {}" {
		t.Fatal("an invalid edit reached the file")
	}
	if cmd := v.handleKey(a, key("e")); cmd == nil || v.stage != editEditing {
		t.Fatal("e did not reopen the editor")
	}
	if b, _ := os.ReadFile(v.copyPath); string(b) != "INVALID: [\n" {
		t.Fatal("editing again lost the changes")
	}
	finishEdit(t, a, v, "INVALID again\n")
	press(a, "esc")
	if a.composeEdit == nil || !strings.Contains(ansi.Strip(v.View()), "esc again") {
		t.Fatal("one esc discarded the changes")
	}
	press(a, "esc")
	if a.composeEdit != nil || !strings.Contains(a.flash, "discarded") {
		t.Fatal("second esc did not discard", a.flash)
	}
}

func TestComposeEditReportsUnchangedAndConcurrentChanges(t *testing.T) {
	a, _, _ := composeUIFixture(t)
	file := composeFile(t, a)
	v := openEdit(t, a)
	finishEdit(t, a, v, "services: {}")
	if v.stage != editUnchanged || !strings.Contains(v.View(), "No changes") {
		t.Fatal("unchanged edit went on to checking", v.stage)
	}
	v.handleKey(a, key("e"))
	finishEdit(t, a, v, "services:\n  web: {}\n")
	if err := os.WriteFile(file, []byte("changed elsewhere"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, cmd := a.Update(key("s"))
	a.Update(cmd())
	if a.composeEdit == nil || !strings.Contains(ansi.Strip(v.View()), "changed on disk") {
		t.Fatal("a concurrent change was overwritten or not reported", ansi.Strip(v.View()))
	}
	if b, _ := os.ReadFile(file); string(b) != "changed elsewhere" {
		t.Fatal("overwrote the concurrent change")
	}
	press(a, "esc", "esc")
}

func TestComposeEditChoosesAmongSeveralFiles(t *testing.T) {
	a, _, _ := composeUIFixture(t)
	first := composeFile(t, a)
	second := filepath.Join(filepath.Dir(first), "override.yaml")
	if err := os.WriteFile(second, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := a.client.(*fakeClient)
	for _, item := range c.containers {
		item.Labels[composeProjectLabel+".config_files"] = first + "," + second
	}
	a.editor = "vi"
	cmd := a.handleKey(key("e"))
	v := a.composeEdit
	a.Update(cmd())
	if v.stage != editChoosing || !strings.Contains(v.View(), "override.yaml") {
		t.Fatal("several files did not offer a choice", v.stage)
	}
	press(a, "j")
	_, cmd = a.Update(key("enter"))
	a.Update(cmd())
	if v.file != second || v.stage != editEditing {
		t.Fatal("chose the wrong file", v.file)
	}
	v.Close()
}

func TestComposeEditOnContainerRowStillOpensShell(t *testing.T) {
	a, _, _ := composeUIFixture(t)
	press(a, "j")
	a.handleKey(key("e"))
	if a.composeEdit != nil || a.shell == nil {
		t.Fatal("e on a container row did not open a shell")
	}
}

func TestResolveEditorPrefersSettingThenVisualThenEditorThenBuiltin(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "code --wait")
	if got := resolveEditor(""); strings.Join(got, " ") != "code --wait" {
		t.Fatal(got)
	}
	t.Setenv("VISUAL", "hx")
	if got := resolveEditor(""); strings.Join(got, " ") != "hx" {
		t.Fatal(got)
	}
	if got := resolveEditor("nano"); strings.Join(got, " ") != "nano" {
		t.Fatal(got)
	}
	if got := resolveEditor(builtinEditor); !isBuiltinEditor(got) {
		t.Fatal("editor: builtin did not win over $VISUAL", got)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	if got := resolveEditor(" "); !isBuiltinEditor(got) {
		t.Fatal("no editor set did not fall back to the built-in one", got)
	}
}

func TestUnifiedDiffShowsHunksWithContext(t *testing.T) {
	old := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\n"
	edited := "a\nB\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nM\nn\n"
	got := ansi.Strip(strings.Join(unifiedDiff(old, edited, 2), "\n"))
	want := "@@ -1,4 +1,4 @@\n a\n-b\n+B\n c\n d\n@@ -11,3 +11,4 @@\n k\n l\n-m\n+M\n+n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if unifiedDiff("same\n", "same\n", 3) != nil {
		t.Fatal("identical text produced a diff")
	}
}

func TestComposeEditExplainsProjectsItCannotEdit(t *testing.T) {
	a, _, _ := composeUIFixture(t)
	for _, item := range a.client.(*fakeClient).containers {
		delete(item.Labels, composeProjectLabel+".config_files")
	}
	cmd := a.handleKey(key("e"))
	a.Update(cmd())
	if v := a.composeEdit; v.stage != editBlocked || !strings.Contains(v.View(), "labels") {
		t.Fatal("a project without Compose file labels was not explained", v.stage, v.View())
	}
	press(a, "esc")
	if a.composeEdit != nil {
		t.Fatal("esc did not close the explanation")
	}
}
