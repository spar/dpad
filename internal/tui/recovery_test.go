package tui

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"dpad/internal/docker"
	"dpad/internal/sortx"
)

func TestStatsUnavailableRecoveryPreservesView(t *testing.T) {
	v := NewStatsView(t.Context(), &fakeClient{}, sortx.CPU, true)
	v.SetSize(150, 25)
	v.SetFilter("web")
	v.SetTotals(true)
	v.cur.key = "b"
	old := make(chan docker.StatsUpdate)
	v.Update(statsStartedMsg{ch: old})
	at := time.Now()
	sample := docker.StatsUpdate{Stats: docker.Stats{ID: "b", Name: "web", CPUPercent: 80, MemUsage: 1024}, Generation: 1, SampledAt: at}
	v.Update(statsBatchMsg{sample})
	v.Update(statsBatchMsg{{Stats: sample.Stats, Generation: 1, Unavailable: true, Err: errors.New("lost")}})
	if v.history["b"] != nil || v.rates["b"] != (docker.Rates{}) {
		t.Fatal("old chart/rates retained")
	}
	rows, _ := v.visible()
	if ansi.Strip(rows[0].cells[1]) != "—" || !strings.Contains(v.Status(), "CPU — · MEM —") {
		t.Fatal(v.Status(), rows)
	}
	v.Update(statsClosedMsg{old})
	if v.watching {
		t.Fatal("closed watch still active")
	}
	v.requested = true
	if v.retryNow() == nil || v.retryNow() != nil {
		t.Fatal("manual retry must start exactly one watch")
	}
	if v.Update(statsRetryMsg{generation: 0}) != nil {
		t.Fatal("obsolete timer started a watch")
	}
	fresh := make(chan docker.StatsUpdate)
	v.Update(statsStartedMsg{ch: fresh, generation: v.watchGen})
	v.Update(statsSourceBatchMsg{ch: old, batch: statsBatchMsg{sample}})
	if len(v.data) != 0 {
		t.Fatal("old watcher updated new session")
	}
	sample.Generation = 2
	sample.SampledAt = at.Add(time.Second)
	v.Update(statsSourceBatchMsg{ch: fresh, batch: statsBatchMsg{sample}})
	if v.health["b"].err != nil || v.history["b"] == nil {
		t.Fatal("fresh sample did not recover")
	}
	if id, _, ok := v.Selected(); !ok || id != "b" || v.filter != "web" || v.key != sortx.CPU || !v.reverse || !v.totals {
		t.Fatal("view state lost")
	}
	v.Update(statsBatchMsg{{Stats: sample.Stats, Generation: 1, Unavailable: true}})
	if v.health["b"].err != nil {
		t.Fatal("old generation invalidated recovered data")
	}
}

func TestStatsFrozenTimestampExpires(t *testing.T) {
	v := NewStatsView(t.Context(), nil, sortx.Name, false)
	sample := docker.StatsUpdate{Stats: docker.Stats{ID: "a", Name: "web"}, Generation: 1, SampledAt: time.Now()}
	v.Update(statsBatchMsg{sample})
	h := v.health["a"]
	h.received = time.Now().Add(-statsStaleAfter - time.Second)
	v.health["a"] = h
	v.Update(statsBatchMsg{sample})
	if !errors.Is(v.health["a"].err, errStatsStale) || v.history["a"] != nil {
		t.Fatal("frozen source kept stale data live")
	}
	sample.SampledAt = sample.SampledAt.Add(time.Second)
	v.Update(statsBatchMsg{sample})
	if v.health["a"].err != nil {
		t.Fatal("fresh timestamp did not recover")
	}
}

func TestStatsStartupRetryAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	v := NewStatsView(ctx, &fakeClient{}, sortx.Name, false)
	if v.Update(refreshMsg{}) != nil {
		t.Fatal("refresh started unopened Stats")
	}
	v.Focus()
	if v.Update(statsErrMsg{generation: v.watchGen, err: errors.New("offline")}) == nil || v.watching || v.retryDelay != 2*time.Second {
		t.Fatal("startup failure did not schedule retry")
	}
	if !strings.Contains(v.View(), "reconnecting") {
		t.Fatal(v.View())
	}
	if v.Update(statsRetryMsg{generation: v.watchGen}) == nil {
		t.Fatal("scheduled retry did not start")
	}
	cancel()
	if v.Update(statsErrMsg{generation: v.watchGen, err: errors.New("offline")}) != nil {
		t.Fatal("retry after cancellation")
	}
}

func TestDaemonProbeRecoveryAndStaleResults(t *testing.T) {
	a := &App{ctx: t.Context(), client: &fakeClient{}, width: 200, daemon: daemonConnection{label: "test-daemon"}}
	if !strings.Contains(a.connectionBar(), "connecting") {
		t.Fatal(a.connectionBar())
	}
	cmd := a.probeDaemon()
	if cmd == nil || a.probeDaemon() != nil {
		t.Fatal("duplicate probe")
	}
	msg := cmd().(daemonProbeMsg)
	a.applyDaemonProbe(msg)
	if !a.daemon.connected || !strings.Contains(a.connectionBar(), "test-daemon") {
		t.Fatal(a.connectionBar())
	}
	a.probeDaemon()
	a.applyDaemonProbe(daemonProbeMsg{generation: msg.generation, err: errors.New("stale")})
	if !a.daemon.connected || !a.daemon.inFlight {
		t.Fatal("old probe affected current connection")
	}
	gen := a.daemon.generation
	a.applyDaemonProbe(daemonProbeMsg{generation: gen, err: errors.New("offline")})
	if a.daemon.connected || !strings.Contains(a.connectionBar(), "reconnecting") {
		t.Fatal(a.connectionBar())
	}
	for range 10 {
		a.applyDaemonProbe(daemonProbeMsg{generation: gen, err: errors.New("offline")})
	}
	if a.daemon.delay != 5*time.Second {
		t.Fatal(a.daemon.delay)
	}
	if a.applyDaemonProbe(daemonProbeMsg{generation: gen}) == nil || !a.daemon.connected || a.daemon.delay != time.Second {
		t.Fatal("did not recover")
	}
}

func TestConnectionErrorRedactsCredentials(t *testing.T) {
	for _, err := range []error{
		&url.Error{Op: "Get", URL: "https://user:password@host?secret=token", Err: errors.New("connection refused")},
		errors.New("dial tcp://user:password@host:1234: connection refused\nretrying"),
	} {
		got := connectionError(err)
		if strings.Contains(got, "password") || strings.Contains(got, "token") || strings.Contains(got, "\n") {
			t.Fatal(got)
		}
	}
}
