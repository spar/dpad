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
	return strings.Join(t.lines(-1, 0, len(t.Rows)), "\n")
}

// Window renders the header and as many rows as fit in height lines,
// scrolled so the selected row is visible and highlighted. A negative
// selected highlights nothing. Only the rows shown are formatted.
func (t Table) Window(selected, height int) string {
	avail := max(height-1, 1)
	start := 0
	if selected >= avail {
		start = selected - avail + 1
	}
	end := min(start+avail, len(t.Rows))
	return strings.Join(t.lines(selected, start, end), "\n")
}

// lines returns the header line followed by rows[start:end]. Column widths
// account for every row, so columns don't shift while scrolling.
func (t Table) lines(selected, start, end int) []string {
	n := len(t.Columns)
	widths := make([]int, n)
	headers, headerW := make([]string, n), make([]int, n)
	for i, c := range t.Columns {
		headers[i], headerW[i] = Header.Render(c.Header), ansi.StringWidth(c.Header)
		widths[i] = headerW[i]
	}
	// Truncate and measure every cell once.
	cells, cellW := make([][]string, len(t.Rows)), make([][]int, len(t.Rows))
	for r, row := range t.Rows {
		cells[r], cellW[r] = make([]string, n), make([]int, n)
		for i := range min(n, len(row)) {
			s, w := row[i], ansi.StringWidth(row[i])
			if m := t.Columns[i].Max; m > 0 && w > m {
				s = ansi.Truncate(s, m, "…")
				w = ansi.StringWidth(s)
			}
			cells[r][i], cellW[r][i] = s, w
			widths[i] = max(widths[i], w)
		}
	}

	line := func(cells []string, cellW []int) string {
		var b strings.Builder
		for i, w := range widths {
			pad := strings.Repeat(" ", w-cellW[i])
			if t.Columns[i].Align == Right {
				b.WriteString(pad + cells[i])
			} else if i == n-1 {
				b.WriteString(cells[i])
			} else {
				b.WriteString(cells[i] + pad)
			}
			if i < n-1 {
				b.WriteString(colGap)
			}
		}
		out := strings.TrimRight(b.String(), " ")
		if t.Width > 0 {
			out = ansi.Truncate(out, t.Width, "…")
		}
		return out
	}

	out := make([]string, 0, end-start+1)
	out = append(out, line(headers, headerW))
	for r := start; r < end; r++ {
		if r != selected {
			out = append(out, line(cells[r], cellW[r]))
			continue
		}
		// Inner ANSI resets would break the highlight, so render plain text.
		plain := make([]string, n)
		for i, c := range cells[r] {
			plain[i] = ansi.Strip(c)
		}
		l := line(plain, cellW[r])
		if t.Width > 0 {
			l += strings.Repeat(" ", max(0, t.Width-ansi.StringWidth(l)))
		}
		out = append(out, Selected.Render(l))
	}
	return out
}
