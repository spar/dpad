package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// logScrollStep is how many columns ←→ pan.
const logScrollStep = 6

// logScroll is the scrolling window over the log buffer. It knows each line's
// width up front and asks for a line's text only when drawing it, so adding
// lines or changing the filter costs nothing per line that is off screen.
type logScroll struct {
	SoftWrap         bool
	width, height    int
	widths           []int
	line             func(i int) string
	longest          int
	yOffset, xOffset int
}

func (s *logScroll) SetWidth(w int)  { s.width = w }
func (s *logScroll) SetHeight(h int) { s.height = h }
func (s *logScroll) Height() int     { return s.height }
func (s *logScroll) YOffset() int    { return s.yOffset }

// SetLines replaces the content: widths holds each line's display width and
// line returns the text of line i.
func (s *logScroll) SetLines(widths []int, line func(i int) string) {
	s.widths, s.line, s.longest = widths, line, 0
	for _, w := range widths {
		s.longest = max(s.longest, w)
	}
	s.yOffset = min(s.yOffset, s.maxYOffset())
	s.xOffset = min(s.xOffset, s.maxXOffset())
}

// SetContentLines is SetLines for text that is already rendered.
func (s *logScroll) SetContentLines(lines []string) {
	widths := make([]int, len(lines))
	for i, l := range lines {
		widths[i] = ansi.StringWidth(l)
	}
	s.SetLines(widths, func(i int) string { return lines[i] })
}

// rows is how many screen rows line i takes.
func (s *logScroll) rows(i int) int {
	if !s.SoftWrap || s.width <= 0 || s.widths[i] <= s.width {
		return 1
	}
	return (s.widths[i] + s.width - 1) / s.width
}

// TotalLineCount counts screen rows, so wrapped lines count more than once.
func (s *logScroll) TotalLineCount() int {
	if !s.SoftWrap {
		return len(s.widths)
	}
	total := 0
	for i := range s.widths {
		total += s.rows(i)
	}
	return total
}

func (s *logScroll) maxYOffset() int { return max(s.TotalLineCount()-s.height, 0) }
func (s *logScroll) maxXOffset() int { return max(s.longest-s.width, 0) }
func (s *logScroll) AtBottom() bool  { return s.yOffset >= s.maxYOffset() }
func (s *logScroll) SetYOffset(n int) {
	s.yOffset = min(max(n, 0), s.maxYOffset())
}
func (s *logScroll) GotoTop()    { s.yOffset = 0 }
func (s *logScroll) GotoBottom() { s.yOffset = s.maxYOffset() }

// SetXOffset pans; wrapped lines have nothing to pan to.
func (s *logScroll) SetXOffset(n int) {
	if !s.SoftWrap {
		s.xOffset = min(max(n, 0), s.maxXOffset())
	}
}

func (s *logScroll) Update(msg tea.Msg) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch msg.String() {
		case "up", "k":
			s.SetYOffset(s.yOffset - 1)
		case "down", "j":
			s.SetYOffset(s.yOffset + 1)
		case "left", "h":
			s.SetXOffset(s.xOffset - logScrollStep)
		case "right", "l":
			s.SetXOffset(s.xOffset + logScrollStep)
		}
	}
}

func (s *logScroll) View() string {
	if s.width <= 0 || s.height <= 0 {
		return ""
	}
	out := make([]string, 0, s.height)
	if !s.SoftWrap {
		cut := s.xOffset > 0 || s.longest > s.width
		for i := s.yOffset; i < len(s.widths) && len(out) < s.height; i++ {
			l := s.line(i)
			if cut {
				l = ansi.Cut(l, s.xOffset, s.xOffset+s.width)
			}
			out = append(out, l)
		}
		return strings.Join(out, "\n")
	}
	// Find the line holding row yOffset, then how far into it that row is.
	i, skip := 0, s.yOffset
	for ; i < len(s.widths) && skip >= s.rows(i); i++ {
		skip -= s.rows(i)
	}
	for ; i < len(s.widths) && len(out) < s.height; i++ {
		l := s.line(i)
		if s.widths[i] <= s.width {
			out = append(out, l)
			continue
		}
		for row := skip; row < s.rows(i) && len(out) < s.height; row++ {
			out = append(out, ansi.Cut(l, row*s.width, (row+1)*s.width))
		}
		skip = 0
	}
	return strings.Join(out, "\n")
}
