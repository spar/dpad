package tui

import (
	"errors"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"

	"dpad/internal/config"
)

func configWithMouse(mouse *bool) config.Config { return config.Config{Mouse: mouse} }

func commandApp(t *testing.T, yaml string) *App {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	a := newTestApp()
	a.client = &fakeClient{}
	a.configure(cfg, nil)
	a.Update(containersMsg{items: []container.Summary{{ID: "id-web", Names: []string{"/web"}, Image: "nginx", State: "running", Labels: map[string]string{composeProjectLabel: "shop", composeServiceLabel: "web"}}}})
	return a
}

func TestCustomCommandRunsWithTheSelectedContainer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	a := commandApp(t, `customCommands:
  - {name: show, key: O, command: "echo {{.Name}} {{.Image}} {{.Project}}/{{.Service}}"}`)
	_, cmd := a.Update(key("O"))
	if cmd == nil || a.actionsInFlight != 1 {
		t.Fatal("o should start the command")
	}
	msg := cmd().(actionDoneMsg)
	if msg.err != nil || msg.text != "show on web: done: web nginx shop/web" {
		t.Fatalf("%+v", msg)
	}
}

func TestCustomCommandConfirmsAndReportsFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	a := commandApp(t, `customCommands:
  - {name: fail, key: O, confirm: true, command: "echo nope >&2; exit 3"}`)
	a.Update(key("O"))
	if a.confirm == nil || !strings.Contains(a.footer(), "Run fail on web?") {
		t.Fatal("confirm: true should ask first")
	}
	_, cmd := a.Update(key("y"))
	msg := cmd().(actionDoneMsg)
	var exit interface{ ExitCode() int }
	if !errors.As(msg.err, &exit) || exit.ExitCode() != 3 || !strings.Contains(msg.err.Error(), "nope") {
		t.Fatalf("err = %v", msg.err)
	}
}

func TestProjectCommandsApplyOnlyToProjectRows(t *testing.T) {
	a := commandApp(t, `customCommands:
  - {name: compose ps, key: O, scope: project, command: "docker compose -p {{.Project}} ps"}`)
	_, cmd := a.Update(key("O"))
	if cmd != nil || !strings.Contains(a.flash, "no custom command for O applies") {
		t.Fatalf("project command ran on a container row, flash %q", a.flash)
	}
	press(a, "4")
	if target, vars, ok := a.customCommandTarget(config.ScopeProject); !ok || target != "project shop" || vars.Project != "shop" {
		t.Fatalf("target %q vars %+v", target, vars)
	}
	if _, help := a.helpRows(); !slices.ContainsFunc(help, func(h [2]string) bool { return strings.Contains(h[1], "compose ps (custom)") }) {
		t.Fatal("help should list the custom command")
	}
}

func TestConfigErrorsShowInTheStatus(t *testing.T) {
	a := newTestApp()
	a.configure(config.Config{}, errors.New("config.yaml: line 3:\n bad"))
	if a.flash != "config: config.yaml: line 3: bad" || !a.flashErr {
		t.Fatalf("flash %q", a.flash)
	}
}
