package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseKeepsValidCommandsAndReportsTheRest(t *testing.T) {
	cfg, err := Parse([]byte(`
mouse: false
customCommands:
  - name: bash
    key: b
    command: docker exec -it {{.ID}} bash
    attach: true
  - name: compose ps
    key: b
    scope: project
    command: docker compose -p {{.Project}} ps
  - name: clash
    key: x
    command: echo
  - name: duplicate
    key: b
    command: echo
  - name: typo
    key: o
    command: echo {{.Bogus}}
  - name: wrong scope
    key: w
    scope: service
    command: echo
  - key: z
    command: echo
`))
	if cfg.MouseEnabled() {
		t.Error("mouse: false should turn the mouse off")
	}
	var names []string
	for _, c := range cfg.CustomCommands {
		names = append(names, c.Name+"/"+c.Scope)
	}
	if got := strings.Join(names, ","); got != "bash/container,compose ps/project" {
		t.Fatalf("valid commands = %s", got)
	}
	for _, want := range []string{`clash: key "x" is already used by dpad`, `duplicate: key "b" is already used by bash`, "typo:", "wrong scope: scope must be", "custom command 7: name is required"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error %v should mention %q", err, want)
		}
	}
}

func TestMissingFileIsDefaults(t *testing.T) {
	t.Setenv("DPAD_CONFIG", filepath.Join(t.TempDir(), "none.yaml"))
	cfg, err := Load()
	if err != nil || !cfg.MouseEnabled() || len(cfg.CustomCommands) != 0 {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestPathPrefersOverrideThenXDG(t *testing.T) {
	t.Setenv("DPAD_CONFIG", "/tmp/x.yaml")
	if p, _ := Path(); p != "/tmp/x.yaml" {
		t.Errorf("DPAD_CONFIG ignored: %s", p)
	}
	t.Setenv("DPAD_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if p, _ := Path(); p != filepath.Join("/xdg", "dpad", "config.yaml") {
		t.Errorf("XDG_CONFIG_HOME ignored: %s", p)
	}
}

func TestRenderQuotesValuesSoTheyStayOneWord(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	cfg, err := Parse([]byte("customCommands:\n  - {name: echo, key: o, command: 'printf %s {{.Service}}'}\n"))
	if err != nil {
		t.Fatal(err)
	}
	evil := `x'; touch pwned; echo '$(id)`
	line, err := cfg.CustomCommands[0].Render(Vars{Service: evil})
	if err != nil {
		t.Fatal(err)
	}
	args := Shell(line)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = t.TempDir()
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != evil {
		t.Fatalf("output %q, want the value unchanged", out)
	}
	if _, err := os.Stat(filepath.Join(cmd.Dir, "pwned")); err == nil {
		t.Fatal("value was run as a command")
	}
}

func TestParseComposeProjectEnv(t *testing.T) {
	cfg, err := Parse([]byte(`
composeProjects:
  ci-runner:
    env:
      RUNNER_STACK: ci
`))
	if err != nil || cfg.ComposeProjects["ci-runner"].Env["RUNNER_STACK"] != "ci" {
		t.Fatal("per-project Compose env not parsed", cfg.ComposeProjects, err)
	}
}
