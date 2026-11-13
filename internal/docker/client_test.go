package docker

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moby/moby/client"
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

func TestContextEndpoint(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("DOCKER_CONFIG", cfg)
	writeContext(t, cfg, "colima", "unix:///colima.sock")
	writeContext(t, cfg, "remote", "ssh://prod")

	tests := []struct {
		name, current, env, wantName, wantHost string
	}{
		{"no config", "", "", "default", ""},
		{"current context", "colima", "", "colima", "unix:///colima.sock"},
		{"DOCKER_CONTEXT wins", "colima", "remote", "remote", "ssh://prod"},
		{"default context", "default", "", "default", ""},
		{"explicit default context", "colima", "default", "default", ""},
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
			name, host, err := contextEndpoint()
			if err != nil || name != tt.wantName || host != tt.wantHost {
				t.Errorf("contextEndpoint() = %q, %q, %v; want %q, %q, nil", name, host, err, tt.wantName, tt.wantHost)
			}
		})
	}
}

func TestNewDaemonSelection(t *testing.T) {
	tests := []struct {
		name, context, host, want string
	}{
		{"stored context", "", "", "stored @ tcp://127.0.0.1:12345"},
		{"environment context", "other", "", "other @ tcp://127.0.0.1:23456"},
		{"host overrides stored context", "", "tcp://127.0.0.1:34567", "tcp://127.0.0.1:34567"},
		{"host overrides environment context", "other", "tcp://127.0.0.1:34567", "tcp://127.0.0.1:34567"},
		{"host overrides missing context", "missing", "tcp://127.0.0.1:34567", "tcp://127.0.0.1:34567"},
		{"explicit default context", "default", "", "default @ " + client.DefaultDockerHost},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := t.TempDir()
			t.Setenv("DOCKER_CONFIG", cfg)
			t.Setenv("DOCKER_CONTEXT", tt.context)
			t.Setenv("DOCKER_HOST", tt.host)
			t.Setenv("DOCKER_TLS_VERIFY", "")
			t.Setenv("DOCKER_CERT_PATH", "")
			writeContext(t, cfg, "stored", "tcp://127.0.0.1:12345")
			writeContext(t, cfg, "other", "tcp://127.0.0.1:23456")
			if err := os.WriteFile(filepath.Join(cfg, "config.json"), []byte(`{"currentContext":"stored"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			c, err := New()
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if got := c.ConnectionLabel(); got != tt.want {
				t.Errorf("ConnectionLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewRejectsInvalidContext(t *testing.T) {
	tests := []struct {
		name, config, metadata, wantErr string
	}{
		{"missing context", `{"currentContext":"broken"}`, "", `read docker context "broken"`},
		{"malformed context", `{"currentContext":"broken"}`, `{`, `read docker context "broken"`},
		{"missing endpoint", `{"currentContext":"broken"}`, `{"Endpoints":{}}`, `has no docker endpoint`},
		{"empty endpoint", `{"currentContext":"broken"}`, `{"Endpoints":{"docker":{"Host":""}}}`, `has no docker endpoint`},
		{"malformed config", `{`, "", `read docker config`},
	}
	for _, tt := range tests {
		for _, envContext := range []bool{false, true} {
			name := tt.name
			if envContext {
				if tt.name == "malformed config" {
					continue
				}
				name += " via DOCKER_CONTEXT"
			}
			t.Run(name, func(t *testing.T) {
				cfg := t.TempDir()
				t.Setenv("DOCKER_CONFIG", cfg)
				t.Setenv("DOCKER_HOST", "")
				t.Setenv("DOCKER_CONTEXT", "")
				if envContext {
					t.Setenv("DOCKER_CONTEXT", "broken")
				}
				if err := os.WriteFile(filepath.Join(cfg, "config.json"), []byte(tt.config), 0o600); err != nil {
					t.Fatal(err)
				}
				if tt.metadata != "" {
					writeContext(t, cfg, "broken", "tcp://127.0.0.1:12345")
					sum := sha256.Sum256([]byte("broken"))
					path := filepath.Join(cfg, "contexts", "meta", hex.EncodeToString(sum[:]), "meta.json")
					if err := os.WriteFile(path, []byte(tt.metadata), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				c, err := New()
				if c != nil {
					c.Close()
					t.Fatal("New returned a client for an invalid context")
				}
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("New() error = %v, want %q", err, tt.wantErr)
				}
			})
		}
	}
}
