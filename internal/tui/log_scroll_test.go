package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/spar/dpad/internal/docker"
)

func TestLogScrollWrapsAndPans(t *testing.T) {
	s := logScroll{width: 4, height: 3}
	s.SetContentLines([]string{"ab", "0123456789", "yz"})
	if got := s.View(); got != "ab\n0123\nyz" {
		t.Fatalf("unwrapped view = %q", got)
	}
	s.Update(key("right"))
	if got := s.View(); s.xOffset != 6 || got != "\n6789\n" {
		t.Fatalf("panned view = %q at x=%d", got, s.xOffset)
	}
	s.SetXOffset(0)
	s.SoftWrap = true
	if n := s.TotalLineCount(); n != 5 {
		t.Fatalf("wrapped rows = %d, want 5", n)
	}
	s.SetYOffset(2)
	if got := s.View(); got != "4567\n89\nyz" || !s.AtBottom() {
		t.Fatalf("wrapped view from row 2 = %q", got)
	}
	s.SetXOffset(3)
	if s.xOffset != 0 {
		t.Fatal("wrapped lines panned")
	}
}

func TestLogsStyleOnlyDrawnLines(t *testing.T) {
	v := newTestLogsView(5)
	styled := 0
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = fmt.Sprintf("INFO line %d", i)
	}
	addLogLines(v, lines...)
	widths := v.vp.widths
	v.vp.SetLines(widths, func(i int) string {
		styled++
		return v.text(v.lines[i].line)
	})
	if out := v.View(); styled != 5 || !strings.Contains(out, "line 199") {
		t.Fatalf("styled %d lines for a 5 row pane: %q", styled, out)
	}
}

func benchmarkLogs(b *testing.B) *LogsView {
	v := newTestLogsView(40)
	lines := make([]docker.LogLine, maxLogLines)
	for i := range lines {
		lines[i] = docker.LogLine{Time: time.Unix(int64(i), 0), Text: fmt.Sprintf(`level=info msg="GET /api/v1/items/%d" status=200 duration=12ms remote=10.0.0.%d`, i, i%255)}
	}
	v.Update(logBatchMsg{ch: v.ch, lines: lines})
	return v
}

func BenchmarkLogsBatchAndView(b *testing.B) {
	for _, wrap := range []bool{false, true} {
		b.Run(fmt.Sprintf("wrap=%t", wrap), func(b *testing.B) {
			v := benchmarkLogs(b)
			v.vp.SoftWrap = wrap
			v.vp.SetWidth(60)
			batch := []docker.LogLine{{Text: `level=warn msg="slow request" duration=900ms`}}
			b.ReportAllocs()
			for b.Loop() {
				v.Update(logBatchMsg{ch: v.ch, lines: batch})
				v.View()
			}
		})
	}
}

func BenchmarkLogsFilterKeystroke(b *testing.B) {
	v := benchmarkLogs(b)
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		v.setQuery([]string{"s", "st", "sta", "stat"}[i%4])
		v.View()
		i++
	}
}
