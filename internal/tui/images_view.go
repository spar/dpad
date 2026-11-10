package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/moby/moby/api/types/image"

	"dockernext/internal/render"
	"dockernext/internal/sortx"
)

type imagesMsg struct {
	items []image.Summary
	err   error
}

// ImagesView lists local images; one row per tag.
type ImagesView struct {
	list[image.Summary]
	ctx    context.Context
	client dockerClient
}

func NewImagesView(ctx context.Context, c dockerClient) *ImagesView {
	return &ImagesView{ctx: ctx, client: c, list: list[image.Summary]{
		columns: render.ImageColumns,
		keys:    sortx.ImageKeys,
		key:     sortx.Size,
		match: func(img image.Summary, f string) bool {
			return matches(f, append([]string{img.ID}, img.RepoTags...)...)
		},
		sort: sortx.Images,
		rows: func(img image.Summary) []row {
			var out []row
			for i, cells := range render.ImageRows(img) {
				out = append(out, row{key: fmt.Sprintf("%s#%d", img.ID, i), cells: cells})
			}
			return out
		},
		loading: "loading…",
		empty:   "no images",
	}}
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
		v.setItems(msg.items, msg.err)
	case refreshMsg:
		return v.load()
	}
	return nil
}

func (v *ImagesView) Status() string {
	return render.ImageSummary(v.items) + " · " + sortLabel(v.key, v.reverse)
}
