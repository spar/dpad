package tui

import (
	"context"
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/moby/moby/api/types/image"

	"dpad/internal/docker"
	"dpad/internal/render"
	"dpad/internal/sortx"
)

type imagesMsg struct {
	items []image.Summary
	err   error
}

// ImagesView lists local images; one row per tag.
type ImagesView struct {
	list[image.Summary]
	loader
	containerUsage
	ctx    context.Context
	client dockerClient
}

func NewImagesView(ctx context.Context, c dockerClient) *ImagesView {
	v := &ImagesView{ctx: ctx, client: c}
	v.list = list[image.Summary]{
		columns: append(slices.Clone(render.ImageColumns), render.Column{Header: "USAGE", Min: 19}),
		keys:    sortx.ImageKeys,
		sortCol: map[sortx.Key]int{sortx.Name: 0, sortx.Created: 3, sortx.Size: 4},
		key:     sortx.Size,
		match: func(img image.Summary, f string) bool {
			return matches(f, append([]string{img.ID}, img.RepoTags...)...)
		},
		sort: sortx.Images,
		rows: func(img image.Summary) []row {
			var out []row
			for i, cells := range render.ImageRows(img) {
				ref := img.ID
				if len(img.RepoTags) > 0 {
					ref = img.RepoTags[i]
				}
				usage := "loading…"
				if v.usageErr != nil {
					usage = "unknown (refresh)"
				} else if v.usageLoaded {
					usage = render.ImageUsage(docker.ContainersUsingImage(v.containers, img.ID))
				}
				out = append(out, row{key: img.ID + "#" + ref, cells: append(cells, usage), image: &docker.ImageRemoval{ID: img.ID, Reference: ref, UntagOnly: len(img.RepoTags) > 1}})
			}
			return out
		},
		loading: "loading…",
		empty:   "no images",
	}
	return v
}

func (v *ImagesView) Title() string { return "Images" }

func (v *ImagesView) load() tea.Cmd {
	if !v.begin() {
		return nil
	}
	return func() tea.Msg {
		items, err := v.client.Images(v.ctx)
		return imagesMsg{items, err}
	}
}

// Images change rarely, so they reload when the tab is opened and on demand
// (refreshMsg), not on a timer.
func (v *ImagesView) Init() tea.Cmd { return v.load() }

func (v *ImagesView) Focus() tea.Cmd {
	if v.inFlight {
		return nil
	}
	return v.load()
}

func (v *ImagesView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case imagesMsg:
		again := v.end()
		v.preserveSelection(func() {
			v.setItems(msg.items, msg.err)
		})
		if again {
			return v.load()
		}
	case containersMsg:
		v.setContainers(msg)
		v.rowCache = nil
	case refreshMsg:
		return v.load()
	}
	return nil
}

func (v *ImagesView) SelectedImage() (docker.ImageRemoval, bool) {
	rows, keys := v.visible()
	if !v.loaded || v.err != nil || len(rows) == 0 {
		return docker.ImageRemoval{}, false
	}
	r := rows[v.cur.index(keys)]
	return *r.image, true
}

func (v *ImagesView) Help() [][2]string {
	return append(v.list.Help(), [2]string{"d / enter", "inspect selected image and its container usage"}, [2]string{"x", "remove selected tag or image (asks y/N)"})
}

func (v *ImagesView) Status() string {
	return render.ImageSummary(v.items) + " · " + sortLabel(v.key, v.reverse)
}
