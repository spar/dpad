// Package render holds colors, formatting helpers and the ANSI-aware table
// renderer shared by the CLI commands and the TUI.
package render

import (
	"image/color"
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"dpad/internal/theme"
)

// Styles derived from the active theme; ApplyTheme sets them.
var (
	Header   lipgloss.Style
	Dim      lipgloss.Style
	Bold     = lipgloss.NewStyle().Bold(true)
	Selected = lipgloss.NewStyle().Reverse(true)
	Match    lipgloss.Style
	Error    lipgloss.Style
	Warn     lipgloss.Style
	Accent   lipgloss.Style
	Success  lipgloss.Style
	Brand    lipgloss.Style
)

var (
	active          *theme.Theme
	generation      int
	defaultHeader   lipgloss.Style
	defaultSelected = Selected
	// barOn and barOff wrap a usage bar inside the selected row so the bar
	// keeps its own colors; barBg is laid under the bar's styled runs.
	barOn, barOff, barBg = "\x1b[27m", "\x1b[7m", ""
)

func init() { ApplyTheme(theme.Default()) }

// ApplyTheme makes t the theme every style is drawn from.
func ApplyTheme(t *theme.Theme) {
	active = t
	generation++
	ui := t.UI
	defaultHeader = fg(ui.Header.C()).Bold(true)
	Header = defaultHeader
	Dim = fg(ui.Dim.C())
	Match = fg(ui.MatchText.C()).Background(ui.MatchBackground.C())
	Error = fg(ui.Error.C()).Bold(true)
	Warn = fg(ui.Warning.C())
	Accent = fg(ui.Accent.C()).Bold(true)
	Success = fg(ui.Success.C())
	Brand = fg(ui.Brand.C()).Bold(true)
}

// Theme returns the active theme.
func Theme() *theme.Theme { return active }

// ThemeGeneration changes whenever the theme does, so views can tell when
// text they cached was styled with an older theme.
func ThemeGeneration() int { return generation }

// SetAccent tints table headers and the selected row with c, or restores the
// defaults when c is nil. The selected row keeps reverse video when color is
// off, since a background alone would vanish.
func SetAccent(c color.Color) {
	Header, Selected = defaultHeader, defaultSelected
	barOn, barOff, barBg = "\x1b[27m", "\x1b[7m", ""
	if c == nil {
		return
	}
	Header = lipgloss.NewStyle().Bold(true).Foreground(c)
	if colorDisabled {
		return
	}
	Selected = OnAccent(c)
	barBg = sgrPrefix(lipgloss.NewStyle().Background(c))
	barOn, barOff = "\x1b[m"+barBg, sgrPrefix(Selected)
}

// DimLine dims s and keeps it dim after any styled text inside it, whose
// reset would otherwise switch the dimming off for the rest of the line.
func DimLine(s string) string {
	if pre := sgrPrefix(Dim); pre != "" {
		s = strings.ReplaceAll(s, "\x1b[m", "\x1b[m"+pre)
	}
	return Dim.Render(s)
}

// OnAccent is text on a filled accent background, as in the active tab.
func OnAccent(c color.Color) lipgloss.Style { return fg(active.UI.OnAccent.C()).Background(c) }

// sgrPrefix returns the escape sequence s opens a rendered string with.
func sgrPrefix(s lipgloss.Style) string {
	const mark = "\uE000"
	pre, _, _ := strings.Cut(s.Render(mark), mark)
	return pre
}

// Out is stdout for CLI output. Styles always render full ANSI; Out adapts
// it to the terminal, downsampling colors and stripping all styling when
// stdout isn't a terminal, so piped output stays plain.
var Out = colorprofile.NewWriter(os.Stdout, os.Environ())

// colorDisabled records that the user turned color off; see ColorDisabled.
var colorDisabled bool

// SetupColor turns styling off for CLI output when requested or when
// NO_COLOR is set.
func SetupColor(disable bool) {
	if disable || os.Getenv("NO_COLOR") != "" {
		Out.Profile = colorprofile.NoTTY
		colorDisabled = true
	}
}

// ColorDisabled reports whether SetupColor turned color off.
func ColorDisabled() bool { return colorDisabled }

// NewWriter returns a writer to w that adapts styling like Out. Write whole
// lines to it: escape sequences split across writes aren't recognized.
func NewWriter(w io.Writer) io.Writer {
	return &colorprofile.Writer{Forward: w, Profile: Out.Profile}
}

// fg is a style with foreground c; nil keeps the terminal's color.
func fg(c color.Color) lipgloss.Style {
	if c == nil {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(c)
}

// StateStyle colors a container state; exitCode only matters for "exited".
func StateStyle(state string, exitCode int) lipgloss.Style {
	st := active.States
	switch state {
	case "running":
		return fg(st.Running.C())
	case "exited":
		if exitCode != 0 {
			return fg(st.Failed.C())
		}
		return fg(st.Exited.C())
	case "paused":
		return fg(st.Paused.C())
	case "restarting":
		return fg(st.Restarting.C())
	case "created":
		return fg(st.Created.C())
	case "dead":
		return fg(st.Dead.C()).Bold(true)
	default:
		return fg(st.Exited.C())
	}
}

// HealthBadge renders a short health marker, or "" when there is no healthcheck.
func HealthBadge(status string) string {
	h := active.Health
	switch status {
	case "healthy":
		return fg(h.Healthy.C()).Render("♥ healthy")
	case "unhealthy":
		return fg(h.Unhealthy.C()).Bold(true).Render("✗ unhealthy")
	case "starting":
		return fg(h.Starting.C()).Render("… starting")
	}
	return ""
}

// PercentStyle colors a usage percentage: low <50, medium <80, high otherwise.
func PercentStyle(p float64) lipgloss.Style {
	u := active.Usage
	switch {
	case p < 50:
		return fg(u.Low.C())
	case p < 80:
		return fg(u.Medium.C())
	default:
		return fg(u.High.C()).Bold(true)
	}
}

// Sparkline is the style of the Stats history sparklines.
func Sparkline() lipgloss.Style { return fg(active.Usage.Sparkline.C()) }

func portsStyle() lipgloss.Style { return fg(active.Columns.Ports.C()) }
func tagStyle() lipgloss.Style   { return fg(active.Columns.Tag.C()) }

// Bar renders a fixed-width usage bar for a 0–100 percentage.
func Bar(p float64, width int) string {
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	n := int(p/100*float64(width) + 0.5)
	return PercentStyle(p).Render(strings.Repeat("█", n)) + fg(active.Usage.Track.C()).Render(strings.Repeat("░", width-n))
}

func paletteStyle(i int) lipgloss.Style {
	names := active.Names
	return fg(names[i%len(names)].C())
}

// NameColors hands out palette colors in order of first use, so a handful of
// containers shown together never share a color (unlike hashing).
type NameColors map[string]int

func (n NameColors) Style(name string) lipgloss.Style {
	i, ok := n[name]
	if !ok {
		i = len(n)
		n[name] = i
	}
	return paletteStyle(i)
}

// Swatch previews t in a few cells: its tab accents, then success, warning
// and error.
func Swatch(t *theme.Theme) string {
	colors := append(t.TabAccents(), t.UI.Success.C(), t.UI.Warning.C(), t.UI.Error.C())
	var b strings.Builder
	for i, c := range colors {
		if i == len(colors)-3 {
			b.WriteByte(' ')
		}
		b.WriteString(fg(c).Render("██"))
	}
	return b.String()
}
