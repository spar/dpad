package render

import (
	"fmt"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"

	"dockernext/internal/docker"
)

var (
	ContainerColumns = []Column{
		{Header: "CONTAINER ID"}, {Header: "NAME", Max: 30}, {Header: "IMAGE", Max: 40},
		{Header: "STATE"}, {Header: "STATUS"}, {Header: "HEALTH"}, {Header: "PORTS", Max: 60},
	}
	ImageColumns = []Column{
		{Header: "REPOSITORY", Max: 50}, {Header: "TAG", Max: 30}, {Header: "IMAGE ID"},
		{Header: "CREATED"}, {Header: "SIZE", Align: Right},
	}
	StatsColumns = []Column{
		{Header: "NAME", Max: 30}, {Header: "CPU %", Align: Right}, {},
		{Header: "MEM USAGE / LIMIT", Align: Right}, {Header: "MEM %", Align: Right}, {},
		{Header: "NET I/O", Align: Right}, {Header: "BLOCK I/O", Align: Right}, {Header: "PIDS", Align: Right},
	}
)

const barWidth = 10

func ContainerRow(c container.Summary) []string {
	state := string(c.State)
	st := StateStyle(state, ExitCode(c.Status))
	health := ""
	if c.Health != nil {
		health = HealthBadge(string(c.Health.Status))
	}
	img := c.Image
	if strings.HasPrefix(img, "sha256:") {
		img = ShortID(img)
	}
	return []string{
		Dim.Render(ShortID(c.ID)),
		Bold.Render(docker.ContainerName(c.Names)),
		img,
		st.Render(state),
		st.Render(c.Status),
		health,
		fg(Cyan).Render(Ports(c.Ports)),
	}
}

// ImageRows renders one row per repo:tag; untagged (dangling) images are
// shown as <none> and dimmed.
func ImageRows(img image.Summary) [][]string {
	id := ShortID(img.ID)
	created, size := Ago(img.Created), Size(img.Size)
	if len(img.RepoTags) == 0 {
		return [][]string{{
			Dim.Render("<none>"), Dim.Render("<none>"), Dim.Render(id), Dim.Render(created), Dim.Render(size),
		}}
	}
	rows := make([][]string, 0, len(img.RepoTags))
	for _, rt := range img.RepoTags {
		repo, tag := SplitRepoTag(rt)
		rows = append(rows, []string{Bold.Render(repo), fg(Yellow).Render(tag), Dim.Render(id), created, size})
	}
	return rows
}

// ImageSummary describes a set of images, e.g. "12 images · 1.2GB total · 1 dangling".
func ImageSummary(items []image.Summary) string {
	var total int64
	dangling := 0
	for _, img := range items {
		total += img.Size
		if len(img.RepoTags) == 0 {
			dangling++
		}
	}
	return fmt.Sprintf("%d images · %s total · %d dangling", len(items), Size(total), dangling)
}

// SplitRepoTag splits "registry:5000/app:v1" into repo and tag.
func SplitRepoTag(rt string) (string, string) {
	i := strings.LastIndex(rt, ":")
	if i < 0 || strings.Contains(rt[i:], "/") {
		return rt, "<none>"
	}
	return rt[:i], rt[i+1:]
}

func StatsRow(s docker.Stats) []string {
	return statsRow(s, fmt.Sprintf("%s / %s", Size(int64(s.NetRx)), Size(int64(s.NetTx))), fmt.Sprintf("%s / %s", Size(int64(s.BlockRead)), Size(int64(s.BlockWrite))))
}

func StatsRateRow(s docker.Stats, r docker.Rates) []string {
	net, block := "— / —", "— / —"
	if r.NetValid {
		net = byteRatePair(r.NetRx, r.NetTx)
	}
	if r.BlockValid {
		block = byteRatePair(r.BlockRead, r.BlockWrite)
	}
	return statsRow(s, net, block)
}

func UnavailableStatsRow(s docker.Stats) []string {
	return []string{Bold.Render(s.Name), "—", "", "—", "—", "", "— / —", "— / —", "—"}
}

func statsRow(s docker.Stats, net, block string) []string {
	return []string{
		Bold.Render(s.Name),
		PercentStyle(s.CPUPercent).Render(fmt.Sprintf("%.2f%%", s.CPUPercent)),
		Bar(s.CPUPercent, barWidth),
		fmt.Sprintf("%s / %s", BinSize(s.MemUsage), BinSize(s.MemLimit)),
		PercentStyle(s.MemPercent).Render(fmt.Sprintf("%.2f%%", s.MemPercent)),
		Bar(s.MemPercent, barWidth),
		net,
		block,
		fmt.Sprint(s.PIDs),
	}
}
