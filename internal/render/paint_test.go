package render

import (
	"testing"

	"github.com/spar/dpad/internal/theme"
)

func TestPaintMatchesRender(t *testing.T) {
	t.Cleanup(func() { ApplyTheme(theme.Default()) })
	for _, name := range []string{"default", "mono", "high-contrast"} {
		th, err := theme.Load(name, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		ApplyTheme(th)
		for _, s := range []string{"", "·", "⣿", "▃▅█", " 42.0%"} {
			for _, p := range []float64{0, 49.9, 50, 79.9, 80, 250} {
				if got, want := percentPaint(p).wrap(s), PercentStyle(p).Render(s); got != want {
					t.Errorf("%s: percentPaint(%v).wrap(%q) = %q, want %q", name, p, s, got, want)
				}
			}
			if got, want := usagePaint().dim.wrap(s), Dim.Render(s); got != want {
				t.Errorf("%s: dim.wrap(%q) = %q, want %q", name, s, got, want)
			}
			if got, want := usagePaint().track.wrap(s), fg(Theme().Usage.Track.C()).Render(s); got != want {
				t.Errorf("%s: track.wrap(%q) = %q, want %q", name, s, got, want)
			}
		}
	}
}
