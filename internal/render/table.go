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

// Column describes one table column.
type Column struct {
	Header string
	Align  Align
	Max    int // truncate wider cells to this width; 0 = unlimited
}

// Table renders docker-style borderless columns. Cells may contain ANSI
// styling; widths are measured on visible characters.
type Table struct {
	Columns []Column
	Rows    [][]string
	Width   int // truncate every line to this width; 0 = unlimited
}

// String renders the full table with no row highlighted.
func (t Table) String() string {
	return strings.Join(t.lines(-1), "\n")
}

// Window renders the header and as many rows as fit in height lines,
// scrolled so the selected row is visible and highlighted. A negative
// selected highlights nothing.
func (t Table) Window(selected, height int) string {
	lines := t.lines(selected)
	header, rows := lines[0], lines[1:]
	avail := max(height-1, 1)
	start := 0
	if selected >= avail {
		start = selected - avail + 1
	}
	end := min(start+avail, len(rows))
	return strings.Join(append([]string{header}, rows[start:end]...), "\n")
}

// lines returns the rendered header line followed by one line per row.
func (t Table) lines(selected int) []string {
	widths := make([]int, len(t.Columns))
	cell := func(row []string, i int) string {
		if i >= len(row) {
			return ""
		}
		s := row[i]
		if m := t.Columns[i].Max; m > 0 && ansi.StringWidth(s) > m {
			s = ansi.Truncate(s, m, "…")
		}
		return s
	}
	headers := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		headers[i] = c.Header
		widths[i] = ansi.StringWidth(c.Header)
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
			if t.Columns[i].Align == Right {
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

	out := make([]string, 0, len(t.Rows)+1)
	out = append(out, line(headers, func(s string) string { return Header.Render(s) }))
	for i, r := range t.Rows {
		if i == selected {
			// Inner ANSI resets would break the highlight, so render plain text.
			plain := line(r, ansi.Strip)
			if t.Width > 0 {
				plain += strings.Repeat(" ", max(0, t.Width-ansi.StringWidth(plain)))
			}
			out = append(out, Selected.Render(plain))
			continue
		}
		out = append(out, line(r, func(s string) string { return s }))
	}
	return out
}
