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
	// When a table doesn't fit its Width, columns with a Min shrink toward it,
	// and columns with a Drop rank are hidden, lowest rank first.
	Min  int
	Drop int
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
		headers[i], headerW[i] = c.Header, width(c.Header)
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
	widths := t.layout()
	last := -1
	for i, w := range widths {
		if w >= 0 {
			last = i
		}
	}
	total := 0
	for i, w := range widths {
		if w >= 0 {
			total += w
			if i != last {
				total += len(colGap)
			}
		}
	}
	// Lines only need truncating if the widest possible one doesn't fit.
	truncate := t.Width > 0 && total > t.Width

	line := func(cells []string, cellW []int) string {
		var b strings.Builder
		for i, w := range widths {
			if w < 0 {
				continue
			}
			cell, cw := cells[i], cellW[i]
			if cw > w {
				cell, cw = ansi.Truncate(cell, w, "…"), w
			}
			pad := strings.Repeat(" ", w-cw)
			if t.columns[i].Align == Right {
				b.WriteString(pad + cell)
			} else if i == last {
				b.WriteString(cell)
			} else {
				b.WriteString(cell + pad)
			}
			if i != last {
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
	// Headers are styled here, not in Prepare, so they follow the current accent.
	headers := make([]string, len(t.headers))
	for i, h := range t.headers {
		headers[i] = Header.Render(h)
	}
	out = append(out, line(headers, t.headerWidths))
	for r := start; r < end; r++ {
		if r != selected {
			out = append(out, line(t.cells[r], t.cellWidths[r]))
			continue
		}
		// Inner ANSI resets would break the highlight, so render plain text.
		plain := make([]string, len(t.columns))
		bars := map[string]string{}
		for i, c := range t.cells[r] {
			plain[i] = ansi.Strip(c)
			if isBar(plain[i]) {
				bars[plain[i]] = c
			}
		}
		l := line(plain, t.cellWidths[r])
		if t.Width > 0 {
			l += strings.Repeat(" ", max(0, t.Width-width(l)))
		}
		l = Selected.Render(l)
		// Reversed, a bar's filled blocks read as empty, so bars keep their colors
		// over the row's background.
		for bar, styled := range bars {
			if barBg != "" {
				styled = strings.ReplaceAll(styled, "\x1b[m", "\x1b[m"+barBg)
			}
			l = strings.ReplaceAll(l, bar, barOn+styled+barOff)
		}
		out = append(out, l)
	}
	return out
}

// layout returns each column's width for t.Width, or -1 for a hidden column.
// Columns are hidden by Drop rank until the table fits at its Min widths, any
// hidden column that would still fit comes back, and then the columns with
// the most room above their Min shrink until the table fits.
func (t *PreparedTable) layout() []int {
	widths := slices.Clone(t.widths)
	if t.Width <= 0 {
		return widths
	}
	sum := func(atMin bool) int {
		total, shown := 0, 0
		for i, w := range widths {
			if w < 0 {
				continue
			}
			if m := t.minWidth(i); atMin && m > 0 {
				w = min(w, m)
			}
			total += w
			shown++
		}
		return total + max(shown-1, 0)*len(colGap)
	}
	if sum(false) <= t.Width {
		return widths
	}
	order := make([]int, 0, len(t.columns))
	for i, c := range t.columns {
		if c.Drop > 0 {
			order = append(order, i)
		}
	}
	slices.SortStableFunc(order, func(a, b int) int { return t.columns[a].Drop - t.columns[b].Drop })
	dropped := 0
	for ; dropped < len(order) && sum(true) > t.Width; dropped++ {
		widths[order[dropped]] = -1
	}
	for i := dropped - 1; i >= 0; i-- {
		c := order[i]
		widths[c] = t.widths[c]
		if sum(true) > t.Width {
			widths[c] = -1
		}
	}
	for over := sum(false) - t.Width; over > 0; over-- {
		widest, room := -1, 0
		for i, w := range widths {
			if m := t.minWidth(i); m > 0 && w-m > room {
				widest, room = i, w-m
			}
		}
		if widest < 0 {
			break
		}
		widths[widest]--
	}
	return widths
}

// minWidth is how narrow column i may shrink, or 0 if it keeps its width.
// Headers stay whole.
func (t *PreparedTable) minWidth(i int) int {
	if t.columns[i].Min == 0 {
		return 0
	}
	return max(t.columns[i].Min, t.headerWidths[i])
}

func isBar(s string) bool {
	return s != "" && strings.Trim(s, "█░") == ""
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
			case r >= 0x2580 && r <= 0x259f, r == '→', r == '▾', r == '▸', r == '—', r == '·', r == '▲', r == '▼', r == '↑', r == '↓', r == '♥', r == '✗', r == '…',
				r >= 0xe000 && r <= 0xf8ff, r >= 0xf0000 && r <= 0xffffd: // Nerd Font icons sit in the Private Use Areas
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
