package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/moby/moby/api/types/image"

	"dockernext/internal/docker"
	"dockernext/internal/render"
	"dockernext/internal/sortx"
)

type imagesMsg struct {
	items []image.Summary
	err   error
}

// ImagesView lists local images; one row per tag.
type ImagesView struct {
	ctx     context.Context
	client  *docker.Client
	items   []image.Summary
	key     sortx.Key
	reverse bool
	cur     cursor
	filter  string
	page    int
	loaded  bool
	err     error
}

func NewImagesView(ctx context.Context, c *docker.Client) *ImagesView {
	return &ImagesView{ctx: ctx, client: c, key: sortx.Size, page: 10}
}

func (v *ImagesView) Title() string { return "Images" }

func (v *ImagesView) load() tea.Cmd {
	return func() tea.Msg {
		items, err := v.client.Images(v.ctx)
		return imagesMsg{items, err}
	}
}

// Images change rarely, so they reload only on demand (refreshMsg), not on a timer.
func (v *ImagesView) Init() tea.Cmd { return v.load() }

func (v *ImagesView) Focus() tea.Cmd { return nil }

func (v *ImagesView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case imagesMsg:
		v.items, v.err, v.loaded = msg.items, msg.err, true
	case refreshMsg:
		return v.load()
	}
	return nil
}

type imageRow struct {
	key   string
	cells []string
}

func (v *ImagesView) rows(filter string) []imageRow {
	items := append([]image.Summary(nil), v.items...)
	sortx.Images(items, v.key, v.reverse)
	var out []imageRow
	for _, img := range items {
		if !matches(filter, append([]string{img.ID}, img.RepoTags...)...) {
			continue
		}
		for i, cells := range render.ImageRows(img) {
			out = append(out, imageRow{key: fmt.Sprintf("%s#%d", img.ID, i), cells: cells})
		}
	}
	return out
}

func rowKeys(rows []imageRow) []string {
	keys := make([]string, len(rows))
	for i, r := range rows {
		keys[i] = r.key
	}
	return keys
}

func (v *ImagesView) HandleKey(key string) tea.Cmd {
	switch key {
	case "s":
		v.key = sortx.Next(v.key, sortx.ImageKeys)
	case "r":
		v.reverse = !v.reverse
	default:
		v.cur.navKey(rowKeys(v.rows(v.filter)), key, v.page)
	}
	return nil
}

func (v *ImagesView) View(width, height int, filter string) string {
	switch {
	case v.err != nil:
		return render.Error.Render("error: " + v.err.Error())
	case !v.loaded:
		return render.Dim.Render("loading…")
	}
	v.filter = filter
	v.page = max(height-2, 1)
	rows := v.rows(filter)
	if len(rows) == 0 {
		return render.Dim.Render("no images")
	}
	cells := make([][]string, len(rows))
	for i, r := range rows {
		cells[i] = r.cells
	}
	t := render.Table{Columns: render.ImageColumns, Rows: cells, Width: width}
	return t.Window(v.cur.index(rowKeys(rows)), height)
}

func (v *ImagesView) Selected() (string, string, bool) { return "", "", false }

func (v *ImagesView) Status() string {
	var total int64
	dangling := 0
	for _, img := range v.items {
		total += img.Size
		if len(img.RepoTags) == 0 {
			dangling++
		}
	}
	return fmt.Sprintf("%d images · %s total · %d dangling · %s",
		len(v.items), render.Size(total), dangling, sortLabel(v.key, v.reverse))
}

func (v *ImagesView) Help() [][2]string {
	return [][2]string{
		{"s", "cycle sort: " + sortx.Join(sortx.ImageKeys, " / ")},
		{"r", "reverse sort"},
	}
}
