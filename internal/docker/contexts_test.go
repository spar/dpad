package docker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/moby/moby/client"
)

func TestContextsListsSavedEndpointsWithoutCredentials(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("DOCKER_CONFIG", cfg)
	got, err := Contexts()
	want := []Context{{Name: "default", Endpoint: client.DefaultDockerHost}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("empty store = %+v, %v", got, err)
	}
	writeContext(t, cfg, "z-rootless", "ssh://me@server/run/user/1000/docker.sock")
	writeContext(t, cfg, "a-local", "unix:///other.sock")
	want = []Context{
		{Name: "a-local", Endpoint: "unix:///other.sock"},
		{Name: "default", Endpoint: client.DefaultDockerHost},
		{Name: "z-rootless", Endpoint: "ssh://server/run/user/1000/docker.sock"},
	}
	got, err = Contexts()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("contexts = %+v, %v; want %+v", got, err, want)
	}
	broken := filepath.Join(cfg, "contexts", "meta", "broken")
	if err := os.MkdirAll(broken, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "meta.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Contexts(); err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("invalid metadata error = %v", err)
	}
}

func TestNewForContextOverridesSelectionWithoutChangingIt(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("DOCKER_CONFIG", cfg)
	t.Setenv("DOCKER_HOST", "invalid-host-that-must-be-ignored")
	t.Setenv("DOCKER_CONTEXT", "missing")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	writeContext(t, cfg, "picked", "tcp://127.0.0.1:12345")
	config := []byte(`{"currentContext":"saved"}`)
	configPath := filepath.Join(cfg, "config.json")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"picked", "default"} {
		c, err := NewForContext(t.Context(), name)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		host := "tcp://127.0.0.1:12345"
		if name == "default" {
			host = client.DefaultDockerHost
		}
		if c.ConnectionLabel() != name+" @ "+host || !slices.Contains(c.CLIEnvironment(), "DOCKER_HOST="+host) {
			t.Fatalf("wrong selected daemon: %s", c.ConnectionLabel())
		}
	}
	if os.Getenv("DOCKER_CONTEXT") != "missing" || os.Getenv("DOCKER_HOST") != "invalid-host-that-must-be-ignored" {
		t.Fatal("selection changed the process environment")
	}
	b, err := os.ReadFile(configPath)
	if err != nil || string(b) != string(config) {
		t.Fatal("selection rewrote the saved Docker context")
	}
	if c, err := NewForContext(t.Context(), "missing"); c != nil || err == nil {
		t.Fatal("missing context fell back to another daemon")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := NewForContext(ctx, "picked"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v", err)
	}
}

func TestNewForContextSupportsRootlessSSH(t *testing.T) {
	useFakeSSH(t, "serve")
	cfg := t.TempDir()
	t.Setenv("DOCKER_CONFIG", cfg)
	t.Setenv("DOCKER_HOST", "tcp://wrong-host:12345")
	t.Setenv("DOCKER_CONTEXT", "missing")
	endpoint := "ssh://me@server/run/user/1000/docker.sock"
	writeContext(t, cfg, "rootless", endpoint)
	c, err := NewForContext(t.Context(), "rootless")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !slices.Contains(c.CLIEnvironment(), "DOCKER_HOST="+endpoint) {
		t.Fatal("shell did not retain the picked SSH endpoint")
	}
	items, err := c.Containers(t.Context(), true)
	if err != nil || len(items) != 1 {
		t.Fatalf("picked SSH daemon = %+v, %v", items, err)
	}
}

func TestContextMetadataNameOnlyRequiredForListing(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("DOCKER_CONFIG", cfg)
	writeContext(t, cfg, "picked", "unix:///picked.sock")
	sum := sha256.Sum256([]byte("picked"))
	metadataPath := filepath.Join(cfg, "contexts", "meta", hex.EncodeToString(sum[:]), "meta.json")
	for _, tc := range []struct {
		name, metadata string
		listingError   bool
	}{
		{"missing name", `{"Endpoints":{"docker":{"Host":"unix:///picked.sock"}}}`, false},
		{"non-string name", `{"Name":42,"Endpoints":{"docker":{"Host":"unix:///picked.sock"}}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(metadataPath, []byte(tc.metadata), 0o600); err != nil {
				t.Fatal(err)
			}
			name, host, err := readContextEndpoint(cfg, "picked")
			if err != nil || name != "picked" || host != "unix:///picked.sock" {
				t.Fatalf("selected endpoint = %q, %q, %v", name, host, err)
			}
			choices, err := Contexts()
			if tc.listingError {
				if err == nil || !strings.Contains(err.Error(), hex.EncodeToString(sum[:])) {
					t.Fatalf("listing error = %v", err)
				}
			} else if err != nil || len(choices) != 1 || choices[0].Name != "default" {
				t.Fatalf("listing included unnamed context: %+v, %v", choices, err)
			}
		})
	}
}

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
