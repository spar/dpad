package tui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/moby/moby/api/types/volume"

	"dockernext/internal/render"
)

type volumeDetailsMsg struct {
	v    *VolumeDetailsView
	gen  int
	info volume.Volume
	err  error
}

type VolumeDetailsView struct {
	ctx             context.Context
	cancel          context.CancelFunc
	client          dockerClient
	source          *VolumesView
	name            string
	info            volume.Volume
	vp              viewport.Model
	gen             int
	loading, loaded bool
	err             error
}

func NewVolumeDetailsView(ctx context.Context, c dockerClient, source *VolumesView, name string, width, height int) (*VolumeDetailsView, tea.Cmd) {
	ctx, cancel := context.WithCancel(ctx)
	v := &VolumeDetailsView{ctx: ctx, cancel: cancel, client: c, source: source, name: name, vp: viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))}
	v.vp.SetContent("loading volume details…")
	return v, v.load()
}
func (v *VolumeDetailsView) load() tea.Cmd {
	if v.ctx.Err() != nil {
		return nil
	}
	v.gen++
	v.loading = true
	gen := v.gen
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(v.ctx, 5*time.Second)
		defer cancel()
		info, err := v.client.Volume(ctx, v.name)
		return volumeDetailsMsg{v: v, gen: gen, info: info, err: err}
	}
}
func (v *VolumeDetailsView) Close()                    { v.cancel() }
func (v *VolumeDetailsView) SetSize(width, height int) { v.vp.SetWidth(width); v.vp.SetHeight(height) }
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
func (v *VolumeDetailsView) View() string { return v.vp.View() }
func (v *VolumeDetailsView) Status() string {
	s := "volume: " + render.Accent.Render(v.name)
	if v.loading {
		return s + " · loading…"
	}
	if v.err != nil {
		return s + " · inspect failed (ctrl+r to retry)"
	}
	return s + " · " + render.VolumeUsage(v.source.usage[v.name], v.source.usageKnown())
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
