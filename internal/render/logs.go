package render

import (
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/spar/dpad/internal/docker"
)

// StableNameStyle keeps dynamic source colors stable without retaining names.
func StableNameStyle(name string) lipgloss.Style {
	hash := uint32(2166136261)
	for i := range len(name) {
		hash = (hash ^ uint32(name[i])) * 16777619
	}
	return paletteStyle(int(hash % uint32(len(active.Names))))
}

const logTimeLayout = "2006-01-02T15:04:05.000"

type Level int

const (
	LevelNone Level = iota
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
)

var levelPatterns = []*regexp.Regexp{
	regexp.MustCompile(`"(?:level|severity|lvl)"\s*:\s*"(\w+)"`),              // JSON
	regexp.MustCompile(`\b(?:level|severity|lvl)=["']?(\w+)`),                 // logfmt
	regexp.MustCompile(`\[(emerg|alert|crit|error|warn|notice|info|debug)\]`), // nginx/apache
	regexp.MustCompile(`\b(FATAL|PANIC|CRITICAL|CRIT|ERROR|ERR|WARNING|WARN|INFO|NOTICE|DEBUG|TRACE)\b`),
}

// DetectLevel finds the log level of a line and the byte span of the token
// that indicated it (start == end == -1 if none).
func DetectLevel(line string) (Level, int, int) {
	for _, re := range levelPatterns {
		if m := re.FindStringSubmatchIndex(line); m != nil {
			if lvl := parseLevel(line[m[2]:m[3]]); lvl != LevelNone {
				return lvl, m[2], m[3]
			}
		}
	}
	return LevelNone, -1, -1
}

func parseLevel(s string) Level {
	switch strings.ToLower(s) {
	case "fatal", "panic", "critical", "crit", "error", "err", "emerg", "alert":
		return LevelError
	case "warning", "warn":
		return LevelWarn
	case "info", "notice", "information":
		return LevelInfo
	case "debug", "trace":
		return LevelDebug
	}
	return LevelNone
}

func levelStyles(l Level) (token, text lipgloss.Style) {
	switch l {
	case LevelError:
		return fg(active.Logs.Error.C()).Bold(true), fg(active.Logs.Error.C())
	case LevelWarn:
		return fg(active.Logs.Warn.C()).Bold(true), fg(active.Logs.Warn.C())
	case LevelInfo:
		return fg(active.Logs.Info.C()).Bold(true), lipgloss.NewStyle()
	case LevelDebug:
		return fg(active.Logs.Debug.C()), Dim
	}
	return lipgloss.NewStyle(), lipgloss.NewStyle()
}

// LogLine renders a log line with level highlighting and grep matches.
// prefixWidth > 0 adds an aligned container-name prefix in nameStyle.
func LogLine(l docker.LogLine, nameStyle lipgloss.Style, prefixWidth int, timestamps bool, grep *regexp.Regexp) string {
	var b strings.Builder
	if prefixWidth > 0 {
		b.WriteString(nameStyle.Render(l.Container + strings.Repeat(" ", max(0, prefixWidth-len(l.Container)))))
		sep := Dim.Render(" │ ")
		if l.Stderr {
			sep = fg(active.Logs.Error.C()).Render(" ┃ ")
		}
		b.WriteString(sep)
	} else if l.Stderr {
		b.WriteString(fg(active.Logs.Error.C()).Render("┃ "))
	}
	if timestamps && !l.Time.IsZero() {
		b.WriteString(Dim.Render(l.Time.Local().Format(logTimeLayout)) + " ")
	}
	b.WriteString(highlight(l.Text, grep))
	return b.String()
}

// LogLineWidth is the display width LogLine gives l, where textWidth is the
// width of l.Text. It lets callers lay lines out without styling them.
func LogLineWidth(l docker.LogLine, prefixWidth int, timestamps bool, textWidth int) int {
	w := textWidth
	if prefixWidth > 0 {
		w += max(prefixWidth, ansi.StringWidth(l.Container)) + 3
	} else if l.Stderr {
		w += 2
	}
	if timestamps && !l.Time.IsZero() {
		w += len(logTimeLayout) + 1
	}
	return w
}

// highlight styles text by priority: grep match > level token > line tint.
func highlight(text string, grep *regexp.Regexp) string {
	const (
		base = iota
		token
		match
	)
	lvl, ts, te := DetectLevel(text)
	tokenStyle, textStyle := levelStyles(lvl)
	marks := make([]byte, len(text))
	for i := ts; i >= 0 && i < te; i++ {
		marks[i] = token
	}
	if grep != nil {
		for _, m := range grep.FindAllStringIndex(text, -1) {
			for i := m[0]; i < m[1]; i++ {
				marks[i] = match
			}
		}
	}
	styles := [...]lipgloss.Style{textStyle, tokenStyle, Match}

	var b strings.Builder
	for start := 0; start < len(text); {
		end := start
		for end < len(text) && marks[end] == marks[start] {
			end++
		}
		b.WriteString(styles[marks[start]].Render(text[start:end]))
		start = end
	}
	return b.String()
}
