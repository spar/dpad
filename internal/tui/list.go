package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"dockernext/internal/render"
	"dockernext/internal/sortx"
)

// row is one table row of a list tab.
type row struct {
	key   string // stable identity, so the cursor survives re-sorting
	cells []string
	// id and name identify the container the row acts on; empty if none.
	id, name string
}

// list is the sortable, filterable table shared by the list tabs. Tabs embed
// it, supply items, and describe how to match, sort and render them.
type list[T any] struct {
	columns []render.Column
	keys    []sortx.Key // sort keys "s" cycles through
	key     sortx.Key
	reverse bool
	match   func(item T, filter string) bool
	sort    func(items []T, key sortx.Key, reverse bool)
	rows    func(item T) []row
	loading string // shown until loaded
	empty   string // shown when no row matches

	items       []T
	loaded      bool
	err         error
	noHighlight bool // hide the cursor (standalone `dn stats`)
	cur         cursor
	filter      string
	width       int
	height      int
	page        int // rows moved by pgup/pgdn
}

// visible returns the rows that match the filter, in sort order.
func (l *list[T]) visible(filter string) []row {
	items := make([]T, 0, len(l.items))
	for _, it := range l.items {
		if filter == "" || l.match(it, filter) {
			items = append(items, it)
		}
	}
	l.sort(items, l.key, l.reverse)
	var out []row
	for _, it := range items {
		out = append(out, l.rows(it)...)
	}
	return out
}

func rowKeys(rows []row) []string {
	keys := make([]string, len(rows))
	for i, r := range rows {
		keys[i] = r.key
	}
	return keys
}

func (l *list[T]) HandleKey(key string) tea.Cmd {
	switch key {
	case "s":
		l.key = sortx.Next(l.key, l.keys)
	case "r":
		l.reverse = !l.reverse
	default:
		l.cur.navKey(rowKeys(l.visible(l.filter)), key, l.page)
	}
	return nil
}

func (l *list[T]) SetSize(width, height int) {
	l.width, l.height = width, height
	l.page = max(height-2, 1)
}

func (l *list[T]) SetFilter(filter string) { l.filter = filter }

func (l *list[T]) Filter() string { return l.filter }

func (l *list[T]) View() string {
	switch {
	case l.err != nil:
		return render.Error.Render("error: " + l.err.Error())
	case !l.loaded:
		return render.Dim.Render(l.loading)
	}
	rows := l.visible(l.filter)
	if len(rows) == 0 {
		return render.Dim.Render(l.empty)
	}
	cells := make([][]string, len(rows))
	for i, r := range rows {
		cells[i] = r.cells
	}
	sel := l.cur.index(rowKeys(rows))
	if l.noHighlight {
		sel = -1
	}
	t := render.Table{Columns: l.columns, Rows: cells, Width: l.width}
	return t.Window(sel, l.height)
}

// Selected returns the container of the highlighted row, resolved the same
// way View does so actions never target a row that isn't on screen.
func (l *list[T]) Selected() (string, string, bool) {
	rows := l.visible(l.filter)
	if len(rows) == 0 {
		return "", "", false
	}
	r := rows[l.cur.index(rowKeys(rows))]
	return r.id, r.name, r.id != ""
}

func (l *list[T]) Help() [][2]string {
	return [][2]string{
		{"s", "cycle sort: " + sortx.Join(l.keys, " / ")},
		{"r", "reverse sort"},
	}
}
