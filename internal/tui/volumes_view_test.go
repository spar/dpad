package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
)

func volumeContainers() []container.Summary {
	return []container.Summary{
		{ID: "web-id", Names: []string{"/web"}, State: container.StateRunning, Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "data", Destination: "/data", RW: true}, {Type: mount.TypeVolume, Name: "data", Destination: "/readonly"}}},
		{ID: "old-id", Names: []string{"/old-web"}, State: container.StateExited, Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "data", Destination: "/backup"}}},
	}
}
func volumeFake() *fakeClient {
	return &fakeClient{volumes: []volume.Volume{{Name: "data", Driver: "local", Labels: map[string]string{composeProjectLabel: "shop"}}, {Name: "empty", Driver: "local"}}, volumeInfo: volume.Volume{Name: "data", Driver: "local", Mountpoint: "/daemon/data"}}
}
func TestVolumesLazyAndSharedUsage(t *testing.T) {
	f := volumeFake()
	v := NewVolumesView(t.Context(), f)
	if v.Init() != nil || v.Update(refreshMsg{}) != nil {
		t.Fatal("unopened Volumes requested data")
	}
	v.Update(containersMsg{items: volumeContainers()})
	if len(v.usage) != 0 {
		t.Fatal("usage indexed before opening tab")
	}
	cmd := v.Focus()
	v.Update(cmd())
	if len(f.actions) != 1 || f.actions[0] != "volumes " {
		t.Fatal("unexpected API calls", f.actions)
	}
	if len(v.usage["data"]) != 2 {
		t.Fatal("shared usage missing")
	}
	for _, filter := range []string{"data", "LOCAL", "shop", "old-web", "web-id", "old-id"} {
		v.SetFilter(filter)
		rows, _ := v.visible()
		if len(rows) == 0 {
			t.Fatal("missing filter match", filter)
		}
	}
	v.SetFilter("data")
	if !strings.Contains(ansi.Strip(v.View()), "1 running / 2 total") {
		t.Fatal(v.View())
	}
	v.Update(containersMsg{err: errors.New("list failed")})
	if !strings.Contains(v.View(), "unknown") || strings.Contains(v.View(), "unreferenced") || !strings.Contains(v.Status(), "list failed") {
		t.Fatal("failed lookup misreported usage", v.View(), v.Status())
	}
	v.Update(containersMsg{items: volumeContainers()})
	if !strings.Contains(v.View(), "1 running / 2 total") {
		t.Fatal("usage did not recover")
	}
	v.SetFilter("")
	v.SetSize(80, 12)
	if !strings.Contains(ansi.Strip(v.View()), "1 running / 2 total") || strings.Contains(v.View(), "PROJECT") {
		t.Fatal("narrow layout hides usage", v.View())
	}
}

func TestVolumesRefreshCoalescesAndPreservesSelection(t *testing.T) {
	f := volumeFake()
	v := NewVolumesView(t.Context(), f)
	v.Update(v.Focus()())
	v.cur.key = "empty"
	v.HandleKey("r")
	v.SetFilter("local")
	first := v.Focus()
	for range 100 {
		if v.Update(refreshMsg{}) != nil {
			t.Fatal("overlapping refresh scheduled")
		}
	}
	next := v.Update(first())
	if next == nil {
		t.Fatal("pending refresh lost")
	}
	if v.Update(next()) != nil || v.inFlight {
		t.Fatal("refresh did not settle")
	}
	if name, ok := v.SelectedVolume(); !ok || name != "empty" || v.filter != "local" || !v.reverse {
		t.Fatal("view state lost", name)
	}
	f.volumeErr = errors.New("offline")
	v.Update(v.Focus()())
	if _, ok := v.SelectedVolume(); ok {
		t.Fatal("failed list offered details")
	}
	f.volumeErr = nil
	v.Update(v.Focus()())
	if name, _ := v.SelectedVolume(); name != "empty" {
		t.Fatal("recovery lost selection")
	}
	f.volumes = f.volumes[:1]
	v.Update(v.Focus()())
	if name, ok := v.SelectedVolume(); !ok || name != "data" {
		t.Fatal("removed selection did not fall back", name)
	}
	v.Update(volumesMsg{v: v, items: f.volumes, warnings: []string{"driver unavailable\npartial list"}})
	if !strings.Contains(v.Status(), "driver unavailable partial list") || strings.Contains(v.Status(), "\n") {
		t.Fatal(v.Status())
	}
	v.Update(volumesMsg{v: NewVolumesView(t.Context(), f), err: errors.New("foreign")})
	if v.err != nil {
		t.Fatal("foreign load result applied")
	}
}

