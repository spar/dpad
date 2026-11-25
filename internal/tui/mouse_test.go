package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"
)

func click(a *App, x, y int) tea.Cmd {
	_, cmd := a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	return cmd
}

func wheel(a *App, button tea.MouseButton) {
	a.Update(tea.MouseWheelMsg{Button: button})
}

func threeContainers(a *App) {
	a.Update(containersMsg{items: []container.Summary{
		{ID: "a", Names: []string{"/a"}, State: "running"},
		{ID: "b", Names: []string{"/b"}, State: "running"},
		{ID: "c", Names: []string{"/c"}, State: "running"},
	}})
}

func TestClickingATabSwitchesToIt(t *testing.T) {
	a := newTestApp()
	bar := ansi.Strip(a.tabBar())
	click(a, strings.Index(bar, "3 Stats")+2, 0)
	if a.active != 2 {
		t.Fatalf("active = %d after clicking Stats", a.active)
	}
	click(a, strings.Index(bar, "5 Events")+2, 1)
	if a.active != 4 {
		t.Fatalf("active = %d after clicking under Events", a.active)
	}
	click(a, 1, 0)
	if a.active != 4 {
		t.Fatal("clicking the logo should not switch tabs")
	}
	a.resize(40, 30)
	bar = ansi.Strip(a.tabBar())
	click(a, strings.Index(bar, "7"), 0)
	if a.active != 6 {
		t.Fatalf("narrow bar: active = %d, bar %q", a.active, bar)
	}
}

func TestClickSelectsRowAndDoubleClickOpensLogs(t *testing.T) {
	f := &fakeClient{}
	a := newTestApp()
	a.client = f
	threeContainers(a)
	a.render()
	click(a, 5, bodyTop+3)
	if id, _, _ := a.tabs[0].Selected(); id != "c" {
		t.Fatalf("selected %s, want the third row", id)
	}
	click(a, 5, bodyTop)
	click(a, 5, bodyTop+9)
	if id, _, _ := a.tabs[0].Selected(); id != "c" {
		t.Fatal("clicks on the header or below the rows changed the selection")
	}
	click(a, 5, bodyTop+1)
	click(a, 5, bodyTop+1)
	if a.logs == nil || a.logs.name != "a" {
		t.Fatal("double-click should open the row's logs")
	}
	a.logs.Close()
}

func TestWheelMovesSelectionWithoutClearingStatusOrAnsweringPrompts(t *testing.T) {
	a := newTestApp()
	a.client = &fakeClient{}
	threeContainers(a)
	a.flash = "stop web: done"
	wheel(a, tea.MouseWheelDown)
	wheel(a, tea.MouseWheelDown)
	if id, _, _ := a.tabs[0].Selected(); id != "c" || a.flash != "stop web: done" {
		t.Fatalf("selected %s flash %q", id, a.flash)
	}
	wheel(a, tea.MouseWheelUp)
	a.Update(key("x"))
	wheel(a, tea.MouseWheelUp)
	if a.confirm == nil {
		t.Fatal("wheel answered the confirmation")
	}
	if id, _, _ := a.tabs[0].Selected(); id != "b" {
		t.Fatalf("selected %s", id)
	}
}

func TestMouseCaptureFollowsConfig(t *testing.T) {
	a := newApp(t.Context(), &fakeClient{})
	if a.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("mouse should be on by default")
	}
	off := false
	a.configure(configWithMouse(&off), nil)
	if a.View().MouseMode != tea.MouseModeNone {
		t.Fatal("mouse: false should leave the mouse to the terminal")
	}
}
