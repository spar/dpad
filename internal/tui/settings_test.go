package tui

import (
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
	if a.screen() != a.settings {
		t.Fatal("enter on Mouse should leave settings open")
	}
	press(a, "esc")
	if a.settings != nil || a.screen() != nil {
		t.Fatal("esc did not close settings")
	}
}
