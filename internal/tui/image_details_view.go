package tui

import (
	"context"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"dockernext/internal/docker"
	"dockernext/internal/render"
)

type imageDetailsMsg struct {
	v    *ImageDetailsView
	gen  int
	info docker.ImageInfo
	err  error
}

type ImageDetailsView struct {
	ctx     context.Context
	cancel  context.CancelFunc
	client  dockerClient
	removal docker.ImageRemoval
	vp      viewport.Model
	gen     int
	loading bool
	err     error
}

func NewImageDetailsView(ctx context.Context, c dockerClient, removal docker.ImageRemoval, width, height int) (*ImageDetailsView, tea.Cmd) {
	ctx, cancel := context.WithCancel(ctx)
	v := &ImageDetailsView{ctx: ctx, cancel: cancel, client: c, removal: removal, vp: viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))}
	v.vp.SetContent("loading…")
	return v, v.load()
}

func (v *ImageDetailsView) load() tea.Cmd {
	v.gen++
	v.loading = true
	gen := v.gen
	return func() tea.Msg {
		info, err := v.client.ImageDetails(v.ctx, v.removal.ID)
		return imageDetailsMsg{v: v, gen: gen, info: info, err: err}
	}
}

func (v *ImageDetailsView) Close() { v.cancel() }

func (v *ImageDetailsView) SetSize(width, height int) {
	v.vp.SetWidth(width)
	v.vp.SetHeight(height)
}

func (v *ImageDetailsView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case imageDetailsMsg:
		if msg.v != v || msg.gen != v.gen {
			return nil
		}
		v.loading, v.err = false, msg.err
		if msg.err != nil {
			v.vp.SetContent(render.Error.Render("error: " + msg.err.Error()))
		} else {
			v.removal.UntagOnly = len(msg.info.Image.RepoTags) > 1
			v.vp.SetContent(render.ImageDetails(msg.info))
		}
		return nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+r":
			return v.load()
		case "g", "home":
			v.vp.GotoTop()
			return nil
		case "G", "end":
			v.vp.GotoBottom()
			return nil
		}
	}
	var cmd tea.Cmd
	v.vp, cmd = v.vp.Update(msg)
	return cmd
}

func (v *ImageDetailsView) View() string { return v.vp.View() }

func (v *ImageDetailsView) Status() string {
	s := "image: " + render.Accent.Render(v.removal.Reference)
	if v.loading {
		return s + " · loading…"
	}
	if v.err != nil {
		return s + " · inspect failed (ctrl+r to retry)"
	}
	return s
}
