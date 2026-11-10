// Package render holds colors, formatting helpers and the ANSI-aware table
// renderer shared by the CLI commands and the TUI.
package render

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// ANSI palette so colors follow the user's terminal theme.
var (
	Green   = lipgloss.Color("2")
	Red     = lipgloss.Color("1")
	Yellow  = lipgloss.Color("3")
	Blue    = lipgloss.Color("4")
	Magenta = lipgloss.Color("5")
	Cyan    = lipgloss.Color("6")
	Gray    = lipgloss.Color("8")

	Header   = lipgloss.NewStyle().Bold(true).Foreground(Blue)
	Dim      = lipgloss.NewStyle().Foreground(Gray)
	Bold     = lipgloss.NewStyle().Bold(true)
	Selected = lipgloss.NewStyle().Reverse(true)
	Match    = lipgloss.NewStyle().Background(Yellow).Foreground(lipgloss.Color("0"))
	Error    = lipgloss.NewStyle().Bold(true).Foreground(Red)
	Warn     = lipgloss.NewStyle().Foreground(Yellow)
	Accent   = lipgloss.NewStyle().Bold(true).Foreground(Cyan)
)

// SetupColor disables color when requested, when NO_COLOR is set, or when
// stdout is not a terminal (so piped output stays plain).
func SetupColor(disable bool) {
	if disable || os.Getenv("NO_COLOR") != "" || !term.IsTerminal(int(os.Stdout.Fd())) {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
}

func fg(c lipgloss.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

// StateStyle colors a container state; exitCode only matters for "exited".
func StateStyle(state string, exitCode int) lipgloss.Style {
	switch state {
	case "running":
		return fg(Green)
	case "exited":
		if exitCode != 0 {
			return fg(Red)
		}
		return fg(Gray)
	case "paused":
		return fg(Yellow)
	case "restarting":
		return fg(Magenta)
	case "created":
		return fg(Blue)
	case "dead":
		return Error
	default:
		return fg(Gray)
	}
}

// HealthBadge renders a short health marker, or "" when there is no healthcheck.
func HealthBadge(status string) string {
	switch status {
	case "healthy":
		return fg(Green).Render("♥ healthy")
	case "unhealthy":
		return fg(Red).Bold(true).Render("✗ unhealthy")
	case "starting":
		return fg(Yellow).Render("… starting")
	}
	return ""
}

// PercentStyle colors a usage percentage: green <50, yellow <80, red otherwise.
func PercentStyle(p float64) lipgloss.Style {
	switch {
	case p < 50:
		return fg(Green)
	case p < 80:
		return fg(Yellow)
	default:
		return fg(Red).Bold(true)
	}
}

// Bar renders a fixed-width usage bar for a 0–100 percentage.
func Bar(p float64, width int) string {
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	n := int(p/100*float64(width) + 0.5)
	return PercentStyle(p).Render(strings.Repeat("█", n)) + Dim.Render(strings.Repeat("░", width-n))
}

var namePalette = []lipgloss.Color{
	Cyan, Magenta, Blue, Yellow, Green,
	lipgloss.Color("14"), lipgloss.Color("13"), lipgloss.Color("12"), lipgloss.Color("11"), lipgloss.Color("10"),
}

// NameColors hands out palette colors in order of first use, so a handful of
// containers shown together never share a color (unlike hashing).
type NameColors map[string]lipgloss.Style

func (n NameColors) Style(name string) lipgloss.Style {
	if s, ok := n[name]; ok {
		return s
	}
	s := fg(namePalette[len(n)%len(namePalette)])
	n[name] = s
	return s
}
