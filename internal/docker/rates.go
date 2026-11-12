package docker

import (
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
)

type Rates struct {
	NetRx, NetTx, BlockRead, BlockWrite float64
	NetValid, BlockValid                bool
}

const maxRateInterval = 10 * time.Second

func blockRateSources(entries []container.BlkioStatEntry) int {
	count := 0
	for _, e := range entries {
		if strings.EqualFold(e.Op, "read") || strings.EqualFold(e.Op, "write") {
			count++
		}
	}
	return count
}

type rateSample struct {
	at                     time.Time
	rx, tx, read, write    uint64
	networks, blockEntries int
}

type rateTracker struct{ previous rateSample }

func (t *rateTracker) update(s Stats, at time.Time, networks, blockEntries int) Rates {
	previous := t.previous
	if at.IsZero() {
		t.previous = rateSample{}
		return Rates{}
	}
	if !previous.at.IsZero() && !at.After(previous.at) {
		return Rates{}
	}
	t.previous = rateSample{at: at, rx: s.NetRx, tx: s.NetTx, read: s.BlockRead, write: s.BlockWrite, networks: networks, blockEntries: blockEntries}
	duration := at.Sub(previous.at)
	if previous.at.IsZero() || duration > maxRateInterval {
		return Rates{}
	}
	seconds := duration.Seconds()
	var r Rates
	if networks > 0 && networks == previous.networks && s.NetRx >= previous.rx && s.NetTx >= previous.tx {
		r.NetValid = true
		r.NetRx = float64(s.NetRx-previous.rx) / seconds
		r.NetTx = float64(s.NetTx-previous.tx) / seconds
	}
	if blockEntries > 0 && blockEntries == previous.blockEntries && s.BlockRead >= previous.read && s.BlockWrite >= previous.write {
		r.BlockValid = true
		r.BlockRead = float64(s.BlockRead-previous.read) / seconds
		r.BlockWrite = float64(s.BlockWrite-previous.write) / seconds
	}
	return r
}
