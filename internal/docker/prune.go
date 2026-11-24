package docker

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/moby/moby/api/types/build"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

type PruneKind string

const (
	PruneContainers PruneKind = "containers"
	PruneImages     PruneKind = "images"
	PruneVolumes    PruneKind = "volumes"
	PruneNetworks   PruneKind = "networks"
	PruneBuildCache PruneKind = "build cache"
)

var PruneKinds = []PruneKind{PruneContainers, PruneImages, PruneVolumes, PruneNetworks, PruneBuildCache}

// PruneTarget is one resource a prune preview lists. Prune removes exactly the
// previewed targets, except build cache, which the daemon prunes as a whole.
type PruneTarget struct {
	ID, Name, Detail string
	Size             int64 // bytes; -1 when unknown
	Tags             []string
}

const anonymousVolumeLabel = "com.docker.volume.anonymous"

// StoppedContainers are the containers docker container prune removes.
func StoppedContainers(items []container.Summary) []PruneTarget {
	out := []PruneTarget{}
	for _, c := range items {
		switch c.State {
		case container.StateExited, container.StateCreated, container.StateDead:
			out = append(out, PruneTarget{ID: c.ID, Name: ContainerName(c.Names), Detail: c.Image + " · " + c.Status, Size: -1})
		}
	}
	slices.SortFunc(out, func(a, b PruneTarget) int { return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID)) })
	return out
}

// UnusedImages are images no container (running or stopped) uses. Without all,
// only untagged (dangling) images are included, as with docker image prune.
// Newest images come first so children are removed before their parents.
func UnusedImages(images []image.Summary, containers []container.Summary, all bool) []PruneTarget {
	used := map[string]bool{}
	for _, c := range containers {
		used[c.ImageID] = true
	}
	images = slices.Clone(images)
	slices.SortFunc(images, func(a, b image.Summary) int {
		return cmp.Or(cmp.Compare(b.Created, a.Created), cmp.Compare(a.ID, b.ID))
	})
	out := []PruneTarget{}
	for _, img := range images {
		if used[img.ID] {
			continue
		}
		tags := slices.DeleteFunc(slices.Clone(img.RepoTags), func(t string) bool { return t == "<none>:<none>" })
		if len(tags) > 0 && !all {
			continue
		}
		name := strings.Join(tags, ", ")
		if name == "" {
			name = "<none>"
		}
		out = append(out, PruneTarget{ID: img.ID, Name: name, Detail: shortID(img.ID), Tags: tags, Size: img.Size})
	}
	return out
}

// UnusedVolumes are volumes no container mounts. Without all, only anonymous
// volumes are included, as with docker volume prune.
func UnusedVolumes(volumes []volume.Volume, containers []container.Summary, all bool) []PruneTarget {
	users := VolumeUsers(containers)
	out := []PruneTarget{}
	for _, v := range volumes {
		_, anonymous := v.Labels[anonymousVolumeLabel]
		if len(users[v.Name]) > 0 || (!anonymous && !all) {
			continue
		}
		kind := "named"
		if anonymous {
			kind = "anonymous"
		}
		out = append(out, PruneTarget{ID: v.Name, Name: v.Name, Detail: v.Driver + " · " + kind, Size: -1})
	}
	slices.SortFunc(out, func(a, b PruneTarget) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

// UnusedNetworks are user-defined local networks no container references.
func UnusedNetworks(networks []network.Summary, containers []container.Summary) []PruneTarget {
	users := NetworkUsers(containers, networks)
	out := []PruneTarget{}
	for _, n := range networks {
		switch {
		case n.Name == "bridge" || n.Name == "host" || n.Name == "none", n.Scope == "swarm", len(users[n.ID]) > 0:
			continue
		}
		out = append(out, PruneTarget{ID: n.ID, Name: n.Name, Detail: n.Driver, Size: -1})
	}
	slices.SortFunc(out, func(a, b PruneTarget) int { return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID)) })
	return out
}

// UnusedBuildCache lists the records docker builder prune --all removes.
func UnusedBuildCache(records []build.CacheRecord) []PruneTarget {
	out := []PruneTarget{}
	for _, r := range records {
		if r.InUse {
			continue
		}
		detail := r.Type
		if r.Description != "" {
			detail += " · " + r.Description
		}
		out = append(out, PruneTarget{ID: r.ID, Name: shortID(r.ID), Detail: detail, Size: r.Size})
	}
	slices.SortFunc(out, func(a, b PruneTarget) int { return cmp.Or(cmp.Compare(b.Size, a.Size), cmp.Compare(a.ID, b.ID)) })
	return out
}

func shortID(id string) string {
	if _, hex, ok := strings.Cut(id, ":"); ok {
		id = hex
	}
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// PruneTargets reads what a prune of kind would remove right now.
func (c *Client) PruneTargets(ctx context.Context, kind PruneKind, all bool) ([]PruneTarget, error) {
	if kind == PruneBuildCache {
		res, err := c.api.DiskUsage(ctx, client.DiskUsageOptions{BuildCache: true, Verbose: true})
		return UnusedBuildCache(res.BuildCache.Items), err
	}
	containers, err := c.Containers(ctx, true)
	if err != nil {
		return nil, err
	}
	switch kind {
	case PruneImages:
		images, err := c.Images(ctx)
		return UnusedImages(images, containers, all), err
	case PruneVolumes:
		volumes, _, err := c.Volumes(ctx)
		return UnusedVolumes(volumes, containers, all), err
	case PruneNetworks:
		networks, err := c.Networks(ctx)
		return UnusedNetworks(networks, containers), err
	}
	return StoppedContainers(containers), nil
}

// PruneOne removes one previewed target without forcing, so anything that
// became used since the preview is left in place with an error.
func (c *Client) PruneOne(ctx context.Context, kind PruneKind, t PruneTarget) error {
	var err error
	switch kind {
	case PruneContainers:
		_, err = c.api.ContainerRemove(ctx, t.ID, client.ContainerRemoveOptions{})
	case PruneImages:
		refs := t.Tags
		if len(refs) == 0 {
			refs = []string{t.ID}
		}
		for _, ref := range refs {
			if _, err = c.api.ImageRemove(ctx, ref, client.ImageRemoveOptions{PruneChildren: true}); err != nil {
				break
			}
		}
	case PruneVolumes:
		_, err = c.api.VolumeRemove(ctx, t.ID, client.VolumeRemoveOptions{})
	case PruneNetworks:
		_, err = c.api.NetworkRemove(ctx, t.ID, client.NetworkRemoveOptions{})
	}
	return err
}

// PruneBuildCache removes all unused build cache and reports the removed record IDs.
func (c *Client) PruneBuildCache(ctx context.Context) ([]string, uint64, error) {
	res, err := c.api.BuildCachePrune(ctx, client.BuildCachePruneOptions{All: true})
	return res.Report.CachesDeleted, res.Report.SpaceReclaimed, err
}
