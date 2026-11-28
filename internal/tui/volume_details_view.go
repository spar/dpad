package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/moby/moby/api/types/volume"

	"github.com/spar/dpad/internal/render"
)

type volumeDetailsMsg struct {
	v    *VolumeDetailsView
	gen  int
	info volume.Volume
	err  error
}

type VolumeDetailsView struct {
	detailView
	client dockerClient
	source *VolumesView
	name   string
	info   volume.Volume
	loaded bool
}

func NewVolumeDetailsView(ctx context.Context, c dockerClient, source *VolumesView, name string, width, height int) (*VolumeDetailsView, tea.Cmd) {
	v := &VolumeDetailsView{detailView: newDetailView(ctx, width, height, "loading volume details…"), client: c, source: source, name: name}
	return v, v.load()
}
func (v *VolumeDetailsView) load() tea.Cmd {
	if v.ctx.Err() != nil {
		return nil
	}
	gen := v.startLoad()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(v.ctx, 5*time.Second)
		defer cancel()
		info, err := v.client.Volume(ctx, v.name)
		return volumeDetailsMsg{v: v, gen: gen, info: info, err: err}
	}
}
func (v *VolumeDetailsView) content() {
	v.vp.SetContent(render.VolumeDetails(v.info, v.source.usage[v.name], v.source.usageKnown()))
}
func (v *VolumeDetailsView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case volumeDetailsMsg:
		if msg.v != v || msg.gen != v.gen || v.ctx.Err() != nil {
			return nil
		}
		v.loading = false
		v.loaded = true
		v.err = msg.err
		if msg.err != nil {
			v.vp.SetContent(render.Error.Render("error: " + connectionError(msg.err)))
		} else {
			v.info = msg.info
			v.content()
		}
		return nil
	case containersMsg:
		if v.loaded && v.err == nil {
			v.content()
		}
		return nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+r":
			return tea.Batch(v.load(), func() tea.Msg { return refreshMsg{} })
		}
	}
	return v.updateViewport(msg)
}
func (v *VolumeDetailsView) Status() string {
	return v.status("volume: "+render.Accent.Render(v.name), " · "+render.VolumeUsage(v.source.usage[v.name], v.source.usageKnown()))
}

func (v *VolumeDetailsView) Footer() string {
	return hints("esc", "back", detailHints, "scroll", "←→", "pan", "g/G", "top/bottom", "ctrl+r", "refresh")
}

func (v *VolumeDetailsView) handleKey(a *App, msg tea.KeyPressMsg) tea.Cmd {
	if closeKey(msg.String()) {
		v.Close()
		a.volumeDetails = nil
		return nil
	}
	return v.Update(msg)
}
