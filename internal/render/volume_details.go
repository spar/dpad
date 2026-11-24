package render

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/volume"

	"dpad/internal/docker"
)

func VolumeUsage(users []docker.VolumeUse, known bool) string {
	if !known {
		return "unknown"
	}
	if len(users) == 0 {
		return "unreferenced"
	}
	running := 0
	for _, u := range users {
		if u.State == container.StateRunning {
			running++
		}
	}
	return fmt.Sprintf("%d running / %d total", running, len(users))
}

func VolumeDetails(v volume.Volume, users []docker.VolumeUse, known bool) string {
	var b detailBuilder
	b.field("Name", v.Name)
	b.field("Driver", v.Driver)
	b.field("Scope", v.Scope)
	b.field("Created", v.CreatedAt)
	b.field("Mountpoint (daemon host)", v.Mountpoint)
	b.labels(v.Labels)
	fmt.Fprintln(&b, "\n"+Header.Render("Container references")+" · "+VolumeUsage(users, known))
	if !known {
		fmt.Fprintln(&b, "container usage unavailable; ctrl+r to refresh")
	} else if len(users) == 0 {
		fmt.Fprintln(&b, "none reported by this daemon")
	} else {
		sorted := slices.Clone(users)
		slices.SortFunc(sorted, func(a, b docker.VolumeUse) int { return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID)) })
		for _, u := range sorted {
			fmt.Fprintf(&b, "%s  %s  %s\n", ShortID(u.ID), u.Name, StateStyle(string(u.State), 0).Render(string(u.State)))
			paths := slices.Clone(u.Mounts)
			slices.SortFunc(paths, func(a, b docker.VolumeDestination) int { return cmp.Compare(a.Path, b.Path) })
			for _, p := range paths {
				mode := "rw"
				if p.ReadOnly {
					mode = "ro"
				}
				fmt.Fprintf(&b, "  %s (%s)\n", p.Path, mode)
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
