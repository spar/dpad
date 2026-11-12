package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

func TestRatesUseElapsedTimeAndIntegerCounterDeltas(t *testing.T) {
	var tracker rateTracker
	at := time.Unix(1000, 0)
	first := Stats{NetRx: math.MaxUint64 - 1000, NetTx: 100, BlockRead: 200, BlockWrite: 300}
	if r := tracker.update(first, at, 1, 2); r.NetValid || r.BlockValid {
		t.Fatal("first sample manufactured a rate")
	}
	second := first
	second.NetRx += 125
	second.NetTx += 250
	second.BlockRead += 500
	second.BlockWrite += 1000
	r := tracker.update(second, at.Add(500*time.Millisecond), 1, 2)
	if !r.NetValid || !r.BlockValid || r.NetRx != 250 || r.NetTx != 500 || r.BlockRead != 1000 || r.BlockWrite != 2000 {
		t.Fatalf("wrong rates: %+v", r)
	}
	r = tracker.update(second, at.Add(1500*time.Millisecond), 1, 2)
	if !r.NetValid || !r.BlockValid || r.NetRx != 0 || r.BlockWrite != 0 {
		t.Fatal("idle counters were not measured zero")
	}
}

func TestRatesResetGroupsAndRecoverWithoutSpikes(t *testing.T) {
	var tracker rateTracker
	at := time.Unix(1000, 0)
	tracker.update(Stats{NetRx: 1000, NetTx: 1000, BlockRead: 1000, BlockWrite: 1000}, at, 1, 2)
	r := tracker.update(Stats{NetRx: 10, NetTx: 20, BlockRead: 1100, BlockWrite: 1200}, at.Add(time.Second), 1, 2)
	if r.NetValid || !r.BlockValid || r.BlockRead != 100 {
		t.Fatalf("network reset corrupted block rates: %+v", r)
	}
	r = tracker.update(Stats{NetRx: 110, NetTx: 220, BlockRead: 10, BlockWrite: 20}, at.Add(2*time.Second), 1, 2)
	if !r.NetValid || r.NetRx != 100 || r.BlockValid {
		t.Fatalf("block reset corrupted network rates: %+v", r)
	}
	r = tracker.update(Stats{NetRx: 210, NetTx: 420, BlockRead: 110, BlockWrite: 220}, at.Add(3*time.Second), 2, 4)
	if r.NetValid || r.BlockValid {
		t.Fatal("changed counter sources produced a spike")
	}
	r = tracker.update(Stats{NetRx: 310, NetTx: 620, BlockRead: 210, BlockWrite: 420}, at.Add(4*time.Second), 2, 4)
	if !r.NetValid || !r.BlockValid || r.NetRx != 100 || r.BlockWrite != 200 {
		t.Fatal("new counter baseline did not recover")
	}
}

func TestRatesInvalidIntervalsAndUnavailableCounters(t *testing.T) {
	at := time.Unix(1000, 0)
	for _, invalid := range []time.Time{at, at.Add(-time.Second), {}, at.Add(maxRateInterval + time.Second)} {
		var tracker rateTracker
		tracker.update(Stats{NetRx: 100}, at, 1, 2)
		if r := tracker.update(Stats{NetRx: 50000}, invalid, 1, 2); r.NetValid || r.BlockValid {
			t.Fatalf("invalid interval %v produced rates: %+v", invalid, r)
		}
	}
	var tracker rateTracker
	tracker.update(Stats{}, at, 0, 0)
	if r := tracker.update(Stats{}, at.Add(time.Second), 0, 0); r.NetValid || r.BlockValid {
		t.Fatal("unsupported counters shown as measured zero")
	}
	if r := tracker.update(Stats{NetRx: 1000}, at.Add(2*time.Second), 1, 2); r.NetValid || r.BlockValid {
		t.Fatal("returning counters skipped baseline")
	}
	if r := tracker.update(Stats{NetRx: 2000}, at.Add(3*time.Second), 1, 2); !r.NetValid || r.NetRx != 1000 {
		t.Fatal("returning counters did not recover")
	}
	if r := tracker.update(Stats{NetRx: 50000}, at.Add(30*time.Second), 1, 2); r.NetValid {
		t.Fatal("long gap was treated as current throughput")
	}
	if r := tracker.update(Stats{NetRx: 50100}, at.Add(31*time.Second), 1, 2); !r.NetValid || r.NetRx != 100 {
		t.Fatal("long gap did not rebase")
	}
}

func TestRatesIgnoreOutOfOrderSampleWithoutReplacingBaseline(t *testing.T) {
	var tracker rateTracker
	at := time.Unix(1000, 0)
	tracker.update(Stats{NetRx: 100}, at, 1, 0)
	tracker.update(Stats{NetRx: 50000}, at.Add(-time.Second), 1, 0)
	r := tracker.update(Stats{NetRx: 200}, at.Add(time.Second), 1, 0)
	if !r.NetValid || r.NetRx != 100 {
		t.Fatalf("older sample corrupted baseline: %+v", r)
	}
}

type rateEngine struct {
	engine
	data []byte
}

func (f *rateEngine) ContainerStats(context.Context, string, client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
	return client.ContainerStatsResult{Body: io.NopCloser(bytes.NewReader(f.data))}, nil
}

func TestStreamRatesResetForEachNewStream(t *testing.T) {
	var data bytes.Buffer
	at := time.Unix(1000, 0)
	for i := range 2 {
		if err := json.NewEncoder(&data).Encode(container.StatsResponse{Read: at.Add(time.Duration(i) * time.Second), Networks: map[string]container.NetworkStats{"eth0": {RxBytes: uint64(i+1) * 1000, TxBytes: uint64(i+1) * 2000}}, BlkioStats: container.BlkioStats{IoServiceBytesRecursive: []container.BlkioStatEntry{{Op: "read", Value: uint64(i+1) * 3000}, {Op: "write", Value: uint64(i+1) * 4000}}}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := json.NewEncoder(&data).Encode(container.StatsResponse{Read: at.Add(2 * time.Second), BlkioStats: container.BlkioStats{IoServiceBytesRecursive: []container.BlkioStatEntry{{Op: "Total", Value: 99999}}}}); err != nil {
		t.Fatal(err)
	}
	c := &Client{api: &rateEngine{data: data.Bytes()}}
	for range 2 {
		var samples []Rates
		c.streamStats(t.Context(), "id", "web", func(_ Stats, r Rates, _ time.Time) bool { samples = append(samples, r); return true })
		if len(samples) != 3 || samples[0].NetValid || !samples[1].NetValid || samples[1].NetRx != 1000 || samples[1].BlockWrite != 4000 || samples[2].BlockValid {
			t.Fatalf("stream rates/reset incorrect: %+v", samples)
		}
	}
}

func BenchmarkRatesUpdate(b *testing.B) {
	var tracker rateTracker
	at := time.Unix(1000, 0)
	var s Stats
	b.ReportAllocs()
	for b.Loop() {
		at = at.Add(time.Second)
		s.NetRx += 1000
		s.NetTx += 2000
		s.BlockRead += 3000
		s.BlockWrite += 4000
		tracker.update(s, at, 1, 2)
	}
}
