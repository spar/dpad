package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"

	"github.com/spar/dpad/internal/docker"
	"github.com/spar/dpad/internal/sortx"
)

func composeStatsContainers() []container.Summary {
	return []container.Summary{
		{ID: "a", Names: []string{"/shop-api-1"}, State: container.StateRunning, Labels: map[string]string{composeProjectLabel: "shop", composeServiceLabel: "api"}},
		{ID: "b", Names: []string{"/shop-db-1"}, State: container.StateRunning, Labels: map[string]string{composeProjectLabel: "shop", composeServiceLabel: "db"}},
		{ID: "c", Names: []string{"/zoo-worker-1"}, State: container.StateRunning, Labels: map[string]string{composeProjectLabel: "zoo", composeServiceLabel: "worker"}},
		{ID: "solo", Names: []string{"/standalone"}, State: container.StateRunning},
		{ID: "stopped", State: container.StateExited, Labels: map[string]string{composeProjectLabel: "shop"}},
	}
}
func composeStatsSample(id string, cpu float64, mem uint64) docker.StatsUpdate {
	names := map[string]string{"a": "shop-api-1", "b": "shop-db-1", "c": "zoo-worker-1", "solo": "standalone"}
	return docker.StatsUpdate{Stats: docker.Stats{ID: id, Name: names[id], CPUPercent: cpu, MemUsage: mem, MemLimit: 100 * 1024 * 1024, MemPercent: 25, NetRx: 1000, NetTx: 2000, BlockRead: 3000, BlockWrite: 4000, PIDs: 3}, Generation: 1, SampledAt: time.Now(), Rates: docker.Rates{NetRx: 100, NetTx: 200, BlockRead: 300, BlockWrite: 400, NetValid: true, BlockValid: true}}
}
func newComposeStats(t *testing.T) *StatsView {
	t.Helper()
	v := NewStatsView(t.Context(), &fakeClient{}, sortx.CPU, false)
	v.SetSize(220, 25)
	v.SetGrouped(true)
	v.Update(containersMsg{items: composeStatsContainers()})
	v.Focus()
	return v
}
func fillComposeStats(v *StatsView) {
	v.Update(statsBatchMsg{composeStatsSample("a", 20, 1024*1024), composeStatsSample("b", 30, 2*1024*1024), composeStatsSample("c", 10, 1024), composeStatsSample("solo", 5, 1024)})
}

func TestComposeStatsCollapseAggregatesAndPendingSamples(t *testing.T) {
	v := newComposeStats(t)
	p := v.projects[statsProjectKey("shop")]
	if len(p.members) != 2 || p.available != 0 {
		t.Fatal("pending members or stopped containers counted incorrectly", p)
	}
	if !strings.Contains(v.Status(), "CPU —") {
		t.Fatal(v.Status())
	}
	v.Update(statsBatchMsg{composeStatsSample("a", 20, 1024*1024)})
	rows, _ := v.visible()
	for _, r := range rows {
		if r.key == statsProjectKey("shop") && ansi.Strip(r.cells[1]) != "—" {
			t.Fatal("partial project presented as complete", r)
		}
	}
	v.Update(statsBatchMsg{composeStatsSample("c", 10, 1024)})
	v.HandleKey("a")
	rows, _ = v.visible()
	if rows[0].key != statsProjectKey("shop") {
		t.Fatal("unavailable samples changed alphabetical project order")
	}
	v.HandleKey("c")
	fillComposeStats(v)
	rows, _ = v.visible()
	if len(rows) != 3 {
		t.Fatal("projects should start collapsed, with standalone containers visible", rows)
	}
	if rows[0].key != statsProjectKey("shop") || rows[0].id != "" {
		t.Fatal("project not sorted by aggregate CPU or offered a container action")
	}
	p = v.projects[statsProjectKey("shop")]
	if p.summary.CPUPercent != 50 || p.summary.MemUsage != 3*1024*1024 || p.summary.PIDs != 6 || p.summary.NetRx != 2000 || p.available != 2 || p.rates.NetRx != 200 {
		t.Fatal("wrong aggregate", p)
	}
	if ansi.Strip(rows[0].cells[3]) != "3MiB" || ansi.Strip(rows[0].cells[4]) != "—" {
		t.Fatal("project invented a shared memory limit or percent", rows[0].cells)
	}
	v.cur.key = statsProjectKey("shop")
	v.HandleKey("space")
	rows, _ = v.visible()
	if len(rows) != 5 || rows[1].id != "b" || !strings.Contains(rows[1].cells[0], "db") {
		t.Fatal("members missing or not CPU-sorted", rows)
	}
	v.cur.key = "a"
	if id, _, ok := v.Selected(); !ok || id != "a" || !strings.Contains(v.View(), "History · shop-api-1") {
		t.Fatal("member selection/history lost")
	}
	v.HandleKey("space")
	if v.cur.key != statsProjectKey("shop") || v.expanded[statsProjectKey("shop")] {
		t.Fatal("space on member did not return to project")
	}
}

