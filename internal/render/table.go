package render

import (
	"slices"
	"strings"
	"unicode/utf8"

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

// PreparedTable keeps measured cells across selection and viewport changes.
// Prepare again when row contents or columns change; Width can change freely.
type PreparedTable struct {
	Width        int
	columns      []Column
	widths       []int
	headers      []string
	headerWidths []int
	cells        [][]string
	cellWidths   [][]int
}

func (t Table) String() string                     { return t.Prepare().String() }
func (t Table) Window(selected, height int) string { return t.Prepare().Window(selected, height) }

func (t Table) Prepare() *PreparedTable {
	n := len(t.Columns)
	widths := make([]int, n)
	headers, headerW := make([]string, n), make([]int, n)
	for i, c := range t.Columns {
		headers[i], headerW[i] = Header.Render(c.Header), width(c.Header)
		widths[i] = headerW[i]
	}
	// Truncate and measure every cell once.
	cells, cellW := make([][]string, len(t.Rows)), make([][]int, len(t.Rows))
	for r, row := range t.Rows {
		cells[r], cellW[r] = make([]string, n), make([]int, n)
		for i := range min(n, len(row)) {
			s, w := row[i], width(row[i])
			if w == 0 {
				s = "" // a styled empty cell would stop trailing padding being trimmed
			}
			if m := t.Columns[i].Max; m > 0 && w > m {
				s = truncate(s, m)
				w = width(s)
			}
			cells[r][i], cellW[r][i] = s, w
			widths[i] = max(widths[i], w)
		}
	}

	return &PreparedTable{Width: t.Width, columns: slices.Clone(t.Columns), widths: widths, headers: headers, headerWidths: headerW, cells: cells, cellWidths: cellW}
}

func (t *PreparedTable) String() string { return strings.Join(t.lines(-1, 0, len(t.cells)), "\n") }
func (t *PreparedTable) Window(selected, height int) string {
	avail := max(height-1, 1)
	start := 0
	if selected >= avail {
		start = selected - avail + 1
	}
	return t.WindowFrom(selected, height, start)
}

// WindowFrom renders a viewport whose first data row is start.
func (t *PreparedTable) WindowFrom(selected, height, start int) string {
	avail := max(height-1, 1)
	start = max(0, min(start, len(t.cells)))
	end := min(start+avail, len(t.cells))
	return strings.Join(t.lines(selected, start, end), "\n")
}
func (t *PreparedTable) lines(selected, start, end int) []string {
	n := len(t.columns)
	// Lines only need truncating if the widest possible one doesn't fit.
	total := len(colGap) * (n - 1)
	for _, w := range t.widths {
		total += w
	}
	truncate := t.Width > 0 && total > t.Width

	line := func(cells []string, cellW []int) string {
		var b strings.Builder
		for i, w := range t.widths {
			pad := strings.Repeat(" ", w-cellW[i])
			if t.columns[i].Align == Right {
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
		if truncate {
			out = ansi.Truncate(out, t.Width, "…")
		}
		return out
	}

	out := make([]string, 0, end-start+1)
	out = append(out, line(t.headers, t.headerWidths))
	for r := start; r < end; r++ {
		if r != selected {
			out = append(out, line(t.cells[r], t.cellWidths[r]))
			continue
		}
		// Inner ANSI resets would break the highlight, so render plain text.
		plain := make([]string, n)
		for i, c := range t.cells[r] {
			plain[i] = ansi.Strip(c)
		}
		l := line(plain, t.cellWidths[r])
		if t.Width > 0 {
			l += strings.Repeat(" ", max(0, t.Width-width(l)))
		}
		out = append(out, Selected.Render(l))
	}
	return out
}

func truncate(s string, limit int) string {
	for i := range len(s) {
		if s[i] < ' ' || s[i] > '~' {
			return ansi.Truncate(s, limit, "…")
		}
	}
	if len(s) <= limit {
		return s
	}
	if limit < 1 {
		return ""
	}
	return s[:limit-1] + "…"
}

// width is ansi.StringWidth with a fast path for the usual cell: ASCII text
// with SGR color codes. The general case segments every grapheme, which
// dominates rendering time.
func width(s string) int {
	w := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= utf8.RuneSelf:
			r, size := utf8.DecodeRuneInString(s[i:])
			switch {
			case r >= 0x2580 && r <= 0x259f, r == '→', r == '▾', r == '▸', r == '—', r == '·', r == '▲', r == '▼', r == '↑', r == '↓', r == '♥', r == '✗', r == '…':
				w++
				i += size - 1
			default:
				return ansi.StringWidth(s)
			}
		case c == ansi.ESC:
			// Skip a CSI sequence (ESC [ params final); fall back for others.
			if i+1 >= len(s) || s[i+1] != '[' {
				return ansi.StringWidth(s)
			}
			for i += 2; i < len(s) && (s[i] < 0x40 || s[i] > 0x7e); i++ {
			}
		case c >= 0x20 && c != 0x7f:
			w++
		}
	}
	return w
}
