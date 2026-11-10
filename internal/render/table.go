package render

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type Align int

const (
	Left Align = iota
	Right
)

const colGap = "   "

// Table renders docker-style borderless columns. Cells may contain ANSI
// styling; widths are measured on visible characters.
type Table struct {
	Headers  []string
	Rows     [][]string
	Align    []Align // per column; default Left
	MaxWidth []int   // per column; 0 = unlimited
	Selected int     // row index to highlight, -1 for none
	Width    int     // truncate every line to this width; 0 = unlimited
}

// Lines returns the rendered header line and one line per row.
func (t Table) Lines() (string, []string) {
	widths := make([]int, len(t.Headers))
	cell := func(row []string, i int) string {
		if i >= len(row) {
			return ""
		}
		s := row[i]
		if i < len(t.MaxWidth) && t.MaxWidth[i] > 0 && ansi.StringWidth(s) > t.MaxWidth[i] {
			s = ansi.Truncate(s, t.MaxWidth[i], "…")
		}
		return s
	}
	for i, h := range t.Headers {
		widths[i] = ansi.StringWidth(h)
	}
	for _, r := range t.Rows {
		for i := range widths {
			widths[i] = max(widths[i], ansi.StringWidth(cell(r, i)))
		}
	}

	line := func(cells []string, style func(string) string) string {
		var b strings.Builder
		for i, w := range widths {
			s := style(cell(cells, i))
			pad := strings.Repeat(" ", w-ansi.StringWidth(s))
			if i < len(t.Align) && t.Align[i] == Right {
				b.WriteString(pad + s)
			} else if i == len(widths)-1 {
				b.WriteString(s)
			} else {
				b.WriteString(s + pad)
			}
			if i < len(widths)-1 {
				b.WriteString(colGap)
			}
		}
		out := strings.TrimRight(b.String(), " ")
		if t.Width > 0 {
			out = ansi.Truncate(out, t.Width, "…")
		}
		return out
	}

	header := line(t.Headers, func(s string) string { return Header.Render(s) })
	rows := make([]string, len(t.Rows))
	for i, r := range t.Rows {
		if i == t.Selected {
			// Inner ANSI resets would break the highlight, so render plain text.
			plain := line(r, ansi.Strip)
			if t.Width > 0 {
				plain += strings.Repeat(" ", max(0, t.Width-ansi.StringWidth(plain)))
			}
			rows[i] = Selected.Render(plain)
			continue
		}
		rows[i] = line(r, func(s string) string { return s })
	}
	return header, rows
}

// String renders the full table.
func (t Table) String() string {
	h, rows := t.Lines()
	return strings.Join(append([]string{h}, rows...), "\n")
}
