package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"dockernext/internal/compose"
)

func composeUIFixture(t *testing.T) (*App, string, string) {
	t.Helper()
	dir, bin := t.TempDir(), t.TempDir()
	model, log := filepath.Join(dir, "model.json"), filepath.Join(dir, "mutations")
	if err := os.WriteFile(model, []byte(`{"services":{"web":{"image":"web:latest"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	composeFile := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(composeFile, []byte("services: {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nexec \"$DN_TUI_COMPOSE_BINARY\" -test.run=^TestTUIComposeHelper$ -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DN_TUI_COMPOSE_BINARY", os.Args[0])
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	t.Setenv("DN_TUI_COMPOSE_MODEL", model)
	t.Setenv("DN_TUI_COMPOSE_LOG", log)
	t.Setenv("DOCKER_HOST", "unix:///bound.sock")
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("COMPOSE_PROFILES", "")
	c := &fakeClient{cliEnv: os.Environ()}
	for _, name := range []string{"a", "b"} {
		item := composeContainer(name+"-web", name, "web", "running")
		item.Labels[composeProjectLabel+".working_dir"] = dir
		item.Labels[composeProjectLabel+".config_files"] = composeFile
		c.containers = append(c.containers, item)
	}
	a := newTestApp()
	a.ctx, a.client, a.active = t.Context(), c, 3
	a.Update(containersMsg{items: c.containers})
	return a, model, log
}

func TestTUIComposeHelper(t *testing.T) {
	if os.Getenv("DN_TUI_COMPOSE_BINARY") == "" {
		return
	}
	args := os.Args[slices.Index(os.Args, "--")+1:]
	if slices.Contains(args, "version") {
		fmt.Print("2.40.3")
		os.Exit(0)
	}
	if slices.Contains(args, "config") {
		b, _ := os.ReadFile(os.Getenv("DN_TUI_COMPOSE_MODEL"))
		os.Stdout.Write(b)
		os.Exit(0)
	}
	step := "up"
	if slices.Contains(args, "pull") {
		step = "pull"
	}
	project := args[slices.Index(args, "--project-name")+1]
	f, _ := os.OpenFile(os.Getenv("DN_TUI_COMPOSE_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	fmt.Fprintln(f, step+" "+project)
	f.Close()
	fmt.Fprintln(os.Stdout, "updated "+project)
	os.Exit(0)
}

func finishComposeUpdate(t *testing.T, v *composeUpdateView, cmd tea.Cmd) {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if _, ok := msg.(refreshMsg); ok {
			return
		}
		cmd = v.Update(msg)
	}
}

func TestComposeUpdateSingleAndAllIgnoreListFilterOnlyForAll(t *testing.T) {
	a, model, log := composeUIFixture(t)
	a.setFilter("a")
	load := a.handleKey(key("u"))
	v := a.composeUpdate
	v.Update(load())
	if v.stage != "preview" || len(v.projects) != 1 || v.projects[0].Name != "a" {
		t.Fatal("single project preview is wrong", v.projects)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("preview changed a project")
	}
	press(a, "enter")
	if !v.showOutput || !strings.Contains(ansi.Strip(v.View()), "Directory:") {
		t.Fatal("source preview unavailable")
	}
	press(a, "esc", "esc")
	if a.composeUpdate != nil || a.tabs[3].Filter() != "a" {
		t.Fatal("cancel lost project view state")
	}
	load = a.handleKey(key("U"))
	v = a.composeUpdate
	defer v.Close()
	v.Update(load())
	if len(v.projects) != 2 || !strings.Contains(v.View(), "filter ignored") {
		t.Fatal("all projects used the tab filter")
	}
	if err := os.WriteFile(model, []byte(`{"services":{"web":{"image":"web:changed"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	check := v.HandleKey(key("y"))
	if v.HandleKey(key("y")) != nil {
		t.Fatal("duplicate confirmation accepted")
	}
	if v.Update(check()) != nil || v.stage != "preview" || !strings.Contains(v.note, "changed") {
		t.Fatal("changed configuration did not require fresh confirmation")
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("stale preview performed writes")
	}
	check = v.HandleKey(key("y"))
	cmd := v.Update(check())
	press(a, "c", "R")
	if a.contextPicker != nil || a.projectAction != nil || a.confirm != nil {
		t.Fatal("busy update allowed another action/context switch")
	}
	finishComposeUpdate(t, v, cmd)
	if v.stage != "done" || !strings.Contains(v.Status(), "2 succeeded") {
		t.Fatal(v.stage, v.Status())
	}
	b, err := os.ReadFile(log)
	if err != nil || string(b) != "pull a\nup a\npull b\nup b\n" {
		t.Fatalf("update commands = %q, %v", b, err)
	}
	press(a, "esc")
	if a.tabs[3].Filter() != "a" {
		t.Fatal("completed update lost tab filter")
	}
}

func TestComposeUpdateBlockedTargetsAndClosedPreviewNeverRun(t *testing.T) {
	a, _, log := composeUIFixture(t)
	c := a.client.(*fakeClient)
	delete(c.containers[0].Labels, composeProjectLabel+".config_files")
	load := a.handleKey(key("u"))
	v := a.composeUpdate
	v.Update(load())
	if v.ready() != 0 || v.HandleKey(key("y")) != nil || !strings.Contains(v.Status(), "1 blocked") {
		t.Fatal("blocked project could be confirmed")
	}
	press(a, "esc")
	load = a.handleKey(key("U"))
	v = a.composeUpdate
	v.Update(load())
	check := v.HandleKey(key("y"))
	press(a, "esc")
	if v.Update(check()) != nil || v.ctx.Err() == nil {
		t.Fatal("closed preview started an update")
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("closed preview performed writes")
	}
}

func TestComposeUpdateRequiresProjectsTabAndWaitsForActions(t *testing.T) {
	a, _, _ := composeUIFixture(t)
	a.active = 0
	press(a, "u", "U")
	if a.composeUpdate != nil {
		t.Fatal("updates opened outside Projects")
	}
	a.active, a.actionsInFlight = 3, 1
	press(a, "u")
	if a.composeUpdate != nil || !a.flashErr {
		t.Fatal("update raced an in-flight container action")
	}
	a.actionsInFlight = 0
	a.tabs[3].HandleKey("j")
	load := a.handleKey(key("u"))
	if load == nil || a.composeUpdate.selected != "a" {
		t.Fatal("container row could not update its parent project")
	}
	a.composeUpdate.Close()
}

func TestComposeUpdateOutputAndSmallTerminals(t *testing.T) {
	v, _ := newComposeUpdate(t.Context(), &fakeClient{}, "demo", 80, 12)
	defer v.Close()
	p := compose.Project{Name: "demo", Directory: "/srv/demo", Files: []string{"/srv/demo/compose.yaml"}, Services: []string{"web"}, Images: []string{"web:latest"}}
	v.Update(composePlanMsg{view: v, projects: []compose.Project{p}})
	for _, size := range [][2]int{{80, 12}, {30, 5}, {10, 1}} {
		v.SetSize(size[0], size[1])
		for _, output := range []bool{false, true} {
			v.showOutput = output
			v.updateOutput(true)
			text := ansi.Strip(v.View())
			if len(strings.Split(text, "\n")) > size[1] {
				t.Fatal("update view overflows height", text)
			}
			for line := range strings.SplitSeq(text, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatal("update view overflows width", line)
				}
			}
		}
	}
}

func TestComposeUpdateExecutionHostSelectionRequiresNewPreview(t *testing.T) {
	c := &fakeClient{cliEnv: []string{"DOCKER_HOST=ssh://server"}}
	v, _ := newComposeUpdate(t.Context(), c, "demo", 100, 16)
	defer v.Close()
	p := compose.Project{Name: "demo", Directory: "/srv/demo", Services: []string{"web"}}
	v.Update(composePlanMsg{view: v, projects: []compose.Project{p}})
	if !strings.Contains(v.Footer(), "local/SSH") || v.HandleKey(key("h")) == nil || v.stage != "loading" || v.locations["demo"] != "ssh" {
		t.Fatal("execution-host change did not load a fresh preview")
	}
	if v.HandleKey(key("y")) != nil {
		t.Fatal("host change could be confirmed before preview loaded")
	}
	p.Remote, p.Location = true, "ssh"
	v.Update(composePlanMsg{view: v, projects: []compose.Project{p}})
	if v.HandleKey(key("h")) == nil || v.locations["demo"] != "local" {
		t.Fatal("could not switch back to local execution")
	}
}