func TestVolumeDetailsShareUsageAndReturnState(t *testing.T) {
	a := newTestApp()
	f := volumeFake()
	a.client = f
	v := a.tabs[5].(*VolumesView)
	v.client = f
	a.Update(containersMsg{items: volumeContainers()})
	cmd := a.switchTab(5)
	a.Update(cmd())
	v.SetFilter("data")
	a.filter.SetValue("data")
	v.HandleKey("r")
	for _, keyName := range []string{"S", "x", "R", "e"} {
		press(a, keyName)
		if a.confirm != nil || a.shell != nil {
			t.Fatal("Volumes offered mutations")
		}
	}
	for _, keyName := range []string{"enter", "d"} {
		cmd = a.handleKey(key(keyName))
		if cmd == nil {
			t.Fatal("details did not open")
		}
		a.Update(cmd())
		detail := a.volumeDetails
		out := ansi.Strip(detail.View())
		if !strings.Contains(out, "/readonly (ro)") || !strings.Contains(out, "old-web") {
			t.Fatal(out)
		}
		if len(f.actions) < 2 || f.actions[len(f.actions)-1] != "volume inspect data" {
			t.Fatal("details fetched unexpected data", f.actions)
		}
		pending := detail.load()
		a.Update(containersMsg{err: errors.New("usage unavailable")})
		if !strings.Contains(detail.View(), "usage unavailable") {
			t.Fatal("details retained stale usage", detail.View())
		}
		detail.Update(pending())
		if !strings.Contains(detail.Status(), "unknown") {
			t.Fatal(detail.Status())
		}
		a.Update(containersMsg{items: volumeContainers()})
		if !strings.Contains(detail.Status(), "1 running / 2 total") {
			t.Fatal(detail.Status())
		}
		press(a, "esc")
		if a.volumeDetails != nil || detail.ctx.Err() == nil || a.active != 5 || v.filter != "data" || !v.reverse {
			t.Fatal("return lost state or leaked context")
		}
	}
	if strings.Contains(ansi.Strip(a.footer()), "S/x/R") || strings.Contains(ansi.Strip(a.helpView()), "start (or unpause)") {
		t.Fatal("read-only volume view advertises mutations")
	}
}

func TestVolumeDetailsIgnoreStaleAndCancelledResults(t *testing.T) {
	f := volumeFake()
	source := NewVolumesView(t.Context(), f)
	v, first := NewVolumeDetailsView(t.Context(), f, source, "data", 80, 12)
	second := v.load()
	v.Update(first())
	if !v.loading || v.loaded {
		t.Fatal("stale inspect applied")
	}
	f.volumeErr = errors.New("volume removed")
	v.Update(second())
	if !strings.Contains(v.View(), "volume removed") {
		t.Fatal(v.View())
	}
	f.volumeErr = nil
	v.Update(v.load()())
	if v.err != nil {
		t.Fatal("inspect did not recover")
	}
	late := v.load()
	v.Close()
	v.Update(late())
	if v.load() != nil {
		t.Fatal("request after close")
	}
	ctx, cancel := context.WithCancel(t.Context())
	list := NewVolumesView(ctx, f)
	cancel()
	if list.Focus() != nil {
		t.Fatal("load after parent cancellation")
	}
}

func BenchmarkVolumesUsageAndView(b *testing.B) {
	f := volumeFake()
	f.volumes = make([]volume.Volume, 1000)
	containers := make([]container.Summary, 1000)
	for i := range f.volumes {
		name := fmt.Sprintf("data-%04d", i)
		f.volumes[i] = volume.Volume{Name: name, Driver: "local"}
		containers[i] = container.Summary{ID: fmt.Sprint(i), Names: []string{fmt.Sprint("web-", i)}, State: container.StateRunning, Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: name, Destination: "/data"}}}
	}
	v := NewVolumesView(b.Context(), f)
	v.SetSize(160, 35)
	v.Update(v.Focus()())
	b.ReportAllocs()
	for b.Loop() {
		v.Update(containersMsg{items: containers})
		v.View()
	}
}

func TestSixTabsKeepActiveVolumeVisibleAtNarrowWidths(t *testing.T) {
	a := newTestApp()
	a.active = 5
	for _, width := range []int{80, 40} {
		a.resize(width, 24)
		bar := ansi.Strip(a.tabBar())
		if !strings.Contains(bar, "6 Volumes") || ansi.StringWidth(bar) > width {
			t.Fatal(width, bar)
		}
	}
}
