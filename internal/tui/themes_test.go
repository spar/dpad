package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dpad/internal/render"
	"dpad/internal/theme"
)

func TestThemePickerPreviewsKeepsAndCancels(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DPAD_CONFIG", filepath.Join(dir, "config.yaml"))
	if err := os.MkdirAll(filepath.Join(dir, "themes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "themes", "broken.yaml"), []byte("ui:\n  eror: red\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { render.ApplyTheme(theme.Default()) })

	a := newTestApp()
	press(a, "C")
	if a.themePicker == nil || !strings.Contains(a.render(), "default (current)") || !strings.Contains(a.render(), "preview: Containers") {
		t.Fatal("picker did not open on the current theme with a preview")
	}
	press(a, "down")
	moved := render.Theme().Name
	if moved == "default" {
		t.Fatal("moving did not preview the next theme")
	}
	press(a, "esc")
	if a.themePicker != nil || render.Theme().Name != "default" {
		t.Fatalf("esc left theme %s applied", render.Theme().Name)
	}

	press(a, "C")
	a.themePicker.cur.key = "broken"
	press(a, "enter")
	if a.themePicker == nil || !a.flashErr || !strings.Contains(a.flash, "eror") {
		t.Fatalf("a broken theme was accepted: %q", a.flash)
	}
	a.themePicker.cur.key = "matrix"
	press(a, "k", "j", "enter")
	if a.themePicker != nil || render.Theme().Name != "matrix" || !strings.Contains(a.flash, "saved") {
		t.Fatalf("enter did not keep matrix: theme %s, flash %q", render.Theme().Name, a.flash)
	}
	if saved, err := os.ReadFile(filepath.Join(dir, "config.yaml")); err != nil || string(saved) != "theme: matrix\n" {
		t.Fatalf("choice not saved: %q, %v", saved, err)
	}
	if v := a.View(); v.BackgroundColor == nil {
		t.Fatal("matrix did not set the terminal background")
	}
}
