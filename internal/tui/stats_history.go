package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/spar/dpad/internal/docker"
	"github.com/spar/dpad/internal/render"
)

const statsHistorySeconds = 120

type statsBucket struct {
	second                           int64
	count                            uint64
	cpuSum, memSum, cpuPeak, memPeak float64
}

type statsHistory struct {
	buckets                [statsHistorySeconds]statsBucket
	generation             uint64
	lastSample, receivedAt time.Time
	version                uint64
}

// add stores one-second aggregates without shifting slices or allocating.
func (h *statsHistory) add(s docker.Stats, at, received time.Time) bool {
	if at.IsZero() {
		at = received
	}
	if !h.lastSample.IsZero() && !at.After(h.lastSample) {
		return false
	}
	if math.IsNaN(s.CPUPercent) || math.IsInf(s.CPUPercent, 0) || s.CPUPercent < 0 {
		return false
	}
	second := at.Unix()
	b := &h.buckets[(second%statsHistorySeconds+statsHistorySeconds)%statsHistorySeconds]
	if b.count == 0 || b.second != second {
		*b = statsBucket{second: second}
	}
	b.count++
	b.cpuSum += s.CPUPercent
	b.memSum += float64(s.MemUsage)
	b.cpuPeak = max(b.cpuPeak, s.CPUPercent)
	b.memPeak = max(b.memPeak, float64(s.MemUsage))
	h.lastSample, h.receivedAt = at, received
	h.version++
	return true
}

func (h *statsHistory) endSecond(now time.Time) int64 {
	// Age daemon timestamps with the local clock, including remote clock skew.
	return h.lastSample.Unix() + max(int64(now.Sub(h.receivedAt)/time.Second), 0)
}

type statsHistorySummary struct {
	count                            uint64
	cpuAvg, memAvg, cpuPeak, memPeak float64
}

func (h *statsHistory) chart(end int64, width int) (string, string, statsHistorySummary) {
	width = min(max(width, 1), statsHistorySeconds)
	var cpu, mem [statsHistorySeconds]float64
	var present [statsHistorySeconds]bool
	var summary statsHistorySummary
	start := end - statsHistorySeconds + 1
	for _, b := range h.buckets {
		if b.count == 0 || b.second < start || b.second > end {
			continue
		}
		summary.count += b.count
		summary.cpuAvg += b.cpuSum
		summary.memAvg += b.memSum
		summary.cpuPeak = max(summary.cpuPeak, b.cpuPeak)
		summary.memPeak = max(summary.memPeak, b.memPeak)
		col := int((b.second - start) * int64(width) / statsHistorySeconds)
		cpu[col] = max(cpu[col], b.cpuPeak)
		mem[col] = max(mem[col], b.memPeak)
		present[col] = true
	}
	if summary.count > 0 {
		summary.cpuAvg /= float64(summary.count)
		summary.memAvg /= float64(summary.count)
	}
	return statsSparkline(cpu[:width], present[:width], max(100, summary.cpuPeak)), statsSparkline(mem[:width], present[:width], max(1, summary.memPeak)), summary
}

func statsSparkline(values []float64, present []bool, scale float64) string {
	const bars = "▁▂▃▄▅▆▇█"
	var out strings.Builder
	out.Grow(len(values) * 3)
	for i, value := range values {
		if !present[i] {
			out.WriteByte(' ')
		} else if value == 0 {
			out.WriteRune('·')
		} else {
			level := min(int(math.Ceil(value/scale*8))-1, 7)
			out.WriteString(bars[max(level, 0)*3 : max(level, 0)*3+3])
		}
	}
	return out.String()
}

type statsHistoryCache struct {
	history    *statsHistory
	version    uint64
	end        int64
	width      int
	theme      int
	name, text string
}

func (v *StatsView) historyPanel(id, name string, now time.Time) string {
	h := v.history[id]
	if h == nil {
		return ""
	}
	end := h.endSecond(now)
	c := &v.historyCache
	if c.history == h && c.version == h.version && c.end == end && c.width == v.width && c.name == name && c.theme == render.ThemeGeneration() {
		return c.text
	}
	width := min(max(v.width-55, 12), 60)
	cpu, mem, s := h.chart(end, width)
	cpu, mem = render.Sparkline().Render(cpu), render.Sparkline().Render(mem)
	title := render.Bold.Render("History · " + name + " · last 2m")
	var cpuLine, memLine string
	if s.count == 0 {
		cpuLine = "CPU " + cpu + "  avg —  peak —"
		memLine = "MEM " + mem + "  avg —  peak —"
	} else {
		cpuLine = fmt.Sprintf("CPU %s  avg %.1f%%  peak %.1f%%", cpu, s.cpuAvg, s.cpuPeak)
		memLine = fmt.Sprintf("MEM %s  avg %s  peak %s", mem, render.BinSize(uint64(s.memAvg)), render.BinSize(uint64(s.memPeak)))
	}
	legend := fmt.Sprintf("old → new · gaps blank · scale CPU 0–%.0f%% / MEM 0–%s", max(100, s.cpuPeak), render.BinSize(uint64(s.memPeak)))
	lines := []string{title, cpuLine, memLine, render.Dim.Render(legend)}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], v.width, "…")
	}
	text := strings.Join(lines, "\n")
	*c = statsHistoryCache{history: h, version: h.version, end: end, width: v.width, theme: render.ThemeGeneration(), name: name, text: text}
	return text
}

// recent returns the average CPU of each of the n seconds ending at end,
// oldest first, and whether that second had a sample. Samples arrive about a
// second apart, each at its own moment, so a lone second without one takes
// its predecessor's value rather than drawing as a gap.
func (h *statsHistory) recent(end int64, n int) ([]float64, []bool) {
	cpu, present := make([]float64, n), make([]bool, n)
	start := end - int64(n) + 1
	for _, b := range h.buckets {
		if b.count == 0 || b.second < start || b.second > end {
			continue
		}
		cpu[b.second-start] = b.cpuSum / float64(b.count)
		present[b.second-start] = true
	}
	for i := 1; i < n; i++ {
		if !present[i] && present[i-1] && (i == n-1 || present[i+1]) {
			cpu[i], present[i] = cpu[i-1], true
		}
	}
	return cpu, present
}
