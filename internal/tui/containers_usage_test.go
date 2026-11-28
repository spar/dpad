package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"

	"github.com/spar/dpad/internal/config"
	"github.com/spar/dpad/internal/docker"
	"github.com/spar/dpad/internal/render"
	"github.com/spar/dpad/internal/sortx"
)

func TestStatsHistoryRecentFillsLoneMissingSeconds(t *testing.T) {
	var h statsHistory
	received := time.Unix(1010, 0)
	for _, s := range []struct {
		at  int64
		cpu float64
	}{{1000, 10}, {1002, 30}, {1003, 40}, {1006, 50}, {1008, 60}} {
		h.add(docker.Stats{CPUPercent: s.cpu}, time.Unix(s.at, 0), received)
	}
	cpu, present := h.recent(1009, 10)
	wantPresent := []bool{true, true, true, true, false, false, true, true, true, true}
	wantCPU := []float64{10, 10, 30, 40, 0, 0, 50, 50, 60, 60}
	for i := range cpu {
		if present[i] != wantPresent[i] || cpu[i] != wantCPU[i] {
			t.Fatalf("recent = %v %v, want %v %v", cpu, present, wantCPU, wantPresent)
		}
	}
}

func TestContainersShowLiveUsageFromTheStatsStream(t *testing.T) {
	ctx := context.Background()
	stats := NewStatsView(ctx, &fakeClient{}, sortx.CPU, false)
	v := NewContainersView(ctx, &fakeClient{})
	v.usage = stats
	v.SetSize(140, 10)
	v.Update(containersMsg{items: []container.Summary{
		{ID: "a", Names: []string{"/web"}, Image: "nginx", State: container.StateRunning, Status: "Up 2 hours"},
		{ID: "b", Names: []string{"/old"}, Image: "nginx", State: container.StateExited, Status: "Exited (0)"},
	}})
	if v.Focus() != nil || stats.watching {
		t.Fatal("stats should not be watched while the CPU graph is off")
	}
	if strings.Contains(v.View(), "CPU") {
		t.Fatal("CPU column shown while the graph is off")
	}

	v.setGraph(render.GraphBars)
	if v.Focus() == nil || !stats.watching {
		t.Fatal("focusing Containers with a CPU graph should start the stats stream")
	}
	ch := make(chan docker.StatsUpdate)
	stats.Update(statsStartedMsg{ch: ch, generation: stats.watchGen})
	now := time.Now()
	msg := statsSourceBatchMsg{ch: ch, batch: statsBatchMsg{
		{Stats: docker.Stats{ID: "a", Name: "web", CPUPercent: 42, MemUsage: 300 << 20}, SampledAt: now},
	}}
	v.View()
	v.Update(msg)
	stats.Update(msg)
	view := ansi.Strip(v.View())
	for _, want := range []string{"CPU", "MEM", "42.0%", "300M"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	if !strings.Contains(v.Status(), "CPU 42.0% · MEM 300MiB") {
		t.Errorf("status lacks the totals: %q", v.Status())
	}
	for line := range strings.SplitSeq(view, "\n") {
		if strings.Contains(line, "old") && strings.Contains(line, "%") {
			t.Errorf("stopped container shows usage: %q", line)
		}
	}
}

func TestContainersUsageFitsEightyColumns(t *testing.T) {
	ctx := context.Background()
	stats := NewStatsView(ctx, &fakeClient{}, sortx.CPU, false)
	v := NewContainersView(ctx, &fakeClient{})
	v.usage = stats
	v.setGraph(render.GraphBraille)
	v.SetSize(80, 10)
	v.Update(containersMsg{items: []container.Summary{{
		ID: "a", Names: []string{"/expense-tracker-ollama"}, Image: "ollama/ollama:latest", State: container.StateRunning,
		Status: "Up 34 hours (healthy)", Health: &container.HealthSummary{Status: container.Healthy},
		Ports: []container.PortSummary{{PrivatePort: 11434, Type: "tcp"}},
	}}})
	stats.data["a"] = docker.Stats{ID: "a", CPUPercent: 38.4, MemUsage: 5939 << 20}
	stats.history["a"] = &statsHistory{}
	stats.history["a"].add(stats.data["a"], time.Now(), time.Now())
	v.rowCache = nil
	view := ansi.Strip(v.View())
	for _, want := range []string{"NAME", "STATUS", "CPU", "MEM", "PORTS", "38.4%", "5.8G", "11434/tcp"} {
		if !strings.Contains(view, want) {
			t.Errorf("80 columns lack %q:\n%s", want, view)
		}
	}
	for line := range strings.SplitSeq(view, "\n") {
		if w := ansi.StringWidth(line); w > 80 {
			t.Errorf("line is %d wide: %q", w, line)
		}
	}
}

func TestCPUGraphSettingCyclesAndSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("DPAD_CONFIG", path)
	a := newTestApp()
	a.containers().usage = a.tabs[2].(*StatsView)
	a.configure(config.Config{}, nil)
	if got := a.containers().graph; got != defaultCPUGraph {
		t.Fatalf("default graph = %q, want %q", got, defaultCPUGraph)
	}

	press(a, "o")
	a.settings.cur.key = "CPU graph"
	press(a, "enter")
	if got := a.containers().graph; got != render.GraphBars {
		t.Fatalf("enter cycled to %q, want bars", got)
	}
	if !strings.Contains(ansi.Strip(a.render()), "bars") {
		t.Error("settings did not show the new style")
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "cpuGraph: bars") {
		t.Fatalf("config not saved: %q %v", data, err)
	}
	cfg, err := config.Parse(data)
	if err != nil || cfg.CPUGraph != render.GraphBars {
		t.Fatalf("saved config reads back as %q, %v", cfg.CPUGraph, err)
	}

	press(a, "enter", "enter")
	if got := a.containers().graph; got != render.GraphOff {
		t.Fatalf("graph = %q, want off", got)
	}
	data, _ = os.ReadFile(path)
	if cfg, err := config.Parse(data); err != nil || cfg.CPUGraph != render.GraphOff {
		t.Fatalf("off reads back as %q, %v from %q", cfg.CPUGraph, err, data)
	}
}

func TestUnknownCPUGraphFallsBackAndReports(t *testing.T) {
	a := newTestApp()
	a.containers().usage = a.tabs[2].(*StatsView)
	a.configure(config.Config{CPUGraph: "sparkles"}, nil)
	if a.containers().graph != defaultCPUGraph || !a.flashErr || !strings.Contains(a.flash, "sparkles") {
		t.Fatalf("graph %q, flash %q", a.containers().graph, a.flash)
	}
}
