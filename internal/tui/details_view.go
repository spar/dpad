package tui

import (
	"context"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/moby/moby/api/types/container"

	"github.com/spar/dpad/internal/docker"
	"github.com/spar/dpad/internal/render"
)

type detailsMsg struct {
	v    *DetailsView
	gen  int
	info container.InspectResponse
	err  error
}

// DetailsView shows a scrollable snapshot of one container's inspect data.
type DetailsView struct {
	detailView
	client   dockerClient
	id, name string
}

func NewDetailsView(ctx context.Context, c dockerClient, id, name string, width, height int) (*DetailsView, tea.Cmd) {
	v := &DetailsView{
		detailView: newDetailView(ctx, width, height, "loading…"),
		client:     c, id: id, name: name,
	}
	return v, v.load()
}

func (v *DetailsView) load() tea.Cmd {
	gen := v.startLoad()
	return func() tea.Msg {
		info, err := v.client.Inspect(v.ctx, v.id)
		return detailsMsg{v: v, gen: gen, info: info, err: err}
	}
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
		}
	}
	return v.updateViewport(msg)
}

func (v *DetailsView) Status() string {
	return v.status("details: "+render.Accent.Render(v.name), "")
}

type imageDetailsMsg struct {
	v    *ImageDetailsView
	gen  int
	info docker.ImageInfo
	err  error
}

type ImageDetailsView struct {
	detailView
	client  dockerClient
	removal docker.ImageRemoval
}

func NewImageDetailsView(ctx context.Context, c dockerClient, removal docker.ImageRemoval, width, height int) (*ImageDetailsView, tea.Cmd) {
	v := &ImageDetailsView{detailView: newDetailView(ctx, width, height, "loading…"), client: c, removal: removal}
	return v, v.load()
}

func (v *ImageDetailsView) load() tea.Cmd {
	gen := v.startLoad()
	return func() tea.Msg {
		info, err := v.client.ImageDetails(v.ctx, v.removal.ID)
		return imageDetailsMsg{v: v, gen: gen, info: info, err: err}
	}
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
		}
	}
	return v.updateViewport(msg)
}

func (v *ImageDetailsView) Status() string {
	return v.status("image: "+render.Accent.Render(v.removal.Reference), "")
}

type detailView struct {
	ctx     context.Context
	cancel  context.CancelFunc
	vp      viewport.Model
	gen     int
	loading bool
	err     error
}

func newDetailView(ctx context.Context, width, height int, loading string) detailView {
	ctx, cancel := context.WithCancel(ctx)
	v := detailView{ctx: ctx, cancel: cancel, vp: viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))}
	v.vp.SetContent(loading)
	return v
}

func (v *detailView) startLoad() int {
	v.gen++
	v.loading = true
	return v.gen
}

func (v *detailView) Close() { v.cancel() }
func (v *detailView) SetSize(width, height int) {
	v.vp.SetWidth(width)
	v.vp.SetHeight(height)
}
func (v *detailView) View() string { return v.vp.View() }
func (v *detailView) updateViewport(msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch msg.String() {
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
func (v *detailView) status(label, suffix string) string {
	if v.loading {
		return label + " · loading…"
	}
	if v.err != nil {
		return label + " · inspect failed (ctrl+r to retry)"
	}
	return label + suffix
}

const detailHints = "↑↓/pgup/pgdn"

func (v *DetailsView) Footer() string {
	return hints("esc", "back", detailHints, "scroll", "←→", "pan", "g/G", "top/bottom", "ctrl+r", "refresh", "enter", "logs", "e", "shell")
}

func (v *DetailsView) handleKey(a *App, msg tea.KeyPressMsg) tea.Cmd {
	switch key := msg.String(); {
	case closeKey(key):
		v.Close()
		a.details = nil
		return nil
	case key == "e":
		return a.openShell(v.id, v.name)
	case key == "enter":
		var cmd tea.Cmd
		a.logs, cmd = NewLogsView(a.ctx, a.client, v.id, v.name, a.width, a.bodyHeight())
		return cmd
	}
	return v.Update(msg)
}

func (v *ImageDetailsView) Footer() string {
	return hints("esc", "back", detailHints, "scroll", "←→", "pan", "g/G", "top/bottom", "ctrl+r", "refresh", "x", "remove tag/image")
}

func (v *ImageDetailsView) handleKey(a *App, msg tea.KeyPressMsg) tea.Cmd {
	switch key := msg.String(); {
	case closeKey(key):
		v.Close()
		a.imageDetails = nil
		return nil
	case key == "x":
		if !v.loading && v.err == nil {
			a.askImageRemoval(v.removal)
		}
		return nil
	}
	return v.Update(msg)
}
