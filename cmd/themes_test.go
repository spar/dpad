package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dpad/internal/config"
	"dpad/internal/render"
	"dpad/internal/theme"
)

func TestSetupThemePrecedence(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte("theme: amber\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DPAD_CONFIG", cfg)
	t.Setenv("DPAD_THEME", "")
	t.Cleanup(func() { render.ApplyTheme(theme.Default()) })

	for _, tt := range []struct{ flag, env, want string }{
		{"", "", "amber"},
		{"", "nord", "nord"},
		{"mono", "nord", "mono"},
	} {
		t.Setenv("DPAD_THEME", tt.env)
		cfg, _ := config.Load()
		if err := setupTheme(tt.flag, cfg.Theme); err != nil {
			t.Fatal(err)
		}
		if got := render.Theme().Name; got != tt.want {
			t.Errorf("flag %q, env %q: theme %s, want %s", tt.flag, tt.env, got, tt.want)
		}
	}
	render.ApplyTheme(theme.Default())
	if err := setupTheme("nope", ""); err == nil || !strings.Contains(err.Error(), "unknown theme") || render.Theme().Name != "default" {
		t.Errorf("unknown theme: err %v, theme %s", err, render.Theme().Name)
	}
}
