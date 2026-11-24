package tui

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"dpad/internal/docker"
	"dpad/internal/sortx"
)

func TestStatsHistoryAggregatesPeaksAndLeavesGaps(t *testing.T) {
	var h statsHistory
	now := time.Unix(5000, 0)
	for _, s := range []docker.StatsUpdate{
		{SampledAt: time.Unix(1000, 0), Stats: docker.Stats{CPUPercent: 10, MemUsage: 100}},
		{SampledAt: time.Unix(1000, 500000000), Stats: docker.Stats{CPUPercent: 90, MemUsage: 300}},
		{SampledAt: time.Unix(1002, 0), Stats: docker.Stats{CPUPercent: 20, MemUsage: 200}},
	} {
		if !h.add(s.Stats, s.SampledAt, now) {
			t.Fatal("valid sample rejected")
		}
	}
	cpu, _, s := h.chart(h.endSecond(now), 120)
	if s.count != 3 || s.cpuAvg != 40 || s.cpuPeak != 90 || s.memAvg != 200 || s.memPeak != 300 {
		t.Fatalf("wrong sample summary: %+v", s)
	}
	bars := []rune(cpu)
	if bars[117] != '█' || bars[118] != ' ' || bars[119] != '▂' {
		t.Fatalf("spike or gap lost: %q", cpu)
	}
	compressed, _, _ := h.chart(1002, 12)
	if []rune(compressed)[11] != '█' {
		t.Fatal("downsampling lost a short spike")
	}
	cpu, _, s = h.chart(h.endSecond(now.Add(121*time.Second)), 120)
	if s.count != 0 || strings.TrimSpace(cpu) != "" {
		t.Fatal("expired samples were rendered as current")
	}
	if end := h.endSecond(now.Add(5 * time.Second)); end != 1007 {
		t.Fatalf("remote clock skew was not handled: %d", end)
	}
}

func TestStatsHistoryBoundedAndRejectsDuplicateOrOlderSamples(t *testing.T) {
	var h statsHistory
	now := time.Unix(1000, 0)
	for i := range 3 * statsHistorySeconds {
		at := now.Add(time.Duration(i) * time.Second)
		h.add(docker.Stats{CPUPercent: float64(i), MemUsage: uint64(i)}, at, at)
	}
	_, _, s := h.chart(1359, 60)
	if s.count != 120 || s.cpuPeak != 359 || s.memPeak != 359 {
		t.Fatalf("history ring did not keep only the window: %+v", s)
	}
	for _, at := range []time.Time{time.Unix(1359, 0), time.Unix(1358, 0)} {
		if h.add(docker.Stats{CPUPercent: 999}, at, now) {
			t.Fatal("duplicate/out-of-order sample changed history")
		}
	}
	if h.add(docker.Stats{CPUPercent: math.NaN()}, time.Unix(1360, 0), now) {
		t.Fatal("invalid sample corrupted history")
	}
	if h.version != 360 {
		t.Fatal("rejected sample invalidated cache")
	}
	if got := statsSparkline([]float64{0, 100, 400}, []bool{true, true, true}, 400); got != "·▂█" {
		t.Fatalf("CPU above 100%% was clipped or zero treated as a gap: %q", got)
	}
}

func TestStatsHistoryUpdateHasNoAllocations(t *testing.T) {
	var h statsHistory
	s := docker.Stats{CPUPercent: 10, MemUsage: 1024}
	at := time.Unix(1000, 0)
	if allocations := testing.AllocsPerRun(1000, func() { at = at.Add(time.Millisecond); h.add(s, at, at) }); allocations != 0 {
		t.Fatalf("history insertion allocated %.1f objects", allocations)
	}
}

func TestStatsHistoryResetsOnStreamGenerationAndReleasesStoppedContainers(t *testing.T) {
	v := NewStatsView(context.Background(), nil, sortx.Name, false)
	at := time.Now()
	sample := func(generation uint64, offset time.Duration, cpu float64) docker.StatsUpdate {
		return docker.StatsUpdate{Generation: generation, SampledAt: at.Add(offset), Stats: docker.Stats{ID: "web", Name: "web", CPUPercent: cpu}}
	}
	v.Update(statsBatchMsg{sample(1, 0, 90), sample(1, time.Second, 100)})
	old := v.history["web"]
	v.Update(statsBatchMsg{sample(2, 2*time.Second, 10)})
	_, _, s := v.history["web"].chart(at.Add(2*time.Second).Unix(), 60)
	if v.history["web"] == old || s.count != 1 || s.cpuPeak != 10 {
		t.Fatal("restart retained prior stream's peaks")
	}
	v.Update(statsBatchMsg{sample(1, 3*time.Second, 999), {Stats: docker.Stats{ID: "web"}, Gone: true, Generation: 1}})
	if v.data["web"].CPUPercent != 10 {
		t.Fatal("stale stream replaced the restarted container")
	}
	v.Update(statsBatchMsg{{Stats: docker.Stats{ID: "web"}, Gone: true, Generation: 2}})
	if len(v.history) != 0 || len(v.data) != 0 {
		t.Fatal("stopped container retained history")
	}
	v.noHighlight = true
	v.Update(statsBatchMsg{sample(3, 4*time.Second, 5)})
	if len(v.history) != 0 || len(v.data) != 1 {
		t.Fatal("standalone stats allocated unused history")
	}
}

func TestStatsHistoryPanelFollowsSelectionAndFitsTerminal(t *testing.T) {
	v := NewStatsView(context.Background(), nil, sortx.Name, false)
	v.SetSize(100, 15)
	now := time.Now()
	v.Update(statsBatchMsg{
		{SampledAt: now, Stats: docker.Stats{ID: "a", Name: "alpha", CPUPercent: 10, MemUsage: 1024}},
		{SampledAt: now, Stats: docker.Stats{ID: "b", Name: "beta", CPUPercent: 200, MemUsage: 4096}},
	})
	v.HandleKey("j")
	out := ansi.Strip(v.View())
	if !strings.Contains(out, "History · beta") || !strings.Contains(out, "peak 200.0%") || strings.Count(out, "\n")+1 != 15 {
		t.Fatalf("selected history did not fit layout: %s", out)
	}
	v.SetFilter("alpha")
	out = ansi.Strip(v.View())
	if strings.Contains(out, "beta") || !strings.Contains(out, "History · alpha") {
		t.Fatal("history did not follow filtered selection")
	}
	v.historyPanel("a", "alpha", now)
	if allocations := testing.AllocsPerRun(1000, func() { v.historyPanel("a", "alpha", now) }); allocations != 0 {
		t.Fatalf("unchanged history panel allocated %.1f objects", allocations)
	}
	for line := range strings.SplitSeq(v.View(), "\n") {
		if ansi.StringWidth(line) > 100 {
			t.Fatal("history overflowed width")
		}
	}
	v.SetSize(60, 8)
	if strings.Contains(v.View(), "History") {
		t.Fatal("history crowded small terminal")
	}
	v.SetSize(100, 15)
	v.noHighlight = true
	if strings.Contains(v.View(), "History") {
		t.Fatal("standalone stats displayed an implicit selection's history")
	}
}
