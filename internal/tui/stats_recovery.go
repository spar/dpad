package tui

import (
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/spar/dpad/internal/docker"
)

const statsStaleAfter = 5 * time.Second

var errStatsStale = errors.New("stats samples stopped arriving")

type statsHealth struct {
	generation         uint64
	received, sourceAt time.Time
	err                error
}
type statsRetryMsg struct{ generation uint64 }
type statsClosedMsg struct{ ch <-chan docker.StatsUpdate }
type statsTickMsg struct{ ch <-chan docker.StatsUpdate }
type statsSourceBatchMsg struct {
	ch    <-chan docker.StatsUpdate
	batch statsBatchMsg
}

func (v *StatsView) retryWatch() tea.Cmd {
	v.watching = false
	v.ch = nil
	delay := v.retryDelay
	if delay == 0 {
		delay = time.Second
	}
	v.retryDelay = min(delay*2, 5*time.Second)
	gen := v.watchGen
	return tea.Tick(delay, func(time.Time) tea.Msg { return statsRetryMsg{generation: gen} })
}

func (v *StatsView) markUnavailable(id string, health statsHealth, err error) {
	health.err = err
	v.health[id] = health
	delete(v.history, id)
	delete(v.rates, id)
	v.historyCache = statsHistoryCache{}
}

func (v *StatsView) expireSamples(now time.Time) {
	changed := false
	for id, h := range v.health {
		if h.err == nil && !h.received.IsZero() && now.Sub(h.received) >= statsStaleAfter {
			v.markUnavailable(id, h, errStatsStale)
			v.rowCache = nil
			changed = true
		}
	}
	if changed && v.grouped {
		v.rebuildItems()
	}
}

func (v *StatsView) retryNow() tea.Cmd {
	if v.requested && !v.watching && v.ctx.Err() == nil {
		return v.Focus()
	}
	return nil
}
