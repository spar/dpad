package tui

import (
	"context"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/moby/moby/api/types/container"

	"dockernext/internal/render"
)

type detailsMsg struct {
	v    *DetailsView
	gen  int
	info container.InspectResponse
	err  error
}

// DetailsView shows a scrollable snapshot of one container's inspect data.
type DetailsView struct {
	ctx      context.Context
	cancel   context.CancelFunc
	client   dockerClient
	id, name string
	vp       viewport.Model
	gen      int
	loading  bool
	err      error
}

func NewDetailsView(ctx context.Context, c dockerClient, id, name string, width, height int) (*DetailsView, tea.Cmd) {
	ctx, cancel := context.WithCancel(ctx)
	v := &DetailsView{
		ctx: ctx, cancel: cancel, client: c, id: id, name: name,
		vp: viewport.New(viewport.WithWidth(width), viewport.WithHeight(height)),
	}
	v.vp.SetContent("loading…")
	return v, v.load()
}

func (v *DetailsView) load() tea.Cmd {
	v.gen++
	v.loading = true
	gen := v.gen
	return func() tea.Msg {
		info, err := v.client.Inspect(v.ctx, v.id)
		return detailsMsg{v: v, gen: gen, info: info, err: err}
	}
}

func (v *DetailsView) Close() { v.cancel() }

func (v *DetailsView) SetSize(width, height int) {
	v.vp.SetWidth(width)
	v.vp.SetHeight(height)
}

func (v *DetailsView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case detailsMsg:
		if msg.v != v || msg.gen != v.gen {
			return nil
		}
		v.loading, v.err = false, msg.err
		if msg.err != nil {
			v.vp.SetContent(render.Error.Render("error: " + msg.err.Error()))
		} else {
			v.vp.SetContent(render.ContainerDetails(msg.info))
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

func (v *DetailsView) View() string { return v.vp.View() }

func (v *DetailsView) Status() string {
	s := "details: " + render.Accent.Render(v.name)
	if v.loading {
		return s + " · loading…"
	}
	if v.err != nil {
		return s + " · inspect failed (ctrl+r to retry)"
	}
	return s
}
