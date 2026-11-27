package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"dpad/internal/render"
	"dpad/internal/theme"
)

func TestSettingsShowsValuesAndOpensPickers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DPAD_CONFIG", filepath.Join(dir, "config.yaml"))
	t.Cleanup(func() { render.ApplyTheme(theme.Default()) })

	a := newTestApp()
	a.resize(400, 30) // room for a long temp path, as on macOS
	a.daemon.label = "remote @ ssh://box"
	press(a, "o")
	if a.settings == nil {
		t.Fatal("o did not open settings")
	}
	view := ansi.Strip(a.render())
	for _, want := range []string{"Theme", "default", "Docker context", "remote", filepath.Join(dir, "config.yaml"), "enter change"} {
		if !strings.Contains(view, want) {
			t.Errorf("settings view lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(a.settings.View(), "ssh://box") {
		t.Error("the context value should be its name only")
	}

	press(a, "enter")
	if a.screen() != a.themePicker || a.themePicker == nil {
		t.Fatal("enter on Theme did not open the theme picker")
	}
	press(a, "esc")
	if a.screen() != a.settings {
		t.Fatal("esc in the theme picker did not return to settings")
	}
	press(a, "enter")
	a.themePicker.cur.key = "matrix"
	press(a, "enter")
	if a.screen() != a.settings || !strings.Contains(a.render(), "matrix") {
		t.Fatalf("settings did not show the saved theme:\n%s", a.render())
	}

	press(a, "j")
	a.actionsInFlight = 1
	press(a, "enter")
	if a.contextPicker != nil || !a.flashErr {
		t.Fatal("context switch should wait for container actions")
	}
	a.actionsInFlight = 0
	press(a, "enter")
	if a.screen() != a.contextPicker || a.contextPicker == nil {
		t.Fatal("enter on Docker context did not open the context picker")
	}
	press(a, "esc")

	press(a, "j", "enter")
	if a.screen() != a.editorPicker || a.editorPicker == nil {
		t.Fatal("enter on Editor did not open the editor picker")
	}
	press(a, "esc")

	press(a, "j", "enter")
	if a.screen() != a.settings || a.containers().graph != render.GraphBraille {
		t.Fatal("enter on CPU graph should cycle it in place")
	}

	press(a, "j", "enter")
	if a.screen() != a.settings {
		t.Fatal("enter on Mouse should leave settings open")
	}
	press(a, "esc")
	if a.settings != nil || a.screen() != nil {
		t.Fatal("esc did not close settings")
	}
}

func TestEditorPickerOffersInstalledEditorsAndSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("DPAD_CONFIG", path)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "vim")
	installed := map[string]bool{"nano": true, "vim": true, "code": true}
	defer func(orig func(string) (string, error)) { lookPath = orig }(lookPath)
	lookPath = func(name string) (string, error) {
		if installed[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}

	a := newTestApp()
	a.resize(200, 30)
	press(a, "o", "j", "j", "enter")
	view := ansi.Strip(a.render())
	for _, want := range []string{"$VISUAL / $EDITOR (current)", "now vim · from $EDITOR", "nano", "code --wait"} {
		if !strings.Contains(view, want) {
			t.Errorf("editor picker lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "emacs") {
		t.Error("offered an editor that is not installed")
	}

	press(a, "j", "enter")
	if a.editor != "nano" || a.screen() != a.settings {
		t.Fatal("choosing nano did not apply it and return to settings", a.editor)
	}
	if got, _ := os.ReadFile(path); string(got) != "editor: nano\n" {
		t.Fatalf("config = %q", got)
	}
	if !strings.Contains(ansi.Strip(a.render()), "nano") {
		t.Error("settings does not show the chosen editor")
	}

	a.editor = "my-editor --flag"
	press(a, "enter")
	if !strings.Contains(ansi.Strip(a.render()), "my-editor --flag (current)") {
		t.Errorf("an editor set by hand is not listed:\n%s", a.render())
	}
	press(a, "home", "enter")
	if a.editor != builtinEditor || !strings.Contains(ansi.Strip(a.render()), "Editor") {
		t.Fatal("choosing built-in did not apply it", a.editor)
	}
	if got, _ := os.ReadFile(path); string(got) != "editor: builtin\n" {
		t.Fatalf("config = %q", got)
	}
	press(a, "enter", "home", "j", "enter")
	if a.editor != "" {
		t.Fatal("choosing $VISUAL / $EDITOR did not clear the setting")
	}
	if got, _ := os.ReadFile(path); string(got) != "" {
		t.Fatalf("config = %q, want the editor line removed", got)
	}
}
