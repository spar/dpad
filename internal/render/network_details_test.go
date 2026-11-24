package render

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/network"

	"dpad/internal/docker"
)

func TestNetworkMetadataAndMissingAddresses(t *testing.T) {
	info := network.Inspect{Network: network.Network{ID: "net-id", Name: "app", Driver: "bridge", Scope: "local", Internal: true, Attachable: true, EnableIPv4: true, EnableIPv6: true, IPAM: network.IPAM{Driver: "default", Config: []network.IPAMConfig{
		{Subnet: netip.MustParsePrefix("172.30.0.0/24"), Gateway: netip.MustParseAddr("172.30.0.1")},
		{Subnet: netip.MustParsePrefix("fd00:1234::/64"), Gateway: netip.MustParseAddr("fd00:1234::1")},
	}}, Labels: map[string]string{"z": "last", "a": "first"}}, Containers: map[string]network.EndpointResource{"web": {Name: "web"}}}
	out := ansi.Strip(NetworkDetails(info))
	for _, want := range []string{"app", "net-id", "bridge", "Scope:  local", "Internal:  true", "Attachable:  true", "IPv6 enabled:  true", "172.30.0.0/24", "172.30.0.1", "fd00:1234::/64", "fd00:1234::1", "Endpoints (inspect snapshot):  1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "invalid Prefix") || strings.Index(out, "a=first") > strings.Index(out, "z=last") {
		t.Fatal(out)
	}
	if NetworkPrefix(netip.Prefix{}) != "—" {
		t.Fatal("missing address rendered as valid")
	}
	if NetworkUsage(nil, false) != "unknown" || NetworkUsage(nil, true) != "0 / 0" {
		t.Fatal("unknown confused with zero")
	}
	if got := NetworkUsage([]docker.NetworkUse{{Attached: true}, {Attached: false}}, true); got != "1 / 2" {
		t.Fatal(got)
	}
}
