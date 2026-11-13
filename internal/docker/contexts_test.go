package docker

import (
	"context"
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