func TestComposeStatsFiltersModesAndSelection(t *testing.T) {
	v := newComposeStats(t)
	fillComposeStats(v)
	for _, filter := range []string{"SHOP", "api", "shop-api-1", "a"} {
		v.SetFilter(filter)
		rows, _ := v.visible()
		found := false
		for _, r := range rows {
			if r.key == statsProjectKey("shop") {
				found = true
			}
		}
		if !found {
			t.Fatal("project match lost", filter)
		}
	}
	v.SetFilter("api")
	v.cur.key = statsProjectKey("shop")
	v.HandleKey("space")
	rows, _ := v.visible()
	if len(rows) != 3 || !strings.Contains(rows[0].cells[0], "2/2") {
		t.Fatal("filter changed whole-project membership or totals", rows)
	}
	v.cur.key = "b"
	v.HandleKey("v")
	if v.grouped || v.filter != "api" || len(v.history) != 4 {
		t.Fatal("flat toggle lost filter/history")
	}
	v.SetFilter("shop")
	v.cur.key = "b"
	v.HandleKey("v")
	if !v.grouped || !v.expanded[statsProjectKey("shop")] {
		t.Fatal("expanded state was lost")
	}
	if id, _, ok := v.Selected(); !ok || id != "b" {
		t.Fatal("mode toggle lost visible member selection")
	}
	v.HandleKey("t")
	rows, _ = v.visible()
	if strings.Contains(rows[0].cells[6], "/s") || !v.totals {
		t.Fatal("group ignored totals mode")
	}
	v.HandleKey("n")
	v.HandleKey("r")
	if id, _, _ := v.Selected(); id != "b" {
		t.Fatal("sort changed selected member")
	}
}

func TestComposeStatsUnavailableStaleAndRecovery(t *testing.T) {
	v := newComposeStats(t)
	fillComposeStats(v)
	key := statsProjectKey("shop")
	v.cur.key = key
	v.HandleKey("space")
	fail := docker.StatsUpdate{Stats: docker.Stats{ID: "b", Name: "shop-db-1"}, Generation: 1, Unavailable: true, Err: errors.New("disconnected")}
	v.Update(statsBatchMsg{fail})
	rows, _ := v.visible()
	found := false
	for _, r := range rows {
		if r.key == key {
			found = true
			if ansi.Strip(r.cells[1]) != "—" || !strings.Contains(r.cells[0], "1/2") {
				t.Fatal("stale aggregate remained numeric", r)
			}
		}
	}
	if !found || v.history["b"] != nil {
		t.Fatal("group disappeared or stale history retained")
	}
	v.Update(statsBatchMsg{composeStatsSample("b", 30, 2*1024*1024)})
	if v.projects[key].available != 2 || v.projects[key].rates.NetValid {
		t.Fatal("recovery did not reset the rate baseline")
	}
	v.Update(statsBatchMsg{composeStatsSample("b", 30, 2*1024*1024)})
	if !v.projects[key].rates.NetValid {
		t.Fatal("second fresh sample did not restore rates")
	}
	h := v.health["a"]
	h.received = time.Now().Add(-statsStaleAfter - time.Second)
	v.health["a"] = h
	v.expireSamples(time.Now())
	if v.projects[key].available != 1 {
		t.Fatal("quiet stream expiry left aggregate live")
	}
	v.Update(containersMsg{err: errors.New("metadata offline")})
	if v.projects[key] == nil || !v.expanded[key] || !strings.Contains(v.Status(), "metadata stale") {
		t.Fatal("metadata error destroyed grouping")
	}
	v.Update(containersMsg{items: composeStatsContainers()})
	if v.membershipErr != nil {
		t.Fatal("metadata did not recover")
	}
	// A watcher restart retains expected membership and expansion but no old values.
	v.Update(statsStartedMsg{generation: v.watchGen, ch: make(chan docker.StatsUpdate)})
	if v.projects[key].available != 0 || !v.expanded[key] || len(v.history) != 0 {
		t.Fatal("watcher restart lost view state or retained measurements")
	}
}

func TestComposeStatsRateValiditySortingAndMembershipChanges(t *testing.T) {
	v := newComposeStats(t)
	fillComposeStats(v)
	key := statsProjectKey("shop")
	sample := composeStatsSample("b", 30, 2048)
	sample.Rates = docker.Rates{BlockValid: true, BlockRead: 123}
	v.Update(statsBatchMsg{sample})
	if v.projects[key].rates.NetValid || !v.projects[key].rates.BlockValid {
		t.Fatal("invalid component was summed as zero")
	}
	v.HandleKey("n")
	rows, _ := v.visible()
	if rows[len(rows)-1].key != key {
		t.Fatal("unknown network rate did not sort last")
	}
	v.HandleKey("r")
	rows, _ = v.visible()
	if rows[len(rows)-1].key != key {
		t.Fatal("reverse put unknown rate first")
	}
	v.cur.key = key
	v.HandleKey("space")
	members := composeStatsContainers()
	members[0].State = container.StateExited
	members[1].State = container.StateExited
	v.Update(containersMsg{items: members})
	v.Update(statsBatchMsg{{Stats: docker.Stats{ID: "a"}, Gone: true, Generation: 1}, {Stats: docker.Stats{ID: "b"}, Gone: true, Generation: 1}})
	if v.projects[key] != nil || v.expanded[key] {
		t.Fatal("empty stopped project retained data or expansion state")
	}
	members[0].State = container.StateRunning
	v.Update(containersMsg{items: members})
	if p := v.projects[key]; p == nil || len(p.members) != 1 || p.available != 0 {
		t.Fatal("new member not shown pending")
	}
}

