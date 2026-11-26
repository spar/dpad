package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"

	"dpad/internal/config"
	"dpad/internal/render"
)

func TestFooterKeepsHelpAndQuitAtAnyWidth(t *testing.T) {
	a := newTestApp()
	items := []container.Summary{composeContainer("web", "shop", "web", "running")}
	for _, tab := range a.tabs {
		tab.Update(containersMsg{items: items})
	}
	for _, width := range []int{200, 120, 80, 40} {
		a.resize(width, 30)
		for i := range a.tabs {
			a.active = i
			footer := ansi.Strip(a.footer())
			if lipgloss.Width(footer) > width || !strings.HasSuffix(footer, "? help · q quit") {
				t.Errorf("%s at %d columns: %q", a.tabs[i].Title(), width, footer)
			}
		}
	}
	a.active = 0
	a.resize(200, 30)
	if !strings.Contains(ansi.Strip(a.footer()), "tab switch · c context · / filter · s/r sort: status · d details") {
		t.Errorf("a wide footer lost hints: %q", ansi.Strip(a.footer()))
	}
	a.resize(110, 30)
	if footer := ansi.Strip(a.footer()); strings.Contains(footer, "tab switch") || !strings.Contains(footer, "P/K/D pause/kill/remove") {
		t.Errorf("general hints should go before row actions: %q", footer)
	}
}

func TestSortHintMatchesTheKeysThatWork(t *testing.T) {
	a := newTestApp()
	a.resize(200, 30)
	for i, want := range map[int]string{0: "s/r sort: status", 5: " r sort: name", 6: " r sort: name"} {
		a.active = i
		if footer := ansi.Strip(a.footer()); !strings.Contains(footer, want) {
			t.Errorf("%s footer = %q, want %q", a.tabs[i].Title(), footer, want)
		}
	}
	for _, h := range a.tabs[5].Help() {
		if h[0] == "s" {
			t.Error("Volumes help offers a sort cycle with one sort key")
		}
	}
}

func TestHelpScrollsWhenItDoesNotFit(t *testing.T) {
	a := newTestApp()
	a.active = 3
	a.resize(80, 24)
	press(a, "?")
	if a.helpOverflow() == 0 {
		t.Fatal("expected Projects help to overflow 24 rows")
	}
	view := ansi.Strip(a.render())
	if n := strings.Count(view, "\n") + 1; n != 24 || !strings.Contains(view, "more below") || !strings.Contains(view, "KEY") {
		t.Fatalf("help view has %d lines: %q", n, view)
	}
	for range 50 {
		press(a, "j")
	}
	view = ansi.Strip(a.render())
	if !a.help || !strings.Contains(view, "KEY") || !strings.Contains(view, "u / U") || strings.Contains(view, "more below") {
		t.Fatalf("scrolled help lost the header or its last row: %q", view)
	}
	press(a, "x")
	if a.help || a.helpTop != 0 || a.confirm != nil {
		t.Fatal("another key should only close help")
	}
	a.resize(120, 50)
	press(a, "?")
	if a.helpOverflow() != 0 || !strings.Contains(ansi.Strip(a.render()), "press any key to close") {
		t.Fatal("help that fits should not scroll")
	}
	press(a, "j")
	if a.help {
		t.Fatal("any key closes help that fits")
	}
}

func TestResultMessagesExpireButErrorsStay(t *testing.T) {
	a := newTestApp()
	a.commands = []config.CustomCommand{}
	_, cmd := a.Update(actionDoneMsg{text: "restart web: done"})
	if cmd == nil || a.flash == "" {
		t.Fatal("result was not shown")
	}
	a.Update(flashExpiredMsg{generation: a.flashGen - 1})
	if a.flash == "" {
		t.Fatal("a stale timer cleared a newer message")
	}
	a.Update(flashExpiredMsg{generation: a.flashGen})
	if a.flash != "" {
		t.Fatal("result did not expire")
	}
	a.Update(actionDoneMsg{err: fmt.Errorf("stop web: no such container")})
	a.Update(flashExpiredMsg{generation: a.flashGen})
	if a.flash == "" || !a.flashErr {
		t.Fatal("an error should stay until the next key")
	}
}

func TestStatusStaysDimAfterStyledText(t *testing.T) {
	plain := render.Dim.Render("x")
	pre, _, _ := strings.Cut(plain, "x")
	if pre == "" {
		t.Skip("dim has no escape sequence in this theme")
	}
	got := render.DimLine("logs: " + render.Accent.Render("web") + " · following")
	if !strings.Contains(got, "\x1b[m"+pre+" · following") {
		t.Fatalf("text after the name is not dim: %q", got)
	}
}
