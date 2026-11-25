package tui

import (
	"fmt"
	"image/color"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"dpad/internal/render"
)

// Prototype: DPAD_LOOK picks a visual variant for mockups (1, 2 or 3).
var look = os.Getenv("DPAD_LOOK")

var tabAccents = []color.Color{
	render.Cyan, lipgloss.Color("12"), render.Yellow, render.Magenta,
	render.Green, lipgloss.Color("13"), lipgloss.Color("14"),
}

var (
	baseHeader   = render.Header
	baseSelected = render.Selected
	black        = lipgloss.Color("0")
)

func tabAccent(i int) color.Color { return tabAccents[i%len(tabAccents)] }

func (a *App) bodyWidth() int {
	if look == "3" {
		return max(a.width-2, 1)
	}
	return a.width
}

func extraRows() int {
	switch look {
	case "2":
		return 1
	case "3":
		return 1 // frame adds 2 rows, status moves into the bottom edge
	}
	return 0
}

// applyLook sets the shared header and selection styles to the active tab's accent.
func (a *App) applyLook() {
	render.Header, render.Selected = baseHeader, baseSelected
	if look != "2" && look != "3" {
		return
	}
	c := tabAccent(a.active)
	render.Header = lipgloss.NewStyle().Bold(true).Foreground(c)
	render.Selected = lipgloss.NewStyle().Background(c).Foreground(black)
}

func lookBrand() string {
	switch look {
	case "2":
		return lipgloss.NewStyle().Bold(true).Foreground(render.Green).Render("▍") + render.Bold.Render("dpad") + " "
	case "3":
		return lipgloss.NewStyle().Bold(true).Background(render.Green).Foreground(black).Render(" dp ") + " "
	}
	return lipgloss.NewStyle().Bold(true).Foreground(render.Green).Render("dpad") + " "
}

func lookTab(i int, label string, active bool) string {
	c := tabAccent(i)
	if active {
		return lipgloss.NewStyle().Bold(true).Background(c).Foreground(black).Padding(0, 1).Render(label)
	}
	num, name, _ := strings.Cut(label, " ")
	s := lipgloss.NewStyle().Bold(true).Foreground(c).Render(num)
	if name != "" {
		s += " " + render.Dim.Render(name)
	}
	return " " + s + " "
}

// tabUnderline draws a heavy accent line under the active tab and a faint one elsewhere.
func (a *App) tabUnderline() string {
	parts, owners := a.tabBarParts()
	var b strings.Builder
	for k, p := range parts {
		w := lipgloss.Width(p)
		if k > 0 {
			b.WriteString(render.Dim.Render("─"))
		}
		if owners[k] == a.active {
			b.WriteString(lipgloss.NewStyle().Foreground(tabAccent(a.active)).Render(strings.Repeat("━", w)))
		} else {
			b.WriteString(render.Dim.Render(strings.Repeat("─", w)))
		}
	}
	if rest := a.width - lipgloss.Width(b.String()); rest > 0 {
		b.WriteString(render.Dim.Render(strings.Repeat("─", rest)))
	}
	return b.String()
}

func (a *App) frame(title, body, status string) string {
	c := lipgloss.NewStyle().Foreground(tabAccent(a.active))
	inner := max(a.width-2, 1)
	t := " " + lipgloss.NewStyle().Bold(true).Foreground(tabAccent(a.active)).Render(title) + " "
	top := c.Render("╭─") + t + c.Render(strings.Repeat("─", max(inner-1-lipgloss.Width(t), 0))+"╮")
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		l = ansi.Truncate(l, inner, "…")
		lines[i] = c.Render("│") + l + strings.Repeat(" ", max(inner-lipgloss.Width(l), 0)) + c.Render("│")
	}
	st := " " + ansi.Truncate(status, max(inner-4, 1), "…") + " "
	bottom := c.Render("╰─") + st + c.Render(strings.Repeat("─", max(inner-1-lipgloss.Width(st), 0))+"╯")
	return top + "\n" + strings.Join(lines, "\n") + "\n" + bottom
}

func connectedLabel() string {
	if look == "2" || look == "3" {
		return lipgloss.NewStyle().Foreground(render.Green).Render("● connected")
	}
	return render.Accent.Render("connected")
}

var _ = fmt.Sprint
