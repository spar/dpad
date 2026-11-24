package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/volume"

	"dpad/internal/docker"
)

func TestVolumeDetailsReferencesAndMetadata(t *testing.T) {
	v := volume.Volume{Name: "data", Driver: "local", Scope: "local", Mountpoint: "/daemon/data", Labels: map[string]string{"z": "last", "a": "first"}}
	users := []docker.VolumeUse{
		{ID: "2", Name: "web", State: container.StateRunning, Mounts: []docker.VolumeDestination{{Path: "/data"}, {Path: "/readonly", ReadOnly: true}}},
		{ID: "1", Name: "old-web", State: container.StateExited, Mounts: []docker.VolumeDestination{{Path: "/backup"}}},
	}
	out := ansi.Strip(VolumeDetails(v, users, true))
	for _, want := range []string{"data", "local", "Mountpoint (daemon host):  /daemon/data", "1 running / 2 total", "old-web", "exited", "/data (rw)", "/readonly (ro)", "/backup (rw)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
	if strings.Index(out, "a=first") > strings.Index(out, "z=last") {
		t.Fatal("labels not sorted")
	}
	if got := VolumeUsage(nil, false); got != "unknown" {
		t.Fatal(got)
	}
	if got := VolumeUsage(nil, true); got != "unreferenced" {
		t.Fatal(got)
	}
	unknown := VolumeDetails(v, nil, false)
	if strings.Contains(unknown, "none reported") || strings.Contains(unknown, "unreferenced") || !strings.Contains(unknown, "usage unavailable") {
		t.Fatal(unknown)
	}
}
