package docker

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/moby/moby/api/types/build"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

func ids(targets []PruneTarget) []string {
	out := []string{}
	for _, t := range targets {
		out = append(out, t.ID)
	}
	return out
}

func TestStoppedContainers(t *testing.T) {
	items := []container.Summary{
		{ID: "run", Names: []string{"/run"}, State: container.StateRunning},
		{ID: "pause", Names: []string{"/pause"}, State: container.StatePaused},
		{ID: "exit", Names: []string{"/b"}, State: container.StateExited},
		{ID: "new", Names: []string{"/a"}, State: container.StateCreated},
		{ID: "dead", Names: []string{"/c"}, State: container.StateDead},
	}
	if got := ids(StoppedContainers(items)); !slices.Equal(got, []string{"new", "exit", "dead"}) {
		t.Fatal(got)
	}
}

func TestUnusedImages(t *testing.T) {
	images := []image.Summary{
		{ID: "old-dangling", Created: 1},
		{ID: "new-dangling", RepoTags: []string{"<none>:<none>"}, Created: 3},
		{ID: "tagged", RepoTags: []string{"a:1", "b:1"}, Created: 2},
		{ID: "used", Created: 4},
	}
	used := []container.Summary{{ID: "c", ImageID: "used", State: container.StateExited}}
	if got := ids(UnusedImages(images, used, false)); !slices.Equal(got, []string{"new-dangling", "old-dangling"}) {
		t.Fatalf("dangling = %v", got)
	}
	all := UnusedImages(images, used, true)
	if got := ids(all); !slices.Equal(got, []string{"new-dangling", "tagged", "old-dangling"}) {
		t.Fatalf("all = %v", got)
	}
	if !slices.Equal(all[1].Tags, []string{"a:1", "b:1"}) || len(all[0].Tags) != 0 {
		t.Fatalf("tags = %v / %v", all[1].Tags, all[0].Tags)
	}
}

func TestUnusedVolumes(t *testing.T) {
	anon := map[string]string{anonymousVolumeLabel: ""}
	volumes := []volume.Volume{{Name: "anon", Labels: anon}, {Name: "named"}, {Name: "mounted", Labels: anon}}
	containers := []container.Summary{{ID: "c", State: container.StateExited, Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "mounted"}}}}
	if got := ids(UnusedVolumes(volumes, containers, false)); !slices.Equal(got, []string{"anon"}) {
		t.Fatalf("anonymous = %v", got)
	}
	if got := ids(UnusedVolumes(volumes, containers, true)); !slices.Equal(got, []string{"anon", "named"}) {
		t.Fatalf("all = %v", got)
	}
}

func TestUnusedNetworks(t *testing.T) {
	networks := []network.Summary{
		{Network: network.Network{ID: "1", Name: "bridge"}},
		{Network: network.Network{ID: "2", Name: "app_default"}},
		{Network: network.Network{ID: "3", Name: "spare"}},
		{Network: network.Network{ID: "4", Name: "ingress", Scope: "swarm"}},
	}
	containers := []container.Summary{{ID: "c", State: container.StateExited, NetworkSettings: &container.NetworkSettingsSummary{Networks: map[string]*network.EndpointSettings{"app_default": {}}}}}
	if got := ids(UnusedNetworks(networks, containers)); !slices.Equal(got, []string{"3"}) {
		t.Fatal(got)
	}
}

func TestUnusedBuildCacheSkipsInUseAndPutsLargestFirst(t *testing.T) {
	records := []build.CacheRecord{{ID: "small", Size: 1}, {ID: "busy", Size: 9, InUse: true}, {ID: "big", Size: 5}}
	if got := ids(UnusedBuildCache(records)); !slices.Equal(got, []string{"big", "small"}) {
		t.Fatal(got)
	}
}

type pruneEngine struct {
	engine
	removed []string
	fail    string
}

func (e *pruneEngine) ImageRemove(_ context.Context, ref string, opts client.ImageRemoveOptions) (client.ImageRemoveResult, error) {
	if opts.Force || !opts.PruneChildren {
		return client.ImageRemoveResult{}, errors.New("unexpected options")
	}
	if ref == e.fail {
		return client.ImageRemoveResult{}, errors.New("conflict")
	}
	e.removed = append(e.removed, ref)
	return client.ImageRemoveResult{}, nil
}

func (e *pruneEngine) ContainerRemove(_ context.Context, id string, opts client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	if opts.Force || opts.RemoveVolumes {
		return client.ContainerRemoveResult{}, errors.New("prune must not force or remove volumes")
	}
	e.removed = append(e.removed, id)
	return client.ContainerRemoveResult{}, nil
}

func TestPruneOneRemovesEachTagThenStopsAtAFailure(t *testing.T) {
	e := &pruneEngine{fail: "b:1"}
	c := &Client{api: e}
	if err := c.PruneOne(t.Context(), PruneImages, PruneTarget{ID: "sha256:x", Tags: []string{"a:1", "b:1", "c:1"}}); err == nil {
		t.Fatal("failure was not reported")
	}
	if err := c.PruneOne(t.Context(), PruneImages, PruneTarget{ID: "sha256:y"}); err != nil {
		t.Fatal(err)
	}
	if err := c.PruneOne(t.Context(), PruneContainers, PruneTarget{ID: "c1"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(e.removed, []string{"a:1", "sha256:y", "c1"}) {
		t.Fatal(e.removed)
	}
}

func TestShortID(t *testing.T) {
	if shortID("sha256:0123456789abcdef") != "0123456789ab" || shortID("zq6pkl3hmt0x5d9w") != "zq6pkl3hmt0x" {
		t.Fatal(shortID("sha256:0123456789abcdef"), shortID("zq6pkl3hmt0x5d9w"))
	}
}
