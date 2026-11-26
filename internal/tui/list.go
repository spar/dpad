package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"dpad/internal/docker"
	"dpad/internal/render"
	"dpad/internal/sortx"
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
	noHighlight bool // hide the cursor (standalone `dp stats`)
	cur         cursor
	filter      string
	width       int
	height      int
	page        int // rows moved by pgup/pgdn
	top         int // first row in the viewport
	shown       int // rows drawn by the last view

	// Cache of visible and its row keys; nil when items, filter or sort change.
	rowCache                 []row
	keyCache                 []string
	rowTheme                 int // theme generation rowCache was styled with
	rowVersion, tableVersion uint64
	table                    *render.PreparedTable
	tableRows                []row
	tableColumns             []render.Column
}

// setItems replaces the list's items after a load.
func (l *list[T]) setItems(items []T, err error) {
	l.items, l.err, l.loaded = items, err, true
	l.rowCache = nil
}

// preserveSelection keeps the selected row, or its position if it disappears.
// Updates may also rebuild indexes used by filtering and row rendering.
func (l *list[T]) preserveSelection(update func()) {
	_, oldKeys := l.visible()
	selected := l.cur.index(oldKeys)
	update()
	_, keys := l.visible()
	if len(keys) > 0 && !slices.Contains(keys, l.cur.key) {
		l.cur.key = keys[min(selected, len(keys)-1)]
	}
}

func (l *list[T]) setSort(key sortx.Key, reverse bool) {
	l.key, l.reverse = key, reverse
	l.rowCache = nil
}

// visible returns the rows that match the filter, in sort order, and their keys.
func (l *list[T]) visible() ([]row, []string) {
	if l.rowCache != nil && l.rowTheme == render.ThemeGeneration() {
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
	l.rowVersion++
	l.rowCache, l.keyCache, l.rowTheme = rows, keys, render.ThemeGeneration()
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
		l.shown = 0
		return render.Error.Render("error: " + l.err.Error())
	case !l.loaded:
		l.shown = 0
		return render.Dim.Render(l.loading)
	}
	rows, keys := l.visible()
	if len(rows) == 0 {
		l.table = nil
		l.tableRows, l.tableColumns = nil, nil
		l.top, l.shown = 0, 0
		return render.Dim.Render(l.empty)
	}
	if l.table == nil || l.tableVersion != l.rowVersion {
		columns := l.sortedColumns()
		if l.table == nil || !slices.Equal(columns, l.tableColumns) || !slices.EqualFunc(rows, l.tableRows, func(a, b row) bool { return slices.Equal(a.cells, b.cells) }) {
			cells := make([][]string, len(rows))
			for i, r := range rows {
				cells[i] = r.cells
			}
			l.table = (render.Table{Columns: columns, Rows: cells, Width: l.width}).Prepare()
			l.tableColumns = slices.Clone(columns)
		}
		l.tableRows = rows
		l.tableVersion = l.rowVersion
	}
	l.table.Width = l.width
	sel := l.cur.index(keys)
	if l.noHighlight {
		sel = -1
	}
	avail := max(height-1, 1)
	l.top = min(l.top, max(len(rows)-avail, 0))
	if sel < 0 {
		l.top = 0
	} else if sel < l.top {
		l.top = sel
	} else if sel >= l.top+avail {
		l.top = sel - avail + 1
	}
	l.shown = min(avail, len(rows)-l.top)
	return l.table.WindowFrom(sel, height, l.top)
}

// clickRow selects the row drawn on line of the last view, where line 0 is
// the header. It reports whether a row was there.
func (l *list[T]) clickRow(line int) bool {
	_, keys := l.visible()
	i := l.top + line - 1
	if line < 1 || line > l.shown || i >= len(keys) {
		return false
	}
	l.cur.key = keys[i]
	return true
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

func (l *list[T]) sortKeys() string {
	if len(l.keys) < 2 {
		return "r"
	}
	return "s/r"
}

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
	help := [][2]string{{"r", "reverse sort"}}
	if len(l.keys) > 1 {
		help = slices.Insert(help, 0, [2]string{"s", "cycle sort: " + sortx.Join(l.keys, " / ")})
	}
	return help
}
