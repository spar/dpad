package docker

import (
	"math"
	"testing"

	"github.com/moby/moby/api/types/container"
)

func TestCalc(t *testing.T) {
	s := &container.StatsResponse{
		CPUStats: container.CPUStats{
			CPUUsage:    container.CPUUsage{TotalUsage: 2_000_000},
			SystemUsage: 20_000_000,
			OnlineCPUs:  4,
		},
		PreCPUStats: container.CPUStats{
			CPUUsage:    container.CPUUsage{TotalUsage: 1_000_000},
			SystemUsage: 10_000_000,
		},
		MemoryStats: container.MemoryStats{
			Usage: 300,
			Limit: 1000,
			Stats: map[string]uint64{"inactive_file": 100},
		},
		Networks: map[string]container.NetworkStats{
			"eth0": {RxBytes: 10, TxBytes: 20},
			"eth1": {RxBytes: 1, TxBytes: 2},
		},
		BlkioStats: container.BlkioStats{IoServiceBytesRecursive: []container.BlkioStatEntry{
			{Op: "Read", Value: 5}, {Op: "write", Value: 7}, {Op: "read", Value: 1},
		}},
		PidsStats: container.PidsStats{Current: 9},
	}
	got := Calc("id", "web", s)

	// cpuDelta/sysDelta * cpus * 100 = 1e6/1e7 * 4 * 100
	if math.Abs(got.CPUPercent-40) > 1e-9 {
		t.Errorf("CPUPercent = %v, want 40", got.CPUPercent)
	}
	if got.MemUsage != 200 || got.MemPercent != 20 {
		t.Errorf("mem = %d (%v%%), want 200 (20%%)", got.MemUsage, got.MemPercent)
	}
	if got.NetRx != 11 || got.NetTx != 22 {
		t.Errorf("net = %d/%d, want 11/22", got.NetRx, got.NetTx)
	}
	if got.BlockRead != 6 || got.BlockWrite != 7 {
		t.Errorf("block = %d/%d, want 6/7", got.BlockRead, got.BlockWrite)
	}
	if got.PIDs != 9 || got.Name != "web" {
		t.Errorf("pids/name = %d/%q", got.PIDs, got.Name)
	}
}

func TestCalcFirstSampleHasNoCPU(t *testing.T) {
	// A stream's first record has an empty PreCPUStats, as the daemon sends it.
	s := &container.StatsResponse{CPUStats: container.CPUStats{
		CPUUsage: container.CPUUsage{TotalUsage: 1_434_910_833_000}, SystemUsage: 14_299_130_000_000, OnlineCPUs: 4,
	}}
	if got := Calc("", "", s); got.CPUPercent != 0 {
		t.Errorf("CPUPercent = %v, want 0 (no previous sample)", got.CPUPercent)
	}
}

func TestMemUsageCgroupV1(t *testing.T) {
	m := container.MemoryStats{Usage: 500, Stats: map[string]uint64{"total_inactive_file": 200, "inactive_file": 50}}
	if got := memUsageNoCache(m); got != 300 {
		t.Errorf("memUsageNoCache = %d, want 300", got)
	}
}

func TestSplitTimestamp(t *testing.T) {
	ts, text := splitTimestamp("2026-11-10T12:00:00.123456789Z hello world")
	if ts.IsZero() || text != "hello world" {
		t.Errorf("got %v %q", ts, text)
	}
	ts, text = splitTimestamp("no timestamp here")
	if !ts.IsZero() || text != "no timestamp here" {
		t.Errorf("got %v %q", ts, text)
	}
}
