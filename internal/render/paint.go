package render

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// paint is a style's opening and closing escape sequences, taken from one
// Render. Cells drawn for every container every second, such as the CPU
// graphs, wrap their text in these instead of paying for a lipgloss Render
// each.
type paint struct{ open, close string }

func paintOf(s lipgloss.Style) paint {
	const mark = ""
	open, close, _ := strings.Cut(s.Render(mark), mark)
	return paint{open, close}
}

func (p paint) wrap(s string) string { return p.open + s + p.close }

func (p paint) write(b *strings.Builder, s string) {
	b.WriteString(p.open)
	b.WriteString(s)
	b.WriteString(p.close)
}

type usagePaints struct {
	generation                    int
	low, medium, high, dim, track paint
}

// usageCache holds the usage colors of the theme it was taken from.
var usageCache usagePaints

func usagePaint() *usagePaints {
	p := &usageCache
	if p.generation != generation {
		p.generation = generation
		p.low, p.medium, p.high = paintOf(PercentStyle(0)), paintOf(PercentStyle(50)), paintOf(PercentStyle(80))
		p.dim, p.track = paintOf(Dim), paintOf(fg(active.Usage.Track.C()))
	}
	return p
}

// percentPaint is PercentStyle(p) as a paint.
func percentPaint(p float64) paint {
	u := usagePaint()
	switch {
	case p < 50:
		return u.low
	case p < 80:
		return u.medium
	}
	return u.high
}
