package docker

import (
	"context"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

type VolumeDestination struct {
	Path     string
	ReadOnly bool
}

type VolumeUse struct {
	ID, Name string
	State    container.ContainerState
	Mounts   []VolumeDestination
}

func (c *Client) Volumes(ctx context.Context) ([]volume.Volume, []string, error) {
	res, err := c.api.VolumeList(ctx, client.VolumeListOptions{})
	return res.Items, res.Warnings, err
}
func (c *Client) Volume(ctx context.Context, name string) (volume.Volume, error) {
	res, err := c.api.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	return res.Volume, err
}

// VolumeUsers indexes named and anonymous volume mounts in one pass. A container
// is counted once per volume even when it mounts that volume at several paths.
func VolumeUsers(items []container.Summary) map[string][]VolumeUse {
	out := make(map[string][]VolumeUse)
	for _, c := range items {
		mounts := map[string][]VolumeDestination{}
		for _, m := range c.Mounts {
			if m.Type != mount.TypeVolume || m.Name == "" {
				continue
			}
			mounts[m.Name] = append(mounts[m.Name], VolumeDestination{Path: m.Destination, ReadOnly: !m.RW})
		}
		for name, paths := range mounts {
			out[name] = append(out[name], VolumeUse{ID: c.ID, Name: ContainerName(c.Names), State: c.State, Mounts: paths})
		}
	}
	return out
}
