package docker

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// writeContext adds a docker context named name with the given endpoint.
func writeContext(t *testing.T, cfgDir, name, host string) {
	t.Helper()
	sum := sha256.Sum256([]byte(name))
	dir := filepath.Join(cfgDir, "contexts", "meta", hex.EncodeToString(sum[:]))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{"Name":"` + name + `","Endpoints":{"docker":{"Host":"` + host + `"}}}`
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestContextHost(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("DOCKER_CONFIG", cfg)
	writeContext(t, cfg, "colima", "unix:///colima.sock")
	writeContext(t, cfg, "remote", "ssh://prod")

	tests := []struct {
		name, current, env, want string
	}{
		{"no config", "", "", ""},
		{"current context", "colima", "", "unix:///colima.sock"},
		{"DOCKER_CONTEXT wins", "colima", "remote", "ssh://prod"},
		{"default context", "default", "", ""},
		{"unknown context", "missing", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Remove(filepath.Join(cfg, "config.json"))
			if tt.current != "" {
				cfgJSON := `{"currentContext":"` + tt.current + `"}`
				if err := os.WriteFile(filepath.Join(cfg, "config.json"), []byte(cfgJSON), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("DOCKER_CONTEXT", tt.env)
			if got := contextHost(); got != tt.want {
				t.Errorf("contextHost() = %q, want %q", got, tt.want)
			}
		})
	}
}
