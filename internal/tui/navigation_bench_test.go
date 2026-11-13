package tui

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
)

func navigationApp(tb testing.TB, active int) *App {
	tb.Helper()
	a := newTestApp()
	a.ctx = tb.Context()
	a.active = active
	items := make([]container.Summary, 1000)
	volumes := make([]volume.Volume, len(items))
	for i := range items {
		name := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprint(i))))
		items[i] = composeContainer(fmt.Sprintf("web-%04d", i), fmt.Sprintf("project-%03d", i/10), "web", "running")
		items[i].Mounts = []container.MountPoint{{Type: mount.TypeVolume, Name: name, Destination: "/data"}}
		volumes[i] = volume.Volume{Name: name, Driver: "local", Labels: map[string]string{composeProjectLabel: fmt.Sprintf("project-%03d", i/10)}}
	}
	for _, tab := range a.tabs {
		tab.Update(containersMsg{items: items})
	}
	v := a.tabs[5].(*VolumesView)
	v.requested = true
	v.Update(volumesMsg{v: v, items: volumes})
	a.resize(140, 40)
	a.render()
	return a
}

func BenchmarkNavigationFrame(b *testing.B) {
	for _, active := range []int{0, 3, 5} {
		b.Run(newTestApp().tabs[active].Title(), func(b *testing.B) {
			a := navigationApp(b, active)
			for range 100 {
				a.handleKey(key("j"))
			}
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				direction := "j"
				if i%20 >= 10 {
					direction = "k"
				}
				a.handleKey(key(direction))
				a.View()
				i++
			}
		})
	}
}
func BenchmarkNavigationRefresh(b *testing.B) {
	for _, active := range []int{3, 5} {
		b.Run(newTestApp().tabs[active].Title(), func(b *testing.B) {
			a := navigationApp(b, active)
			containers := a.tabs[0].(*ContainersView).items
			b.ReportAllocs()
			for b.Loop() {
				a.Update(containersMsg{items: containers})
				a.View()
			}
		})
	}
}
