package render

import (
	"fmt"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"

	"dockernext/internal/docker"
)

var (
	ContainerHeaders = []string{"CONTAINER ID", "NAME", "IMAGE", "STATE", "STATUS", "HEALTH", "PORTS"}
	ContainerMax     = []int{0, 30, 40, 0, 0, 0, 60}

	ImageHeaders = []string{"REPOSITORY", "TAG", "IMAGE ID", "CREATED", "SIZE"}
	ImageAlign   = []Align{Left, Left, Left, Left, Right}
	ImageMax     = []int{50, 30}

	StatsHeaders = []string{"NAME", "CPU %", "", "MEM USAGE / LIMIT", "MEM %", "", "NET I/O", "BLOCK I/O", "PIDS"}
	StatsAlign   = []Align{Left, Right, Left, Right, Right, Left, Right, Right, Right}
	StatsMax     = []int{30}
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

// SplitRepoTag splits "registry:5000/app:v1" into repo and tag.
func SplitRepoTag(rt string) (string, string) {
	i := strings.LastIndex(rt, ":")
	if i < 0 || strings.Contains(rt[i:], "/") {
		return rt, "<none>"
	}
	return rt[:i], rt[i+1:]
}

func StatsRow(s docker.Stats) []string {
	return []string{
		Bold.Render(s.Name),
		PercentStyle(s.CPUPercent).Render(fmt.Sprintf("%.2f%%", s.CPUPercent)),
		Bar(s.CPUPercent, barWidth),
		fmt.Sprintf("%s / %s", BinSize(s.MemUsage), BinSize(s.MemLimit)),
		PercentStyle(s.MemPercent).Render(fmt.Sprintf("%.2f%%", s.MemPercent)),
		Bar(s.MemPercent, barWidth),
		fmt.Sprintf("%s / %s", Size(int64(s.NetRx)), Size(int64(s.NetTx))),
		fmt.Sprintf("%s / %s", Size(int64(s.BlockRead)), Size(int64(s.BlockWrite))),
		fmt.Sprint(s.PIDs),
	}
}
