package docker

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

type volumeEngine struct {
	engine
	err       error
	inspected string
}

func (f *volumeEngine) VolumeList(context.Context, client.VolumeListOptions) (client.VolumeListResult, error) {
	return client.VolumeListResult{Items: []volume.Volume{{Name: "data"}}, Warnings: []string{"driver unavailable"}}, f.err
}
func (f *volumeEngine) VolumeInspect(_ context.Context, name string, _ client.VolumeInspectOptions) (client.VolumeInspectResult, error) {
	f.inspected = name
	return client.VolumeInspectResult{Volume: volume.Volume{Name: name, Mountpoint: "/daemon/data"}}, f.err
}
func TestVolumesPreserveWarningsAndErrors(t *testing.T) {
	f := &volumeEngine{}
	c := &Client{api: f}
	items, warnings, err := c.Volumes(t.Context())
	if err != nil || len(items) != 1 || items[0].Name != "data" || len(warnings) != 1 {
		t.Fatal(items, warnings, err)
	}
	info, err := c.Volume(t.Context(), "data")
	if err != nil || f.inspected != "data" || info.Mountpoint != "/daemon/data" {
		t.Fatal(info, err)
	}
	f.err = errors.New("daemon unavailable")
	if _, _, err = c.Volumes(t.Context()); !errors.Is(err, f.err) {
		t.Fatal("list error lost", err)
	}
	if _, err = c.Volume(t.Context(), "gone"); !errors.Is(err, f.err) {
		t.Fatal("inspect error lost", err)
	}
}
func TestVolumeUsersIncludeStoppedAndDeduplicateContainers(t *testing.T) {
	items := []container.Summary{
		{ID: "running", Names: []string{"/web"}, State: container.StateRunning, Mounts: []container.MountPoint{
			{Type: mount.TypeVolume, Name: "data", Destination: "/data", RW: true},
			{Type: mount.TypeVolume, Name: "data", Destination: "/archive", RW: false},
			{Type: mount.TypeVolume, Name: "anonymous-id", Destination: "/cache", RW: true},
			{Type: mount.TypeBind, Name: "data", Destination: "/bind"},
			{Type: mount.TypeTmpfs, Destination: "/tmp"},
			{Type: mount.TypeVolume, Destination: "/missing-name"},
		}},
		{ID: "stopped", Names: []string{"/old-web"}, State: container.StateExited, Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "data", Destination: "/backup"}}},
	}
	got := VolumeUsers(items)
	if len(got) != 2 || len(got["data"]) != 2 || len(got["anonymous-id"]) != 1 {
		t.Fatalf("wrong references: %+v", got)
	}
	first := got["data"][0]
	if first.Name != "web" || first.State != container.StateRunning || len(first.Mounts) != 2 || first.Mounts[0].ReadOnly || !first.Mounts[1].ReadOnly {
		t.Fatal(first)
	}
	if got["data"][1].State != container.StateExited {
		t.Fatal("stopped reference lost")
	}
	if items[0].Names[0] != "/web" {
		t.Fatal("input mutated")
	}
}
func BenchmarkVolumeUsers(b *testing.B) {
	items := make([]container.Summary, 1000)
	for i := range items {
		items[i] = container.Summary{ID: fmt.Sprint(i), Names: []string{fmt.Sprint("web-", i)}, Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: fmt.Sprint("data-", i), Destination: "/data", RW: true}, {Type: mount.TypeVolume, Name: "shared", Destination: "/shared"}}}
	}
	b.ReportAllocs()
	for b.Loop() {
		VolumeUsers(items)
	}
}
