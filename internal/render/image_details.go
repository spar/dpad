package render

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/moby/moby/api/types/container"

	"dpad/internal/docker"
)

func ImageUsage(items []container.Summary) string {
	if len(items) == 0 {
		return "unused"
	}
	running := 0
	for _, c := range items {
		if c.State == container.StateRunning {
			running++
		}
	}
	return fmt.Sprintf("%d running / %d total", running, len(items))
}

func ImageDetails(info docker.ImageInfo) string {
	i := info.Image
	var b detailBuilder
	b.field("ID", i.ID)
	b.field("Created", i.Created)
	b.field("OS", i.Os)
	b.field("Architecture", i.Architecture)
	if i.Variant != "" {
		b.field("Variant", i.Variant)
	}
	b.field("Size", Size(i.Size))
	for _, group := range []struct {
		title  string
		values []string
	}{{"Tags", i.RepoTags}, {"Digests", i.RepoDigests}} {
		b.section(group.title)
		values := slices.Clone(group.values)
		slices.Sort(values)
		if len(values) == 0 {
			values = []string{"none"}
		}
		fmt.Fprintln(&b, strings.Join(values, "\n"))
	}
	b.section("Configuration")
	if i.Config != nil {
		b.field("Entrypoint", commandArgs(i.Config.Entrypoint))
		b.field("Command", commandArgs(i.Config.Cmd))
		b.field("Working directory", i.Config.WorkingDir)
		b.field("User", i.Config.User)
		b.labels(i.Config.Labels)
	} else {
		b.labels(nil)
	}
	fmt.Fprintln(&b, "\n"+Header.Render("Containers using this image")+" · "+ImageUsage(info.Containers))
	items := slices.Clone(info.Containers)
	slices.SortFunc(items, func(a, b container.Summary) int {
		return cmp.Compare(docker.ContainerName(a.Names), docker.ContainerName(b.Names))
	})
	if len(items) == 0 {
		fmt.Fprintln(&b, "none")
	}
	for _, c := range items {
		fmt.Fprintf(&b, "%s  %s  %s\n", ShortID(c.ID), docker.ContainerName(c.Names), StateStyle(string(c.State), ExitCode(c.Status)).Render(string(c.State)))
	}
	return strings.TrimRight(b.String(), "\n")
}
