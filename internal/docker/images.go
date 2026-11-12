package docker

import (
	"context"
	"fmt"
	"slices"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
)

type ImageInfo struct {
	Image      image.InspectResponse
	Containers []container.Summary
}

// ImageRemoval captures the identity and effect shown in the confirmation.
type ImageRemoval struct {
	ID, Reference string
	UntagOnly     bool
}

func ContainersUsingImage(items []container.Summary, id string) []container.Summary {
	var using []container.Summary
	for _, c := range items {
		if c.ImageID == id {
			using = append(using, c)
		}
	}
	return using
}

func (c *Client) ImageDetails(ctx context.Context, id string) (ImageInfo, error) {
	res, err := c.api.ImageInspect(ctx, id)
	if err != nil {
		return ImageInfo{}, err
	}
	items, err := c.Containers(ctx, true)
	if err != nil {
		return ImageInfo{}, err
	}
	return ImageInfo{Image: res.InspectResponse, Containers: ContainersUsingImage(items, res.ID)}, nil
}

func (c *Client) RemoveImage(ctx context.Context, removal ImageRemoval) error {
	res, err := c.api.ImageInspect(ctx, removal.Reference)
	if err != nil {
		return err
	}
	info := res.InspectResponse
	if info.ID != removal.ID || (len(info.RepoTags) > 1) != removal.UntagOnly ||
		(removal.Reference == removal.ID && len(info.RepoTags) != 0) ||
		(removal.Reference != removal.ID && !slices.Contains(info.RepoTags, removal.Reference)) {
		return fmt.Errorf("image or tags changed; refresh and confirm removal again")
	}
	if !removal.UntagOnly {
		items, err := c.Containers(ctx, true)
		if err != nil {
			return err
		}
		if len(ContainersUsingImage(items, removal.ID)) > 0 {
			return fmt.Errorf("image is used by containers (including stopped containers); remove those containers first")
		}
	}
	_, err = c.api.ImageRemove(ctx, removal.Reference, client.ImageRemoveOptions{Force: false, PruneChildren: false})
	return err
}
