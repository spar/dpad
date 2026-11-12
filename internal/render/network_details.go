package render

import (
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/api/types/network"

	"dockernext/internal/docker"
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
	var b strings.Builder
	field := func(label, value string) {
		if value == "" {
			value = "—"
		}
		fmt.Fprintf(&b, "%s  %s\n", Bold.Render(label+":"), value)
	}
	field("Name", n.Name)
	field("ID", n.ID)
	field("Driver", n.Driver)
	field("Scope", n.Scope)
	created := ""
	if !n.Created.IsZero() {
		created = n.Created.Format(time.RFC3339Nano)
	}
	field("Created", created)
	field("Internal", strconv.FormatBool(n.Internal))
	field("Attachable", strconv.FormatBool(n.Attachable))
	field("Ingress", strconv.FormatBool(n.Ingress))
	field("IPv4 enabled", strconv.FormatBool(n.EnableIPv4))
	field("IPv6 enabled", strconv.FormatBool(n.EnableIPv6))
	field("Endpoints (inspect snapshot)", strconv.Itoa(len(info.Containers)))
	fmt.Fprintln(&b, "\n"+Header.Render("IPAM"))
	field("Driver", n.IPAM.Driver)
	if len(n.IPAM.Config) == 0 {
		fmt.Fprintln(&b, "no subnet configuration reported")
	}
	for _, c := range n.IPAM.Config {
		field("Subnet", NetworkPrefix(c.Subnet))
		gateway := ""
		if c.Gateway.IsValid() {
			gateway = c.Gateway.String()
		}
		field("Gateway", gateway)
		field("IP range", NetworkPrefix(c.IPRange))
		keys := make([]string, 0, len(c.AuxAddress))
		for k := range c.AuxAddress {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			field("Auxiliary "+k, c.AuxAddress[k].String())
		}
	}
	fmt.Fprintln(&b, "\n"+Header.Render("Labels"))
	labels := make([]string, 0, len(n.Labels))
	for k, value := range n.Labels {
		labels = append(labels, k+"="+value)
	}
	slices.Sort(labels)
	if len(labels) == 0 {
		labels = append(labels, "none")
	}
	fmt.Fprintln(&b, strings.Join(labels, "\n"))
	fmt.Fprintln(&b, "\n"+Dim.Render("tab: endpoints · enter on an endpoint: container details"))
	return strings.TrimRight(b.String(), "\n")
}
