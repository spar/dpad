package docker

import (
	"context"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

type NetworkUse struct {
	ID, Name string
	Attached bool
}

func (c *Client) Networks(ctx context.Context) ([]network.Summary, error) {
	res, err := c.api.NetworkList(ctx, client.NetworkListOptions{})
	return res.Items, err
}
func (c *Client) Network(ctx context.Context, id string) (network.Inspect, error) {
	res, err := c.api.NetworkInspect(ctx, id, client.NetworkInspectOptions{})
	return res.Network, err
}

// NetworkUsers distinguishes live endpoints from configured references (including
// stopped containers). Names resolve references that have no network ID yet.
func NetworkUsers(items []container.Summary, networks []network.Summary) map[string][]NetworkUse {
	ids := make(map[string]string, len(networks))
	for _, n := range networks {
		ids[n.Name] = n.ID
	}
	out := make(map[string][]NetworkUse)
	for _, c := range items {
		if c.NetworkSettings == nil {
			continue
		}
		refs := map[string]NetworkUse{}
		for name, ep := range c.NetworkSettings.Networks {
			id := ""
			attached := false
			if ep != nil {
				id = ep.NetworkID
				attached = ep.EndpointID != ""
			}
			if id == "" {
				id = ids[name]
			}
			if id == "" {
				continue
			}
			previous := refs[id]
			refs[id] = NetworkUse{ID: c.ID, Name: ContainerName(c.Names), Attached: previous.Attached || attached}
		}
		for id, ref := range refs {
			out[id] = append(out[id], ref)
		}
	}
	return out
}
