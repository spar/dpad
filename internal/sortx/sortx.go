// Package sortx defines the sort keys and comparators shared by the CLI and TUI.
package sortx

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"

	"dpad/internal/docker"
)

// Key names a sort order, as accepted by --sort.
type Key string

const (
	CPU     Key = "cpu"
	Mem     Key = "mem"
	Net     Key = "net"
	IO      Key = "io"
	PIDs    Key = "pids"
	Name    Key = "name"
	Created Key = "created"
	Status  Key = "status"
	Size    Key = "size"
)

// The keys each list supports, in the order the TUI cycles through them.
var (
	StatsKeys     = []Key{CPU, Mem, Net, IO, PIDs, Name}
	ContainerKeys = []Key{Name, Created, Status}
	ImageKeys     = []Key{Size, Created, Name}
)

// Parse returns s as a Key, or an error if it is not one of keys.
func Parse(s string, keys []Key) (Key, error) {
	if k := Key(s); slices.Contains(keys, k) {
		return k, nil
	}
	return "", fmt.Errorf("invalid sort key %q (valid: %s)", s, Join(keys, ", "))
}

// Join lists keys separated by sep, e.g. for help text.
func Join(keys []Key, sep string) string {
	s := make([]string, len(keys))
	for i, k := range keys {
		s[i] = string(k)
	}
	return strings.Join(s, sep)
}

// Next cycles to the key after current.
func Next(current Key, keys []Key) Key {
	i := slices.Index(keys, current)
	return keys[(i+1)%len(keys)]
}

// Descending reports whether key's natural order puts the biggest or newest
// first; the rest sort ascending (A→Z, running before exited).
func Descending(key Key) bool {
	switch key {
	case Name, Status:
		return false
	}
	return true
}

// Each key has a natural direction: numbers descending (biggest first),
// names ascending. reverse flips it. Ties fall back to name for stable output.

func Stats(items []docker.Stats, key Key, reverse bool) {
	slices.SortStableFunc(items, func(a, b docker.Stats) int {
		var c int
		switch key {
		case CPU:
			c = cmp.Compare(b.CPUPercent, a.CPUPercent)
		case Mem:
			c = cmp.Compare(b.MemUsage, a.MemUsage)
		case Net:
			c = cmp.Compare(b.NetRx+b.NetTx, a.NetRx+a.NetTx)
		case IO:
			c = cmp.Compare(b.BlockRead+b.BlockWrite, a.BlockRead+a.BlockWrite)
		case PIDs:
			c = cmp.Compare(b.PIDs, a.PIDs)
		}
		return order(cmp.Or(c, cmp.Compare(a.Name, b.Name)), reverse)
	})
}

func StatsByRates(items []docker.Stats, key Key, reverse bool, rates map[string]docker.Rates) {
	if key != Net && key != IO {
		Stats(items, key, reverse)
		return
	}
	slices.SortStableFunc(items, func(a, b docker.Stats) int {
		ra, rb := rates[a.ID], rates[b.ID]
		va, vb := ra.NetValid, rb.NetValid
		aTotal, bTotal := ra.NetRx+ra.NetTx, rb.NetRx+rb.NetTx
		if key == IO {
			va, vb = ra.BlockValid, rb.BlockValid
			aTotal, bTotal = ra.BlockRead+ra.BlockWrite, rb.BlockRead+rb.BlockWrite
		}
		if va != vb {
			if va {
				return -1
			}
			return 1
		}
		if !va {
			return order(cmp.Compare(a.Name, b.Name), reverse)
		}
		return order(cmp.Or(cmp.Compare(bTotal, aTotal), cmp.Compare(a.Name, b.Name)), reverse)
	})
}

// containerStateRank orders states so active containers come first.
var containerStateRank = map[container.ContainerState]int{
	"running": 0, "restarting": 1, "paused": 2, "created": 3, "exited": 4, "dead": 5,
}

func Containers(items []container.Summary, key Key, reverse bool) {
	slices.SortStableFunc(items, func(a, b container.Summary) int {
		var c int
		switch key {
		case Created:
			c = cmp.Compare(b.Created, a.Created)
		case Status:
			c = cmp.Compare(rank(a.State), rank(b.State))
		}
		return order(cmp.Or(c, cmp.Compare(docker.ContainerName(a.Names), docker.ContainerName(b.Names))), reverse)
	})
}

func rank(s container.ContainerState) int {
	if r, ok := containerStateRank[s]; ok {
		return r
	}
	return len(containerStateRank)
}

func Images(items []image.Summary, key Key, reverse bool) {
	slices.SortStableFunc(items, func(a, b image.Summary) int {
		var c int
		switch key {
		case Size:
			c = cmp.Compare(b.Size, a.Size)
		case Created:
			c = cmp.Compare(b.Created, a.Created)
		}
		return order(cmp.Or(c, compareImageNames(a, b)), reverse)
	})
}

// compareImageNames orders images by first tag, untagged (dangling) last.
func compareImageNames(a, b image.Summary) int {
	switch ta, tb := len(a.RepoTags) > 0, len(b.RepoTags) > 0; {
	case ta && tb:
		return cmp.Compare(a.RepoTags[0], b.RepoTags[0])
	case ta:
		return -1
	case tb:
		return 1
	}
	return 0
}

// order applies reverse to a comparison result.
func order(c int, reverse bool) int {
	if reverse {
		return -c
	}
	return c
}
