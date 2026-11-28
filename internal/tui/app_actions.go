package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/spar/dpad/internal/docker"
	"github.com/spar/dpad/internal/render"
)

type actionDoneMsg struct {
	text  string
	err   error
	image *docker.ImageRemoval
}

// pending is an action waiting for y/n confirmation.
type pending struct {
	verb, id, name string
	run            func(context.Context, string) error
	question       string
	image          *docker.ImageRemoval
	// start, when set, replaces run: it returns the command to perform.
	start func() tea.Cmd
}

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

func (a *App) openSelectedContainer() tea.Cmd {
	if id, name, ok := a.tabs[a.active].Selected(); ok {
		var cmd tea.Cmd
		a.details, cmd = NewDetailsView(a.ctx, a.client, id, name, a.width, a.bodyHeight())
		return cmd
	}
	return nil
}

func (a *App) openProjectAction(verb string) (tea.Cmd, bool) {
	v, ok := a.tabs[a.active].(*ProjectsView)
	if !ok {
		return nil, false
	}
	if !v.loaded || v.err != nil {
		return nil, true
	}
	if p := v.selectedProject(); p != nil {
		var cmd tea.Cmd
		a.projectAction, cmd = newProjectAction(a.ctx, a.client, p.name, verb, a.width, a.bodyHeight())
		return cmd, true
	}
	return nil, false
}

func (a *App) ask(verb string, run func(context.Context, string) error) {
	if id, name, ok := a.tabs[a.active].Selected(); ok {
		a.confirm = &pending{verb: verb, id: id, name: name, run: run}
	}
}

func (a *App) openSelectedVolume() tea.Cmd {
	source, ok := a.tabs[a.active].(*VolumesView)
	if !ok {
		return nil
	}
	name, ok := source.SelectedVolume()
	if !ok {
		return nil
	}
	var cmd tea.Cmd
	a.volumeDetails, cmd = NewVolumeDetailsView(a.ctx, a.client, source, name, a.width, a.bodyHeight())
	return cmd
}

func (a *App) openSelectedNetwork() tea.Cmd {
	source, ok := a.tabs[a.active].(*NetworksView)
	if !ok {
		return nil
	}
	id, name, ok := source.SelectedNetwork()
	if !ok {
		return nil
	}
	var cmd tea.Cmd
	a.networkDetails, cmd = NewNetworkDetailsView(a.ctx, a.client, id, name, a.width, a.bodyHeight())
	return cmd
}

func (a *App) openSelectedResource() (tea.Cmd, bool) {
	switch a.tabs[a.active].(type) {
	case *NetworksView:
		return a.openSelectedNetwork(), true
	case *VolumesView:
		return a.openSelectedVolume(), true
	case *ImagesView:
		return a.openSelectedImage(), true
	}
	return nil, false
}
