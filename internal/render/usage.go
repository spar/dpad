package render

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

// CPU graph styles for the Containers tab, as named in config.yaml.
const (
	GraphBraille = "braille"
	GraphBars    = "bars"
	GraphMeter   = "meter"
	GraphOff     = "off"
)

// GraphStyles lists the styles in the order the settings screen cycles them.
var GraphStyles = []string{GraphBraille, GraphBars, GraphMeter, GraphOff}

// GraphWidth is the width of a CPU graph in cells. A braille cell holds two
// samples, so CPUGraph takes 2*GraphWidth samples.
const GraphWidth = 12

// GraphSamples is how many one-second samples CPUGraph reads.
const GraphSamples = 2 * GraphWidth

// idleCPU is the usage below which a sample draws as idle, so background
// noise of a few tenths of a percent doesn't look like work.
const idleCPU = 0.5

// recentBars are the bars drawn in usage colors; older ones are dimmed so the
// right edge reads as now.
const recentBars = 3

// usageLevel maps a CPU percentage to 0..levels on a square-root scale, so a
// few percent is visible next to 40%. Above 100% (several cores) is full.
func usageLevel(p float64, levels int) int {
	if p < idleCPU {
		return 0
	}
	return min(levels, max(1, int(math.Ceil(math.Sqrt(min(p, 100)/100)*float64(levels)))))
}

// CPUGraph draws CPU history in style, followed by the latest value. samples
// are one per second, oldest first, with present false where none arrived.
// It returns "" when no sample is present.
func CPUGraph(style string, samples []float64, present []bool) string {
	latest, ok := -1.0, false
	for i, p := range slices.Backward(samples) {
		if present[i] {
			latest, ok = p, true
			break
		}
	}
	if !ok {
		return ""
	}
	var graph string
	switch style {
	case GraphBars:
		graph = barsGraph(samples, present)
	case GraphMeter:
		graph = meterGraph(samples, present, latest)
	default:
		graph = brailleGraph(samples, present)
	}
	return graph + " " + PercentStyle(latest).Render(fmt.Sprintf("%5.1f%%", latest))
}

func last[T any](s []T, n int) []T { return s[max(len(s)-n, 0):] }

func barsGraph(samples []float64, present []bool) string {
	const bars = "▁▂▃▄▅▆▇█"
	samples, present = last(samples, GraphWidth), last(present, GraphWidth)
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", GraphWidth-len(samples)))
	for i, p := range samples {
		level := usageLevel(p, 8)
		switch {
		case !present[i]:
			b.WriteByte(' ')
		case level == 0:
			b.WriteString(Dim.Render("·"))
		case i >= len(samples)-recentBars:
			b.WriteString(PercentStyle(p).Render(bars[(level-1)*3 : level*3]))
		default:
			b.WriteString(Dim.Render(bars[(level-1)*3 : level*3]))
		}
	}
	return b.String()
}

// brailleGraph fills each cell's two dot columns from the bottom, one column
// per sample, giving four levels and twice the history of the bar styles.
func brailleGraph(samples []float64, present []bool) string {
	left := [4]rune{0x40, 0x04, 0x02, 0x01}
	right := [4]rune{0x80, 0x20, 0x10, 0x08}
	samples, present = last(samples, GraphSamples), last(present, GraphSamples)
	cells := (len(samples) + 1) / 2
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", GraphWidth-cells))
	for c := range cells {
		var dots rune
		peak, seen := 0.0, false
		for j, cols := range [2]*[4]rune{&left, &right} {
			k := len(samples) - 2*(cells-c) + j
			if k < 0 || !present[k] {
				continue
			}
			seen = true
			peak = max(peak, samples[k])
			for l := range usageLevel(samples[k], 4) {
				dots |= cols[l]
			}
		}
		switch {
		case !seen:
			b.WriteByte(' ')
		case dots == 0:
			b.WriteString(Dim.Render("⣀"))
		default:
			b.WriteString(PercentStyle(peak).Render(string(0x2800 + dots)))
		}
	}
	return b.String()
}

// meterGraph shows the latest value as a bar and the direction it is moving
// against the few seconds before it.
func meterGraph(samples []float64, present []bool, latest float64) string {
	const width = GraphWidth - 3
	fill := math.Sqrt(min(latest, 100)/100) * width
	if latest < idleCPU {
		fill = 0
	}
	full := int(fill)
	partial := ""
	if full < width {
		if eighths := int((fill - float64(full)) * 8); eighths > 0 {
			partial = string([]rune(" ▏▎▍▌▋▊▉")[eighths])
		}
	}
	used := full + len([]rune(partial))
	track := fg(active.Usage.Track.C())
	bar := track.Render("▕") + PercentStyle(latest).Render(strings.Repeat("█", full)+partial) + track.Render(strings.Repeat("░", width-used)+"▏")

	var sum float64
	n := 0
	for i := len(samples) - 2; i >= 0 && n < 5; i-- {
		if present[i] {
			sum, n = sum+samples[i], n+1
		}
	}
	arrow := Dim.Render("→")
	if n > 0 {
		switch before := sum / float64(n); {
		case latest > before*1.3+idleCPU:
			arrow = PercentStyle(latest).Render("↗")
		case latest < before*0.7-idleCPU:
			arrow = Dim.Render("↘")
		}
	}
	return bar + arrow
}

// CompactSize formats a byte count in at most five cells, such as 331M or
// 5.8G, for narrow columns.
func CompactSize(b uint64) string {
	const k = 1024
	switch {
	case b < k*k:
		return fmt.Sprintf("%dK", b/k)
	case b < 1000*k*k:
		return fmt.Sprintf("%dM", b/(k*k))
	case b < 100*k*k*k:
		return fmt.Sprintf("%.1fG", float64(b)/(k*k*k))
	}
	return fmt.Sprintf("%dG", b/(k*k*k))
}
