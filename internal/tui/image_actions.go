package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"dockernext/internal/docker"
	"dockernext/internal/render"
)

func (a *App) openSelectedImage() tea.Cmd {
	v, ok := a.tabs[a.active].(*ImagesView)
	if !ok {
		return nil
	}
	removal, ok := v.SelectedImage()
	if !ok {
		return nil
	}
	var cmd tea.Cmd
	a.imageDetails, cmd = NewImageDetailsView(a.ctx, a.client, removal, a.width, a.bodyHeight())
	return cmd
}

func (a *App) askImageRemoval(removal docker.ImageRemoval) {
	verb := "remove image"
	name := removal.Reference
	question := fmt.Sprintf("Remove image %s (last tag; image data may be deleted)?", removal.Reference)
	if removal.UntagOnly {
		verb = "remove tag"
		question = fmt.Sprintf("Remove tag %s only? Other tags and image data remain.", removal.Reference)
	} else if removal.Reference == removal.ID {
		name = render.ShortID(removal.ID)
		question = fmt.Sprintf("Remove untagged image %s?", render.ShortID(removal.ID))
	}
	a.confirm = &pending{verb: verb, id: removal.Reference, name: name, question: question, image: &removal, run: func(ctx context.Context, _ string) error { return a.client.RemoveImage(ctx, removal) }}
}
