package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/volume"

	"github.com/spar/dpad/internal/render"
)

func TestNavigationKeepsViewportUntilSelectionLeavesIt(t *testing.T) {
	v := NewContainersView(t.Context(), &fakeClient{})
	items := make([]container.Summary, 30)
	for i := range items {
		items[i] = composeContainer(fmt.Sprintf("web-%02d", i), "demo", "web", "running")
	}
	v.Update(containersMsg{items: items})
	v.SetSize(120, 6)
	v.View()
	for range 10 {
		v.HandleKey("j")
		v.View()
	}
	if v.top != 6 {
		t.Fatal("selected row not scrolled into view", v.top)
	}
	firstPage := strings.Split(ansi.Strip(v.View()), "\n")
	for range 4 {
		v.HandleKey("k")
		v.View()
	}
	page := strings.Split(ansi.Strip(v.View()), "\n")
	for i := range page {
		page[i] = strings.TrimRight(page[i], " ")
		firstPage[i] = strings.TrimRight(firstPage[i], " ")
	}
	if v.top != 6 || strings.Join(page, "\n") != strings.Join(firstPage, "\n") {
		t.Fatal("moving within viewport shifted rows", v.top, page)
	}
	v.HandleKey("k")
	v.View()
	if v.top != 5 {
		t.Fatal("selection moved above viewport without scrolling", v.top)
	}
	v.HandleKey("G")
	v.View()
	if v.top != 25 {
		t.Fatal("last page missing", v.top)
	}
	v.SetSize(120, 15)
	v.View()
	if v.top != 16 {
		t.Fatal("resize left an unnecessarily short viewport", v.top)
	}
	v.SetFilter("web-29")
	if out := v.View(); v.top != 0 || !strings.Contains(out, "web-29") {
		t.Fatal("filter left viewport past data", v.top, out)
	}
	v.SetFilter("absent")
	v.View()
	if v.top != 0 {
		t.Fatal("empty list kept stale viewport")
	}
}

func TestIdenticalRefreshReusesLayoutAndNewColumnsInvalidateIt(t *testing.T) {
	v := NewVolumesView(t.Context(), volumeFake())
	v.Update(v.Focus()())
	v.Update(containersMsg{items: volumeContainers()})
	v.View()
	table := v.table
	v.Update(containersMsg{items: volumeContainers()})
	v.View()
	if v.table != table {
		t.Fatal("unchanged usage remeasured all volume rows")
	}
	v.Update(volumesMsg{v: v, items: volumeFake().volumes})
	v.View()
	if v.table != table {
		t.Fatal("identical inventory refresh remeasured volume rows")
	}
	v.SetSize(60, 10)
	out := v.View()
	if v.table == table || strings.Contains(ansi.Strip(out), "PROJECT") {
		t.Fatal("responsive columns retained obsolete layout")
	}
	table = v.table
	items := volumeContainers()
	items[0].State = container.StateExited
	v.Update(containersMsg{items: items})
	if out = v.View(); v.table == table || !strings.Contains(out, "0 running / 2 total") {
		t.Fatal("changed usage retained stale cells", out)
	}
	table = v.table
	v.Update(volumesMsg{v: v, items: []volume.Volume{{Name: "new-volume", Driver: "local"}}})
	if out = v.View(); v.table == table || !strings.Contains(out, "new-volume") || strings.Contains(out, "data") {
		t.Fatal("changed inventory retained old cells", out)
	}
	v.columns = []render.Column{{Header: "RENAMED"}}
	v.rowCache = nil
	if out = v.View(); !strings.Contains(out, "RENAMED") {
		t.Fatal("column-only update ignored", out)
	}
}

func TestVolumeUsageSnapshotPreservesMountDetailsAndRecovery(t *testing.T) {
	v := NewVolumesView(t.Context(), volumeFake())
	v.Update(v.Focus()())
	items := volumeContainers()
	v.Update(containersMsg{items: items})
	v.View()
	items[0].Mounts[0].Destination = "/changed"
	items[0].Mounts[0].RW = false
	v.Update(containersMsg{items: items})
	users := v.usage["data"]
	if users[0].Mounts[0].Path != "/changed" || !users[0].Mounts[0].ReadOnly {
		t.Fatal("cached mount details are stale", users)
	}
	items[0].Names = []string{"/renamed"}
	v.Update(containersMsg{items: items})
	v.SetFilter("renamed")
	if out := v.View(); !strings.Contains(out, "data") || strings.Contains(out, "empty") {
		t.Fatal("cached usage kept stale filter names", out)
	}
	v.Update(containersMsg{err: errors.New("offline")})
	if out := v.View(); strings.Contains(out, "1 running") || v.usageKnown() {
		t.Fatal("failed refresh reused known usage", out)
	}
	v.Update(containersMsg{items: items})
	if out := v.View(); !strings.Contains(out, "1 running / 2 total") || !v.usageKnown() {
		t.Fatal("usage recovery did not restore references", out)
	}
	items[0].Mounts = nil
	v.Update(containersMsg{items: items})
	if out := v.View(); strings.Contains(out, "data") {
		t.Fatal("removed reference retained filter match", out)
	}
}

func TestProjectSnapshotSkipsOnlyUnchangedDisplayAndFilterData(t *testing.T) {
	changes := map[string]func(*container.Summary){
		"identity": func(c *container.Summary) { c.ID = "replacement" },
		"name":     func(c *container.Summary) { c.Names = []string{"/renamed"} },
		"project":  func(c *container.Summary) { c.Labels[composeProjectLabel] = "other" },
		"service":  func(c *container.Summary) { c.Labels[composeServiceLabel] = "db" },
		"image":    func(c *container.Summary) { c.Image = "new-image" },
		"state":    func(c *container.Summary) { c.State = container.StateExited },
		"status":   func(c *container.Summary) { c.Status = "Exited (1)" },
		"health":   func(c *container.Summary) { c.Health = &container.HealthSummary{Status: container.Unhealthy} },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			v := NewProjectsView()
			v.SetSize(140, 10)
			items := []container.Summary{composeContainer("web", "demo", "web", "running")}
			v.Update(containersMsg{items: items})
			v.View()
			version := v.rowVersion
			v.Update(containersMsg{items: []container.Summary{composeContainer("web", "demo", "web", "running")}})
			v.View()
			if v.rowVersion != version {
				t.Fatal("identical snapshot rebuilt project rows")
			}
			v.Update(containersMsg{err: errors.New("offline")})
			if !strings.Contains(v.View(), "offline") {
				t.Fatal("error hidden by snapshot")
			}
			v.Update(containersMsg{items: items})
			v.View()
			if v.err != nil || v.rowVersion != version {
				t.Fatal("recovery invalidated unchanged data")
			}
			change(&items[0])
			v.Update(containersMsg{items: items})
			v.View()
			if v.rowVersion == version {
				t.Fatal("snapshot ignored changed display/filter data")
			}
			if name == "image" {
				v.SetFilter("new-image")
				if !strings.Contains(v.View(), "demo") {
					t.Fatal("stale image filtering")
				}
			}
			v.Update(containersMsg{})
			if v.selectedProject() != nil || len(v.items) != 0 {
				t.Fatal("removed project retained")
			}
		})
	}
}
