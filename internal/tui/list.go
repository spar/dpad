package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"dockernext/internal/docker"
	"dockernext/internal/render"
	"dockernext/internal/sortx"
)

// row is one table row of a list tab.
type row struct {
	key   string // stable identity, so the cursor survives re-sorting
	cells []string
	// id and name identify the container the row acts on; empty if none.
	id, name string
	image    *docker.ImageRemoval
}

// list is the sortable, filterable table shared by the list tabs. Tabs embed
// it, supply items, and describe how to match, sort and render them.
type list[T any] struct {
	columns []render.Column
	keys    []sortx.Key       // sort keys "s" cycles through
	sortCol map[sortx.Key]int // column whose header shows the sort arrow
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

	// Cache of visible and its row keys; nil when items, filter or sort change.
	rowCache []row
	keyCache []string
}

// setItems replaces the list's items after a load.
func (l *list[T]) setItems(items []T, err error) {
	l.items, l.err, l.loaded = items, err, true
	l.rowCache = nil
}

func (l *list[T]) setSort(key sortx.Key, reverse bool) {
	l.key, l.reverse = key, reverse
	l.rowCache = nil
}

// visible returns the rows that match the filter, in sort order, and their keys.
func (l *list[T]) visible() ([]row, []string) {
	if l.rowCache != nil {
		return l.rowCache, l.keyCache
	}
	items := make([]T, 0, len(l.items))
	for _, it := range l.items {
		if l.filter == "" || l.match(it, l.filter) {
			items = append(items, it)
		}
	}
	l.sort(items, l.key, l.reverse)
	rows := []row{}
	for _, it := range items {
		rows = append(rows, l.rows(it)...)
	}
	keys := make([]string, len(rows))
	for i, r := range rows {
		keys[i] = r.key
	}
	l.rowCache, l.keyCache = rows, keys
	return rows, keys
}

func (l *list[T]) HandleKey(key string) tea.Cmd {
	switch key {
	case "s":
		l.setSort(sortx.Next(l.key, l.keys), l.reverse)
	case "r":
		l.setSort(l.key, !l.reverse)
	default:
		_, keys := l.visible()
		l.cur.navKey(keys, key, l.page)
	}
	return nil
}

func (l *list[T]) SetSize(width, height int) {
	l.width, l.height = width, height
	l.page = max(height-2, 1)
}

func (l *list[T]) SetFilter(filter string) {
	if filter != l.filter {
		l.filter, l.rowCache = filter, nil
	}
}

func (l *list[T]) Filter() string { return l.filter }

func (l *list[T]) View() string {
	return l.view(l.height)
}

func (l *list[T]) view(height int) string {
	switch {
	case l.err != nil:
		return render.Error.Render("error: " + l.err.Error())
	case !l.loaded:
		return render.Dim.Render(l.loading)
	}
	rows, keys := l.visible()
	if len(rows) == 0 {
		return render.Dim.Render(l.empty)
	}
	cells := make([][]string, len(rows))
	for i, r := range rows {
		cells[i] = r.cells
	}
	sel := l.cur.index(keys)
	if l.noHighlight {
		sel = -1
	}
	t := render.Table{Columns: l.sortedColumns(), Rows: cells, Width: l.width}
	return t.Window(sel, height)
}

// sortedColumns returns the columns with an arrow on the sorted one's header:
// ▼ for biggest/newest/Z first, ▲ for the opposite.
func (l *list[T]) sortedColumns() []render.Column {
	i, ok := l.sortCol[l.key]
	if !ok {
		return l.columns
	}
	arrow := " ▲"
	if sortx.Descending(l.key) != l.reverse {
		arrow = " ▼"
	}
	cols := slices.Clone(l.columns)
	cols[i].Header += arrow
	return cols
}

func (l *list[T]) SortKey() sortx.Key { return l.key }

// Selected returns the container of the highlighted row, resolved the same
// way View does so actions never target a row that isn't on screen.
func (l *list[T]) Selected() (string, string, bool) {
	rows, keys := l.visible()
	if len(rows) == 0 {
		return "", "", false
	}
	r := rows[l.cur.index(keys)]
	return r.id, r.name, r.id != ""
}

func (l *list[T]) Help() [][2]string {
	return [][2]string{
		{"s", "cycle sort: " + sortx.Join(l.keys, " / ")},
		{"r", "reverse sort"},
	}
}
