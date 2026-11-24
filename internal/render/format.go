package render

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/moby/moby/api/types/container"
)

// Size formats bytes in decimal units, as docker does for images (e.g. "1.2GB").
func Size(b int64) string { return units.HumanSizeWithPrecision(float64(b), 3) }

func byteRatePair(a, b float64) string {
	return units.HumanSizeWithPrecision(a, 3) + "/s / " + units.HumanSizeWithPrecision(b, 3) + "/s"
}

// BinSize formats bytes in binary units, as docker does for memory (e.g. "512MiB").
func BinSize(b uint64) string { return units.BytesSize(float64(b)) }

// Ago formats a unix timestamp as "3 hours ago", or "N/A" when unset.
func Ago(unix int64) string {
	if unix <= 0 {
		return "N/A"
	}
	return units.HumanDuration(time.Since(time.Unix(unix, 0))) + " ago"
}

var exitRe = regexp.MustCompile(`Exited \((-?\d+)\)`)

// ExitCode extracts the exit code from a status string like "Exited (1) 2 hours ago".
func ExitCode(status string) int {
	if m := exitRe.FindStringSubmatch(status); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// Ports compacts port mappings to "8080→80/tcp", dropping the duplicate
// entries docker reports for IPv4 and IPv6 bindings. A binding to a specific
// address keeps it ("127.0.0.1:8080→80/tcp"), since that limits who can connect.
func Ports(ports []container.PortSummary) string {
	sorted := append([]container.PortSummary(nil), ports...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].PrivatePort != sorted[j].PrivatePort {
			return sorted[i].PrivatePort < sorted[j].PrivatePort
		}
		return sorted[i].PublicPort < sorted[j].PublicPort
	})
	seen := map[string]bool{}
	var out []string
	for _, p := range sorted {
		s := fmt.Sprintf("%d/%s", p.PrivatePort, p.Type)
		if p.PublicPort != 0 {
			host := strconv.Itoa(int(p.PublicPort))
			if p.IP.IsValid() && !p.IP.IsUnspecified() {
				host = net.JoinHostPort(p.IP.String(), host)
			}
			s = host + "→" + s
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return strings.Join(out, ", ")
}

// ShortID returns the 12-character form of a container or image ID.
func ShortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
