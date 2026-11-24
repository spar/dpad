package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"dpad/internal/docker"
	"dpad/internal/sortx"
)

func TestStatsRatesToggleChangesValuesAndSortingPreservesSelection(t *testing.T) {
	v := NewStatsView(context.Background(), nil, sortx.Net, false)
	v.SetSize(180, 15)
	at := time.Now()
	v.Update(statsBatchMsg{
		{SampledAt: at, Generation: 1, Stats: docker.Stats{ID: "old", Name: "old", NetRx: 1000000, BlockRead: 1000000}, Rates: docker.Rates{NetRx: 100, BlockRead: 100, NetValid: true, BlockValid: true}},
		{SampledAt: at, Generation: 2, Stats: docker.Stats{ID: "busy", Name: "busy", NetRx: 1000, BlockRead: 1000}, Rates: docker.Rates{NetRx: 2000, BlockRead: 4000, NetValid: true, BlockValid: true}},
	})
	rows, _ := v.visible()
	if rows[0].id != "busy" || !strings.Contains(rows[0].cells[6], "/s") || !strings.Contains(v.Status(), "I/O: rates") {
		t.Fatal("default live view did not show/sort throughput")
	}
	v.cur.key = "busy"
	v.HandleKey("t")
	rows, _ = v.visible()
	if rows[0].id != "old" || strings.Contains(rows[0].cells[6], "/s") || !strings.Contains(v.Status(), "I/O: totals") {
		t.Fatal("totals mode did not change values and sorting")
	}
	if id, _, _ := v.Selected(); id != "busy" {
		t.Fatal("toggle changed selected container")
	}
	v.HandleKey("i")
	v.HandleKey("t")
	rows, _ = v.visible()
	if rows[0].id != "busy" || !strings.Contains(v.View(), "DISK READ / WRITE") {
		t.Fatal("disk throughput did not control sort/header")
	}
	if renderColumns := v.columns; renderColumns[6].Header != "NET RX / TX" {
		t.Fatal("rate header missing")
	}
}

func TestStatsRatesResetAndCleanup(t *testing.T) {
	v := NewStatsView(context.Background(), nil, sortx.Net, false)
	at := time.Now()
	v.Update(statsBatchMsg{{SampledAt: at, Generation: 1, Stats: docker.Stats{ID: "web", Name: "web"}, Rates: docker.Rates{NetRx: 1000, NetValid: true}}})
	v.Update(statsBatchMsg{{SampledAt: at.Add(time.Second), Generation: 2, Stats: docker.Stats{ID: "web", Name: "web"}}})
	if v.rates["web"].NetValid {
		t.Fatal("restarted stream inherited previous throughput")
	}
	v.Update(statsBatchMsg{{SampledAt: at.Add(2 * time.Second), Generation: 1, Stats: docker.Stats{ID: "web", Name: "web"}, Rates: docker.Rates{NetRx: 500000, NetValid: true}}})
	if v.rates["web"].NetValid {
		t.Fatal("stale stream replaced new throughput")
	}
	v.Update(statsBatchMsg{{Generation: 2, Gone: true, Stats: docker.Stats{ID: "web"}}})
	if len(v.rates) != 0 {
		t.Fatal("stopped container retained rate state")
	}
	v.Update(statsStartedMsg{})
	if len(v.rates) != 0 {
		t.Fatal("new watch retained rate state")
	}
}

func TestStatsInvalidTimestampClearsRateEvenWhenHistoryRejectsSample(t *testing.T) {
	v := NewStatsView(context.Background(), nil, sortx.Net, false)
	at := time.Now()
	v.Update(statsBatchMsg{{SampledAt: at, Generation: 1, Stats: docker.Stats{ID: "web", Name: "web"}, Rates: docker.Rates{NetRx: 1000, NetValid: true}}})
	v.Update(statsBatchMsg{{SampledAt: at, Generation: 1, Stats: docker.Stats{ID: "web", Name: "web"}}})
	if v.rates["web"].NetValid {
		t.Fatal("invalid timestamp left old throughput displayed as current")
	}
}

func TestAppAndStandaloneStatsExposeRatesToggle(t *testing.T) {
	a := newTestApp()
	press(a, "3")
	if !strings.Contains(ansi.Strip(a.footer()), "t totals") {
		t.Fatal("TUI toggle not discoverable")
	}
	press(a, "t")
	if !a.tabs[2].(*StatsView).totals || !strings.Contains(ansi.Strip(a.footer()), "t rates") {
		t.Fatal("TUI toggle not routed to Stats")
	}
	v := NewStatsView(context.Background(), nil, sortx.Net, false)
	v.noHighlight = true
	v.SetSize(180, 12)
	p := statsProgram{v: v}
	v.Update(statsBatchMsg{{Stats: docker.Stats{ID: "web", Name: "web"}, Rates: docker.Rates{NetRx: 1000, NetValid: true}}})
	if !strings.Contains(p.View().Content, "t rates/totals") {
		t.Fatal("standalone toggle not discoverable")
	}
	p.Update(key("t"))
	if !v.totals || len(v.history) != 0 {
		t.Fatal("standalone toggle changed history behavior")
	}
}
