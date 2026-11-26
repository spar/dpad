package theme

import (
	"strings"
	"testing"
)

func FuzzParseColor(f *testing.F) {
	for _, s := range []string{"default", "#00ff41", "#0f4", "#GGGGGG", "196", "256", "-1", "red", "bright-blue", "Grey", " cyan ", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		c, err := ParseColor(s)
		if err != nil {
			return
		}
		if c.Value != s {
			t.Fatalf("ParseColor(%q).Value = %q", s, c.Value)
		}
		if c.C() == nil && !strings.EqualFold(strings.TrimSpace(s), "default") {
			t.Fatalf("ParseColor(%q) accepted a value with no color", s)
		}
	})
}
