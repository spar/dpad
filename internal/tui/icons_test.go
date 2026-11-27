package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"

	"dpad/internal/config"
	"dpad/internal/render"
)

func TestNerdFontBuiltIn(t *testing.T) {
	for name, want := range map[string]bool{
		"ghostty 1.1.3":                    true,
		"ghostty":                          true,
		"WezTerm 20240203-110809-5046fc22": true,
		"WezTerm":                          true,
		"kitty(0.36.0)":                    true,
		"kitty(0.42.1)":                    true,
		"kitty(1.0.0)":                     true,
		"kitty(0.35.2)":                    false,
		"kitty(0.28.1)":                    false,
		"tmux 3.4":                         false,
		"iTerm.app":                        false,
		"Apple_Terminal":                   false,
		"":                                 false,
	} {
		if got := nerdFontBuiltIn(name); got != want {
			t.Errorf("nerdFontBuiltIn(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestIconsAutoFollowsTheTerminal(t *testing.T) {
	t.Cleanup(func() { render.SetIcons(false) })
	a := newTestApp()
	a.configure(config.Config{}, nil)
	if a.icons != render.IconsAuto || render.IconsOn() {
		t.Fatalf("unknown terminal: icons %q, on %v; want auto and off", a.icons, render.IconsOn())
	}
	a.Update(tea.TerminalVersionMsg{Name: "ghostty 1.1.3"})
	if !render.IconsOn() {
		t.Fatal("Ghostty's XTVERSION reply did not turn icons on")
	}
	a.Update(tea.TerminalVersionMsg{Name: "tmux 3.4"})
	if render.IconsOn() {
		t.Fatal("inside tmux icons stayed on")
	}
	a.configure(config.Config{Icons: "nerd"}, nil)
	if !render.IconsOn() {
		t.Fatal("icons: nerd did not turn icons on")
	}
}

func TestUnknownIconsFallsBackAndReports(t *testing.T) {
	a := newTestApp()
	a.configure(config.Config{Icons: "emoji"}, nil)
	if a.icons != render.IconsAuto || !a.flashErr || !strings.Contains(a.flash, "emoji") {
		t.Fatalf("icons %q, flash %q", a.icons, a.flash)
	}
}

func TestIconsSettingCyclesAndSaves(t *testing.T) {
	t.Cleanup(func() { render.SetIcons(false) })
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("DPAD_CONFIG", path)
	a := newTestApp()
	a.configure(config.Config{}, nil)

	press(a, "o")
	if !strings.Contains(ansi.Strip(a.render()), "auto → off") {
		t.Errorf("settings do not show what auto chose:\n%s", ansi.Strip(a.render()))
	}
	a.settings.cur.key = "Icons"
	press(a, "enter")
	if a.icons != render.IconsNerd || !render.IconsOn() {
		t.Fatalf("enter cycled to %q (on %v), want nerd", a.icons, render.IconsOn())
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "icons: nerd") {
		t.Fatalf("config not saved: %q %v", data, err)
	}
	press(a, "enter")
	if a.icons != render.IconsOff || render.IconsOn() {
		t.Fatalf("enter cycled to %q (on %v), want off", a.icons, render.IconsOn())
	}
}

func TestIconsBeforeNamesKeepEightyColumns(t *testing.T) {
	t.Cleanup(func() { render.SetIcons(false) })
	v := NewContainersView(context.Background(), &fakeClient{})
	v.SetSize(80, 10)
	v.Update(containersMsg{items: []container.Summary{
		{ID: "a", Names: []string{"/immich-postgres"}, Image: "postgres:16", State: container.StateRunning, Status: "Up 3 weeks"},
		{ID: "b", Names: []string{"/dp-demo-health-starting-1"}, Image: "acme/tool", State: container.StateRunning, Status: "Up 3 weeks"},
	}})
	render.SetIcons(true)
	view := v.View()
	plain := ansi.Strip(view)
	if !strings.Contains(plain, " immich-postgres") {
		t.Errorf("no postgres icon before the name:\n%s", plain)
	}
	for line := range strings.SplitSeq(view, "\n") {
		if w := ansi.StringWidth(line); w > 80 {
			t.Errorf("line is %d wide: %q", w, ansi.Strip(line))
		}
	}
	v.SetFilter("immich")
	if plain := ansi.Strip(v.View()); !strings.Contains(plain, "immich-postgres") || strings.Contains(plain, "dp-demo") {
		t.Errorf("filter by name broke with icons:\n%s", plain)
	}
}
