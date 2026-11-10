// Package sortx defines the sort keys and comparators shared by the CLI and TUI.
package sortx

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"

	"dockernext/internal/docker"
)

var (
	StatsKeys     = []string{"cpu", "mem", "net", "io", "pids", "name"}
	ContainerKeys = []string{"name", "created", "status"}
	ImageKeys     = []string{"size", "created", "name"}
)

// Validate returns an error if key is not one of keys.
func Validate(key string, keys []string) error {
	if slices.Contains(keys, key) {
		return nil
	}
	return fmt.Errorf("invalid sort key %q (valid: %s)", key, strings.Join(keys, ", "))
}

// Next cycles to the key after current.
func Next(current string, keys []string) string {
	i := slices.Index(keys, current)
	return keys[(i+1)%len(keys)]
}

// Each key has a natural direction: numbers descending (biggest first),
// names ascending. reverse flips it. Ties fall back to name for stable output.

func Stats(items []docker.Stats, key string, reverse bool) {
	slices.SortStableFunc(items, func(a, b docker.Stats) int {
		var c int
		switch key {
		case "cpu":
			c = cmp.Compare(b.CPUPercent, a.CPUPercent)
		case "mem":
			c = cmp.Compare(b.MemUsage, a.MemUsage)
		case "net":
			c = cmp.Compare(b.NetRx+b.NetTx, a.NetRx+a.NetTx)
		case "io":
			c = cmp.Compare(b.BlockRead+b.BlockWrite, a.BlockRead+a.BlockWrite)
		case "pids":
			c = cmp.Compare(b.PIDs, a.PIDs)
		}
		return finish(c, a.Name, b.Name, reverse)
	})
}

// containerStateRank orders states so active containers come first.
var containerStateRank = map[container.ContainerState]int{
	"running": 0, "restarting": 1, "paused": 2, "created": 3, "exited": 4, "dead": 5,
}

func Containers(items []container.Summary, key string, reverse bool) {
	slices.SortStableFunc(items, func(a, b container.Summary) int {
		var c int
		switch key {
		case "created":
			c = cmp.Compare(b.Created, a.Created)
		case "status":
			c = cmp.Compare(rank(a.State), rank(b.State))
		}
		return finish(c, docker.ContainerName(a.Names), docker.ContainerName(b.Names), reverse)
	})
}

func rank(s container.ContainerState) int {
	if r, ok := containerStateRank[s]; ok {
		return r
	}
	return len(containerStateRank)
}

func Images(items []image.Summary, key string, reverse bool) {
	slices.SortStableFunc(items, func(a, b image.Summary) int {
		var c int
		switch key {
		case "size":
			c = cmp.Compare(b.Size, a.Size)
		case "created":
			c = cmp.Compare(b.Created, a.Created)
		}
		return finish(c, imageName(a), imageName(b), reverse)
	})
}

// imageName sorts untagged images after tagged ones.
func imageName(img image.Summary) string {
	if len(img.RepoTags) == 0 {
		return "￿"
	}
	return img.RepoTags[0]
}

func finish(c int, nameA, nameB string, reverse bool) int {
	if c == 0 {
		c = cmp.Compare(nameA, nameB)
	}
	if reverse {
		return -c
	}
	return c
}
