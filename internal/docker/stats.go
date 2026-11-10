package docker

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

// Stats is a computed resource-usage sample for one container.
type Stats struct {
	ID         string
	Name       string
	CPUPercent float64
	MemUsage   uint64
	MemLimit   uint64
	MemPercent float64
	NetRx      uint64
	NetTx      uint64
	BlockRead  uint64
	BlockWrite uint64
	PIDs       uint64
}

// StatsUpdate is emitted by WatchStats: either a fresh sample or notice that
// the container's stream has ended (Gone).
type StatsUpdate struct {
	Stats Stats
	Gone  bool
}

// Calc derives display values from a raw stats response, using the same
// formulas as `docker stats`.
func Calc(id, name string, s *container.StatsResponse) Stats {
	st := Stats{ID: id, Name: name, PIDs: s.PidsStats.Current}

	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemUsage) - float64(s.PreCPUStats.SystemUsage)
	cpus := float64(s.CPUStats.OnlineCPUs)
	if cpus == 0 {
		cpus = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
	}
	// A stream's first record has no previous sample (PreCPUStats is zero), so
	// its "delta" would be the container's lifetime; report 0 until there is one.
	if s.PreCPUStats.SystemUsage > 0 && cpuDelta > 0 && sysDelta > 0 {
		st.CPUPercent = cpuDelta / sysDelta * cpus * 100
	}

	st.MemUsage = memUsageNoCache(s.MemoryStats)
	st.MemLimit = s.MemoryStats.Limit
	if st.MemLimit != 0 {
		st.MemPercent = float64(st.MemUsage) / float64(st.MemLimit) * 100
	}

	for _, n := range s.Networks {
		st.NetRx += n.RxBytes
		st.NetTx += n.TxBytes
	}
	for _, e := range s.BlkioStats.IoServiceBytesRecursive {
		switch strings.ToLower(e.Op) {
		case "read":
			st.BlockRead += e.Value
		case "write":
			st.BlockWrite += e.Value
		}
	}
	return st
}

// memUsageNoCache subtracts page cache like the docker CLI does
// (total_inactive_file on cgroup v1, inactive_file on cgroup v2).
func memUsageNoCache(m container.MemoryStats) uint64 {
	if v, ok := m.Stats["total_inactive_file"]; ok && v < m.Usage {
		return m.Usage - v
	}
	if v := m.Stats["inactive_file"]; v < m.Usage {
		return m.Usage - v
	}
	return m.Usage
}

// maxSnapshotRequests caps concurrent stats requests in StatsSnapshot, so a
// host with hundreds of containers doesn't get hundreds at once.
const maxSnapshotRequests = 16

// StatsSnapshot collects one sample (with a previous sample for CPU deltas)
// from every running container, a bounded number at a time.
func (c *Client) StatsSnapshot(ctx context.Context) ([]Stats, error) {
	list, err := c.Containers(ctx, false)
	if err != nil {
		return nil, err
	}
	out := make([]Stats, len(list))
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxSnapshotRequests)
	for i, ctr := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			name := ContainerName(ctr.Names)
			out[i] = Stats{ID: ctr.ID, Name: name}
			res, err := c.api.ContainerStats(ctx, ctr.ID, client.ContainerStatsOptions{IncludePreviousSample: true})
			if err != nil {
				return
			}
			defer res.Body.Close()
			var raw container.StatsResponse
			if json.NewDecoder(res.Body).Decode(&raw) == nil {
				out[i] = Calc(ctr.ID, name, &raw)
			}
		}()
	}
	wg.Wait()
	return out, nil
}

// WatchStats streams stats for all running containers, following containers as
// they start and stop. The channel is closed when ctx is cancelled.
func (c *Client) WatchStats(ctx context.Context) (<-chan StatsUpdate, error) {
	out := make(chan StatsUpdate, 64)
	var (
		mu sync.Mutex
		// streams maps a container ID to its live stream. A restart replaces
		// the entry while the old stream may still be shutting down, so each
		// stream compares against its own handle before touching the entry.
		streams = map[string]*statsStream{}
		wg      sync.WaitGroup
	)

	start := func(id, name string) {
		mu.Lock()
		defer mu.Unlock()
		if _, ok := streams[id]; ok {
			return
		}
		cctx, cancel := context.WithCancel(ctx)
		h := &statsStream{cancel}
		streams[id] = h
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.streamStats(cctx, id, name, out)
			cancel()
			mu.Lock()
			defer mu.Unlock()
			if streams[id] == h {
				delete(streams, id)
			}
			if streams[id] != nil {
				return // restarted: the new stream owns the row
			}
			// Sent under mu so a new stream for id can't emit a sample first.
			select {
			case out <- StatsUpdate{Stats: Stats{ID: id, Name: name}, Gone: true}:
			case <-ctx.Done():
			}
		}()
	}
	streamIDs := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Collect(maps.Keys(streams))
	}
	stop := func(id string) {
		mu.Lock()
		defer mu.Unlock()
		if h, ok := streams[id]; ok {
			h.cancel()
			delete(streams, id)
		}
	}

	// Start streams for running containers and stop streams for ones that
	// are gone; runs on every (re)subscription to events.
	resync := func() error {
		list, err := c.Containers(ctx, false)
		if err != nil {
			return err
		}
		running := make(map[string]bool, len(list))
		for _, ctr := range list {
			running[ctr.ID] = true
			start(ctr.ID, ContainerName(ctr.Names))
		}
		for _, id := range streamIDs() {
			if !running[id] {
				stop(id)
			}
		}
		return nil
	}
	loop, err := c.followEvents(ctx, []events.Action{events.ActionStart, events.ActionDie}, resync, func(m events.Message) {
		switch m.Action {
		case events.ActionStart:
			start(m.Actor.ID, m.Actor.Attributes["name"])
		case events.ActionDie:
			stop(m.Actor.ID)
		}
	})
	if err != nil {
		return nil, err
	}
	go func() {
		loop()
		wg.Wait()
		close(out)
	}()
	return out, nil
}

// statsStream is one container's live stats stream in WatchStats.
type statsStream struct{ cancel context.CancelFunc }

func (c *Client) streamStats(ctx context.Context, id, name string, out chan<- StatsUpdate) {
	res, err := c.api.ContainerStats(ctx, id, client.ContainerStatsOptions{Stream: true})
	if err != nil {
		return
	}
	defer res.Body.Close()
	dec := json.NewDecoder(res.Body)
	for {
		var raw container.StatsResponse
		if err := dec.Decode(&raw); err != nil {
			return
		}
		select {
		case out <- StatsUpdate{Stats: Calc(id, name, &raw)}:
		case <-ctx.Done():
			return
		}
	}
}

// ContainerName returns the primary name without the leading slash.
func ContainerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}
