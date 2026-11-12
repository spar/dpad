package render

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/moby/moby/api/types/container"

	"dockernext/internal/docker"
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
	var b strings.Builder
	field := func(label, value string) {
		if value == "" {
			value = "—"
		}
		fmt.Fprintf(&b, "%s  %s\n", Bold.Render(label+":"), value)
	}
	field("ID", i.ID)
	field("Created", i.Created)
	field("OS", i.Os)
	field("Architecture", i.Architecture)
	if i.Variant != "" {
		field("Variant", i.Variant)
	}
	field("Size", Size(i.Size))
	for _, group := range []struct {
		title  string
		values []string
	}{{"Tags", i.RepoTags}, {"Digests", i.RepoDigests}} {
		fmt.Fprintln(&b, "\n"+Header.Render(group.title))
		values := slices.Clone(group.values)
		slices.Sort(values)
		if len(values) == 0 {
			values = []string{"none"}
		}
		fmt.Fprintln(&b, strings.Join(values, "\n"))
	}
	fmt.Fprintln(&b, "\n"+Header.Render("Configuration"))
	if i.Config != nil {
		field("Entrypoint", commandArgs(i.Config.Entrypoint))
		field("Command", commandArgs(i.Config.Cmd))
		field("Working directory", i.Config.WorkingDir)
		field("User", i.Config.User)
	}
	fmt.Fprintln(&b, "\n"+Header.Render("Labels"))
	var labels []string
	if i.Config != nil {
		for key, value := range i.Config.Labels {
			labels = append(labels, key+"="+value)
		}
	}
	slices.Sort(labels)
	if len(labels) == 0 {
		labels = []string{"none"}
	}
	fmt.Fprintln(&b, strings.Join(labels, "\n"))
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
