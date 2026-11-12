package tui

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"dockernext/internal/docker"
	"dockernext/internal/sortx"
)

func benchmarkStatsView(n int) (*StatsView, statsBatchMsg) {
	v := NewStatsView(context.Background(), nil, sortx.CPU, false)
	v.SetSize(160, 35)
	batch := make(statsBatchMsg, n)
	for i := range batch {
		batch[i] = docker.StatsUpdate{Stats: docker.Stats{ID: fmt.Sprintf("%064d", i), Name: fmt.Sprintf("container-%04d", i), CPUPercent: float64(i % 150), MemUsage: uint64(i+1) * 1024 * 1024, MemLimit: 1024 * 1024 * 1024}, Rates: docker.Rates{NetRx: 1024, NetTx: 2048, BlockRead: 4096, BlockWrite: 8192, NetValid: true, BlockValid: true}}
	}
	v.Update(batch)
	return v, batch
}

func BenchmarkStatsHistoryAdd(b *testing.B) {
	var h statsHistory
	s := docker.Stats{CPUPercent: 45, MemUsage: 1024 * 1024}
	at := time.Unix(1000, 0)
	b.ReportAllocs()
	for b.Loop() {
		at = at.Add(time.Second)
		h.add(s, at, at)
	}
	b.ReportMetric(float64(reflect.TypeFor[statsHistory]().Size()), "B/container")
}

func BenchmarkStatsHistoryPanel(b *testing.B) {
	v, _ := benchmarkStatsView(1)
	id := fmt.Sprintf("%064d", 0)
	h := &statsHistory{}
	v.history[id] = h
	now := time.Now()
	for i := range statsHistorySeconds {
		at := now.Add(time.Duration(i-statsHistorySeconds+1) * time.Second)
		h.add(docker.Stats{CPUPercent: float64(i), MemUsage: uint64(i) * 1024 * 1024}, at, at)
	}
	b.ReportAllocs()
	for b.Loop() {
		h.version++
		v.historyPanel(id, "container", now)
	}
}

func BenchmarkStatsUpdate(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			v, batch := benchmarkStatsView(n)
			b.ReportAllocs()
			for b.Loop() {
				v.Update(batch)
			}
		})
	}
}

func BenchmarkStatsView(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			v, _ := benchmarkStatsView(n)
			v.View()
			b.ReportAllocs()
			for b.Loop() {
				v.View()
			}
		})
	}
}

func BenchmarkStatsUpdateAndView(b *testing.B) {
	v, batch := benchmarkStatsView(1000)
	b.ReportAllocs()
	for b.Loop() {
		v.Update(batch)
		v.View()
	}
}

func BenchmarkStatsRateSortAndView(b *testing.B) {
	v, batch := benchmarkStatsView(1000)
	v.setSort(sortx.Net, false)
	b.ReportAllocs()
	for b.Loop() {
		v.Update(batch)
		v.View()
	}
}
