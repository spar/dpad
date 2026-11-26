package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestUsageLevelShowsSmallLoadsAndCapsMultiCore(t *testing.T) {
	for _, tc := range []struct {
		p    float64
		want int
	}{{0, 0}, {0.4, 0}, {1, 1}, {4, 2}, {25, 4}, {100, 8}, {250, 8}} {
		if got := usageLevel(tc.p, 8); got != tc.want {
			t.Errorf("usageLevel(%v) = %d, want %d", tc.p, got, tc.want)
		}
	}
}

func TestCPUGraphStylesKeepTheirWidth(t *testing.T) {
	samples, present := make([]float64, GraphSamples), make([]bool, GraphSamples)
	for i := range samples {
		samples[i], present[i] = float64(i*4), i%7 != 0
	}
	present[len(present)-1] = true
	for _, style := range []string{GraphBraille, GraphBars, GraphMeter} {
		got := ansi.Strip(CPUGraph(style, samples, present))
		if w := ansi.StringWidth(got); w != GraphWidth+7 {
			t.Errorf("%s: width %d, want %d: %q", style, w, GraphWidth+7, got)
		}
		if !strings.HasSuffix(got, "92.0%") {
			t.Errorf("%s: latest value missing: %q", style, got)
		}
	}
}

func TestCPUGraphDrawsIdleGapsAndShortHistory(t *testing.T) {
	if got := CPUGraph(GraphBars, []float64{5}, []bool{false}); got != "" {
		t.Errorf("no samples should draw nothing, got %q", got)
	}
	got := ansi.Strip(CPUGraph(GraphBars, []float64{0, 0, 30}, []bool{true, false, true}))
	if want := strings.Repeat(" ", GraphWidth-3) + "· ▅  30.0%"; got != want {
		t.Errorf("bars = %q, want %q", got, want)
	}
	got = ansi.Strip(CPUGraph(GraphBraille, []float64{0, 100, 0, 0}, []bool{true, true, true, true}))
	if want := strings.Repeat(" ", GraphWidth-2) + "⢸⣀   0.0%"; got != want {
		t.Errorf("braille = %q, want %q", got, want)
	}
}

func TestCompactSize(t *testing.T) {
	for b, want := range map[uint64]string{
		316 << 10: "316K", 55 << 20: "55M", 999 << 20: "999M", 5939 << 20: "5.8G", 120 << 30: "120G",
	} {
		if got := CompactSize(b); got != want {
			t.Errorf("CompactSize(%d) = %q, want %q", b, got, want)
		}
	}
}
