package render

import (
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/api/types/network"

	"dpad/internal/docker"
)

func NetworkUsage(users []docker.NetworkUse, known bool) string {
	if !known {
		return "unknown"
	}
	attached := 0
	for _, u := range users {
		if u.Attached {
			attached++
		}
	}
	return fmt.Sprintf("%d / %d", attached, len(users))
}
func NetworkPrefix(p netip.Prefix) string {
	if !p.IsValid() {
		return "—"
	}
	return p.String()
}

func NetworkDetails(info network.Inspect) string {
	n := info.Network
	var b detailBuilder
	b.field("Name", n.Name)
	b.field("ID", n.ID)
	b.field("Driver", n.Driver)
	b.field("Scope", n.Scope)
	created := ""
	if !n.Created.IsZero() {
		created = n.Created.Format(time.RFC3339Nano)
	}
	b.field("Created", created)
	b.field("Internal", strconv.FormatBool(n.Internal))
	b.field("Attachable", strconv.FormatBool(n.Attachable))
	b.field("Ingress", strconv.FormatBool(n.Ingress))
	b.field("IPv4 enabled", strconv.FormatBool(n.EnableIPv4))
	b.field("IPv6 enabled", strconv.FormatBool(n.EnableIPv6))
	b.field("Endpoints (inspect snapshot)", strconv.Itoa(len(info.Containers)))
	b.section("IPAM")
	b.field("Driver", n.IPAM.Driver)
	if len(n.IPAM.Config) == 0 {
		fmt.Fprintln(&b, "no subnet configuration reported")
	}
	for _, c := range n.IPAM.Config {
		b.field("Subnet", NetworkPrefix(c.Subnet))
		gateway := ""
		if c.Gateway.IsValid() {
			gateway = c.Gateway.String()
		}
		b.field("Gateway", gateway)
		b.field("IP range", NetworkPrefix(c.IPRange))
		keys := make([]string, 0, len(c.AuxAddress))
		for k := range c.AuxAddress {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			b.field("Auxiliary "+k, c.AuxAddress[k].String())
		}
	}
	b.labels(n.Labels)
	fmt.Fprintln(&b, "\n"+Dim.Render("tab: endpoints · enter on an endpoint: container details"))
	return strings.TrimRight(b.String(), "\n")
}
