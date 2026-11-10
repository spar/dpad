package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestDockerArgs(t *testing.T) {
	tests := []struct {
		args     []string
		anywhere bool
		want     []string
	}{
		{[]string{"ps", "--no-color", "-q"}, true, []string{"ps", "-q"}},
		{[]string{"stats", "web", "--no-color=true"}, true, []string{"stats", "web"}},
		{[]string{"logs", "--", "--no-color"}, true, []string{"logs", "--", "--no-color"}},
		{[]string{"--no-color", "network", "ls"}, false, []string{"network", "ls"}},
		{[]string{"exec", "web", "ls", "--no-color"}, false, []string{"exec", "web", "ls", "--no-color"}},
	}
	for _, tt := range tests {
		if got := dockerArgs(tt.args, tt.anywhere); !slices.Equal(got, tt.want) {
			t.Errorf("dockerArgs(%q, %v) = %q, want %q", tt.args, tt.anywhere, got, tt.want)
		}
	}
}

func TestShouldPassthrough(t *testing.T) {
	root := newRoot()
	for args, want := range map[string]bool{
		"":                          false,
		"ps -a":                     false,
		"logs -f web":               false,
		"--help":                    false,
		"--no-color":                false,
		"--no-color ps":             false,
		"--no-color network ls":     true,
		"network ls":                true,
		"--context prod ps":         true,
		"--no-color --context x ps": true,
		"-H tcp://h:2375 ps":        true,
	} {
		if got := shouldPassthrough(root, strings.Fields(args)); got != want {
			t.Errorf("shouldPassthrough(%q) = %v, want %v", args, got, want)
		}
	}
}

// fakeDocker puts a docker on PATH that records its arguments, and returns
// a function reading them back.
func fakeDocker(t *testing.T) func() string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "args")
	script := "#!/bin/sh\necho \"$@\" > " + out + "\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return func() string {
		b, _ := os.ReadFile(out)
		return strings.TrimSpace(string(b))
	}
}

func TestRunPassesThrough(t *testing.T) {
	for args, want := range map[string]string{
		"network ls":                  "network ls",
		"--no-color network ls":       "network ls",
		"ps --no-color -q":            "ps -q",
		"ps --format {{.Names}}":      "ps --format {{.Names}}",
		"stats web --no-color":        "stats web",
		"--context prod images":       "--context prod images",
		"exec web ls --no-color":      "exec web ls --no-color",
		"logs --details --no-color w": "logs --details w",
	} {
		got := fakeDocker(t)
		if code := run(strings.Fields(args)); code != 0 {
			t.Errorf("run(%q) exit code %d", args, code)
		}
		if got() != want {
			t.Errorf("run(%q) ran docker %q, want %q", args, got(), want)
		}
	}
}