func TestComposeStatsRestartingMemberDoesNotBlankProject(t *testing.T) {
	v := newComposeStats(t)
	members := composeStatsContainers()
	members[1].State = container.StateRestarting
	v.Update(containersMsg{items: members})
	v.Update(statsBatchMsg{composeStatsSample("a", 20, 1024*1024)})
	p := v.projects[statsProjectKey("shop")]
	if len(p.members) != 1 || p.available != 1 {
		t.Fatal("restarting member kept the project waiting for a sample", p)
	}
	rows, _ := v.visible()
	for _, r := range rows {
		if r.key == statsProjectKey("shop") && ansi.Strip(r.cells[1]) == "—" {
			t.Fatal("project row stayed blank", r)
		}
	}
}

func TestAppComposeStatsProjectAndContainerActions(t *testing.T) {
	a := newTestApp()
	f := &fakeClient{}
	a.client = f
	v := a.tabs[2].(*StatsView)
	v.client = f
	a.Update(containersMsg{items: composeStatsContainers()})
	press(a, "3")
	a.Update(statsBatchMsg{composeStatsSample("a", 20, 1024), composeStatsSample("b", 30, 2048), composeStatsSample("c", 10, 1024)})
	if !v.grouped {
		t.Fatal("TUI Stats should start grouped")
	}
	v.cur.key = statsProjectKey("shop")
	for _, k := range []string{"S", "x", "R", "e", "d"} {
		press(a, k)
		if a.confirm != nil || a.shell != nil || a.details != nil {
			t.Fatal("project header offered container action", k)
		}
	}
	press(a, "enter")
	if !v.expanded[statsProjectKey("shop")] || a.logs != nil {
		t.Fatal("project enter did not expand")
	}
	v.cur.key = "b"
	cmd := a.handleKey(key("d"))
	if cmd == nil || a.details == nil || a.details.id != "b" {
		t.Fatal("member details opened wrong container")
	}
	a.Update(cmd())
	press(a, "esc")
	if id, _, _ := v.Selected(); id != "b" || !v.expanded[statsProjectKey("shop")] {
		t.Fatal("details return lost selection/expansion")
	}
	press(a, "space")
	if !strings.Contains(ansi.Strip(a.footer()), "expand/collapse") {
		t.Fatal(a.footer())
	}
	flat := NewStatsView(t.Context(), f, sortx.CPU, false)
	flat.noHighlight = true
	flat.HandleKey("v")
	if flat.grouped {
		t.Fatal("standalone stats gained a metadata-dependent project mode")
	}
}

func BenchmarkComposeStatsUpdateAndView(b *testing.B) {
	for _, expanded := range []bool{false, true} {
		b.Run(fmt.Sprint("expanded=", expanded), func(b *testing.B) {
			v, batch := benchmarkStatsView(1000)
			v.SetGrouped(true)
			v.requested = true
			members := make([]container.Summary, len(batch))
			for i, u := range batch {
				members[i] = container.Summary{ID: u.Stats.ID, Names: []string{"/" + u.Stats.Name}, State: container.StateRunning, Labels: map[string]string{composeProjectLabel: fmt.Sprintf("project-%03d", i/10), composeServiceLabel: "worker"}}
			}
			v.Update(containersMsg{items: members})
			if expanded {
				for key := range v.projects {
					v.expanded[key] = true
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				v.Update(batch)
				v.View()
			}
		})
	}
}

func TestComposeStatsLateMetadataKeepsFocusOnSelectedProject(t *testing.T) {
	v := NewStatsView(t.Context(), &fakeClient{}, sortx.CPU, false)
	v.SetGrouped(true)
	v.Focus()
	v.Update(statsBatchMsg{composeStatsSample("b", 5, 1024), composeStatsSample("c", 100, 2048)})
	v.cur.key = "b"
	v.Update(containersMsg{items: composeStatsContainers()})
	if v.cur.key != statsProjectKey("shop") {
		t.Fatal("late labels moved focus to another project", v.cur.key)
	}
	v.HandleKey("space")
	v.cur.key = "b"
	members := composeStatsContainers()
	members[1].State = container.StateExited
	v.Update(containersMsg{items: members})
	v.Update(statsBatchMsg{{Stats: docker.Stats{ID: "b"}, Generation: 1, Gone: true}})
	if v.cur.key != statsProjectKey("shop") {
		t.Fatal("removed member moved focus out of its project", v.cur.key)
	}
}
