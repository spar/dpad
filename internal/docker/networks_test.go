package docker

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

type networkEngine struct {
	engine
	err       error
	inspected string
}

func (f *networkEngine) NetworkList(context.Context, client.NetworkListOptions) (client.NetworkListResult, error) {
	return client.NetworkListResult{Items: []network.Summary{{Network: network.Network{ID: "net-id", Name: "app"}}}}, f.err
}
func (f *networkEngine) NetworkInspect(_ context.Context, id string, _ client.NetworkInspectOptions) (client.NetworkInspectResult, error) {
	f.inspected = id
	return client.NetworkInspectResult{Network: network.Inspect{Network: network.Network{ID: id, Name: "app"}}}, f.err
}
func TestNetworkAPIUsesIdentityAndPreservesErrors(t *testing.T) {
	f := &networkEngine{}
	c := &Client{api: f}
	items, err := c.Networks(t.Context())
	if err != nil || len(items) != 1 || items[0].ID != "net-id" {
		t.Fatal(items, err)
	}
	info, err := c.Network(t.Context(), "net-id")
	if err != nil || info.ID != "net-id" || f.inspected != "net-id" {
		t.Fatal(info, err)
	}
	f.err = errors.New("daemon unavailable")
	if _, err = c.Networks(t.Context()); !errors.Is(err, f.err) {
		t.Fatal(err)
	}
	if _, err = c.Network(t.Context(), "gone"); !errors.Is(err, f.err) {
		t.Fatal(err)
	}
}
func TestNetworkUsersAttachedConfiguredAndIdentity(t *testing.T) {
	nets := []network.Summary{{Network: network.Network{ID: "new-id", Name: "app"}}, {Network: network.Network{ID: "empty-id", Name: "empty"}}}
	items := []container.Summary{
		{ID: "running", Names: []string{"/web"}, NetworkSettings: &container.NetworkSettingsSummary{Networks: map[string]*network.EndpointSettings{
			"app": {NetworkID: "new-id", EndpointID: "live"}, "alias": {NetworkID: "new-id"},
		}}},
		{ID: "stopped", Names: []string{"/old-web"}, NetworkSettings: &container.NetworkSettingsSummary{Networks: map[string]*network.EndpointSettings{"app": {NetworkID: "new-id"}}}},
		{ID: "created", NetworkSettings: &container.NetworkSettingsSummary{Networks: map[string]*network.EndpointSettings{"app": nil}}},
		{ID: "old-network", NetworkSettings: &container.NetworkSettingsSummary{Networks: map[string]*network.EndpointSettings{"app": {NetworkID: "deleted-id", EndpointID: "old"}}}},
		{ID: "unknown", NetworkSettings: &container.NetworkSettingsSummary{Networks: map[string]*network.EndpointSettings{"missing": nil}}},
		{ID: "no-settings"},
	}
	got := NetworkUsers(items, nets)
	if len(got["new-id"]) != 3 || len(got["deleted-id"]) != 1 || len(got["empty-id"]) != 0 || len(got) != 2 {
		t.Fatalf("wrong membership: %+v", got)
	}
	if !got["new-id"][0].Attached || got["new-id"][0].Name != "web" || got["new-id"][1].Attached || got["new-id"][2].Attached {
		t.Fatal(got)
	}
	if items[0].Names[0] != "/web" {
		t.Fatal("input was mutated")
	}
}
func BenchmarkNetworkUsers(b *testing.B) {
	nets := make([]network.Summary, 1000)
	items := make([]container.Summary, 1000)
	for i := range nets {
		id := fmt.Sprint(i)
		nets[i] = network.Summary{Network: network.Network{ID: id, Name: "net-" + id}}
		items[i] = container.Summary{ID: "web-" + id, NetworkSettings: &container.NetworkSettingsSummary{Networks: map[string]*network.EndpointSettings{"net-" + id: {NetworkID: id, EndpointID: "ep-" + id}}}}
	}
	b.ReportAllocs()
	for b.Loop() {
		NetworkUsers(items, nets)
	}
}
