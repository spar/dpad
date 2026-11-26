package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"

	"dpad/internal/compose"
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
	script := "#!/bin/sh\nexec \"$DP_TUI_COMPOSE_BINARY\" -test.run=^TestTUIComposeHelper$ -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DP_TUI_COMPOSE_BINARY", os.Args[0])
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	t.Setenv("DP_TUI_COMPOSE_MODEL", model)
	t.Setenv("DP_TUI_COMPOSE_LOG", log)
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
	if os.Getenv("DP_TUI_COMPOSE_BINARY") == "" {
		return
	}
	args := os.Args[slices.Index(os.Args, "--")+1:]
	if slices.Contains(args, "version") {
		fmt.Print("2.40.3")
		os.Exit(0)
	}
	if slices.Contains(args, "config") {
		for i, arg := range args[:len(args)-1] {
			if b, _ := os.ReadFile(args[i+1]); arg == "--file" && strings.Contains(string(b), "INVALID") {
				fmt.Fprintln(os.Stderr, "yaml: line 2: mapping values are not allowed in this context")
				os.Exit(1)
			}
		}
		b, _ := os.ReadFile(os.Getenv("DP_TUI_COMPOSE_MODEL"))
		os.Stdout.Write(b)
		os.Exit(0)
	}
	step := "up"
	if slices.Contains(args, "pull") {
		step = "pull"
	}
	project := args[slices.Index(args, "--project-name")+1]
	f, _ := os.OpenFile(os.Getenv("DP_TUI_COMPOSE_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	fmt.Fprintln(f, step+" "+project)
	f.Close()
	fmt.Fprintln(os.Stdout, "updated "+project)
	os.Exit(0)
}

// loadComposePreview runs the discovery and Compose-file reading steps that
// newComposeUpdate starts, without waiting on the elapsed-time ticker.
func loadComposePreview(t *testing.T, v *composeUpdateView, load tea.Cmd) {
	t.Helper()
	msg, ok := load().(composeDiscoveredMsg)
	if !ok {
		t.Fatal("update did not start by discovering projects")
	}
	v.Update(msg)
	if len(msg.projects) > 0 {
		if v.stage != "reading" || !strings.Contains(ansi.Strip(v.View()), "reading Compose files") {
			t.Fatal("discovered projects are not listed while their files are read", v.View())
		}
		v.Update(v.prepare(msg.projects, msg.containers)())
	}
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
	loadComposePreview(t, v, load)
	if v.stage != "preview" || len(v.projects) != 1 || v.projects[0].Name != "a" {
		t.Fatal("single project preview is wrong", v.projects)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("preview changed a project")
	}
	press(a, "enter")
	if !v.showOutput || !strings.Contains(ansi.Strip(v.View()), "Directory:") || !strings.Contains(v.Footer(), "start update") {
		t.Fatal("source preview unavailable or cannot start the update from it")
	}
	press(a, "esc", "esc")
	if a.composeUpdate != nil || a.tabs[3].Filter() != "a" {
		t.Fatal("cancel lost project view state")
	}
	load = a.handleKey(key("U"))
	v = a.composeUpdate
	loadComposePreview(t, v, load)
	if len(v.projects) != 2 || !strings.Contains(v.View(), "all 2 projects") {
		t.Fatal("all projects used the tab filter")
	}
	if err := os.WriteFile(model, []byte(`{"services":{"web":{"image":"web:changed"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if v.HandleKey(key("y")) == nil || v.stage != "running" || !v.showOutput {
		t.Fatal("confirmation did not start the update and open live progress")
	}
	if v.HandleKey(key("y")) != nil {
		t.Fatal("duplicate confirmation accepted")
	}
	finishComposeUpdate(t, v, v.waitEvents())
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("stale preview performed writes")
	}
	if !strings.Contains(v.Status(), "2 failed") || !strings.Contains(ansi.Strip(v.View()), "configuration changed") {
		t.Fatal("changed configuration was not reported per project", v.Status(), v.View())
	}
	press(a, "esc")
	if !a.flashErr || !strings.Contains(a.flash, "2 failed") {
		t.Fatal("closing a failed update left no summary", a.flash)
	}
	load = a.handleKey(key("U"))
	v = a.composeUpdate
	defer v.Close()
	loadComposePreview(t, v, load)
	v.HandleKey(key("y"))
	if !v.showOutput || !v.followActive {
		t.Fatal("confirmation did not automatically open live Compose output")
	}
	cmd := v.waitEvents()
	press(a, "c", "R")
	if a.contextPicker != nil || a.projectAction != nil || a.confirm != nil {
		t.Fatal("busy update allowed another action/context switch")
	}
	finishComposeUpdate(t, v, cmd)
	if v.stage != "done" || !strings.Contains(v.Status(), "2 up to date") {
		t.Fatal(v.stage, v.Status())
	}
	plain := ansi.Strip(v.View())
	if v.showOutput || !strings.Contains(plain, "Finished in") || strings.Count(plain, "already up to date") != 3 {
		t.Fatal("completed bulk update did not show a per-project summary", plain)
	}
	press(a, "enter")
	if !v.showOutput || strings.Contains(ansi.Strip(v.View()), "Directory:") {
		t.Fatal("completed update output is unavailable")
	}
	b, err := os.ReadFile(log)
	if err != nil || string(b) != "pull a\nup a\npull b\nup b\n" {
		t.Fatalf("update commands = %q, %v", b, err)
	}
	press(a, "esc")
	if a.tabs[3].Filter() != "a" || a.flashErr || !strings.Contains(a.flash, "2 already up to date") {
		t.Fatal("completed update lost tab filter or summary", a.flash)
	}
}

func progressFixture(t *testing.T) *composeUpdateView {
	t.Helper()
	v, _ := newComposeUpdate(t.Context(), &fakeClient{}, "", 90, 16)
	t.Cleanup(v.Close)
	v.Update(composePlanMsg{view: v, gen: v.gen, projects: []compose.Project{{Name: "alpha"}, {Name: "beta"}}})
	v.stage, v.showOutput, v.followActive = "running", true, true
	v.operationCancel = func() {}
	v.SetSize(90, 16)
	return v
}

func TestComposeRawLogDisplaysNativeMultilineOutputAndPhase(t *testing.T) {
	v := progressFixture(t)
	v.rawOutput = true
	output := "[1/2] Pull images\r\n$ docker compose pull\r\nweb Pulling\nabc Downloading 10 MB / 20 MB\nabc Extracting\nweb Pulled\n"
	v.Update(composeEventsMsg{view: v, events: []compose.Event{{Project: "alpha", State: "pulling", Output: output}}})
	var lines []string
	for line := range strings.SplitSeq(ansi.Strip(v.View()), "\n") {
		lines = append(lines, strings.TrimSpace(line))
	}
	for _, line := range []string{"web Pulling", "abc Downloading 10 MB / 20 MB", "abc Extracting", "web Pulled"} {
		if !slices.Contains(lines, line) {
			t.Fatalf("Compose line was flattened or hidden: %q\n%s", line, v.View())
		}
	}
	if !strings.Contains(ansi.Strip(v.View()), "alpha · project 1 of 2 · local · pulling images") || !strings.Contains(ansi.Strip(v.View()), "beta") {
		t.Fatal("current project and pull phase are unclear")
	}
	if strings.Contains(v.View(), "Directory:") || !strings.Contains(v.Footer(), "cancel remaining") {
		t.Fatal("live console is cluttered or lacks cancellation")
	}
	v.Update(composeEventsMsg{view: v, events: []compose.Event{{Project: "alpha", State: "starting", Output: output + "\n[2/2] Start updated services\nContainer alpha-web Recreated\nContainer alpha-web Started\n"}}})
	if !strings.Contains(ansi.Strip(v.View()), "recreating services") || !strings.Contains(v.View(), "Container alpha-web Started") {
		t.Fatal("container recreation phase is hidden")
	}
}

func TestComposeProgressScrollbackAndBulkProjectFollowing(t *testing.T) {
	v := progressFixture(t)
	v.rawOutput = true
	var output strings.Builder
	for i := range 100 {
		fmt.Fprintf(&output, "Layer %03d Downloading\n", i)
	}
	v.Update(composeEventsMsg{view: v, events: []compose.Event{{Project: "alpha", State: "pulling", Output: output.String()}}})
	if !v.vp.AtBottom() || v.cur.key != "alpha" {
		t.Fatal("live output did not follow the active project")
	}
	previous := v.vp.YOffset()
	v.HandleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	position := v.vp.YOffset()
	visible := v.vp.View()
	if position >= previous || v.followActive {
		t.Fatal("scrolling up did not pause following")
	}
	v.Update(composeEventsMsg{view: v, events: []compose.Event{{Project: "alpha", State: "pulling", Output: output.String() + "Another layer Downloading\n"}, {Project: "beta", State: "pulling", Output: "beta Pulling\n"}}})
	if v.cur.key != "alpha" || v.vp.YOffset() != position {
		t.Fatal("new output or the next project stole the scrollback position")
	}
	if v.vp.View() != visible {
		t.Fatal("paused scrollback changed while new output arrived")
	}
	v.HandleKey(key("G"))
	if v.cur.key != "beta" || !v.vp.AtBottom() || !v.followActive || !strings.Contains(v.View(), "beta Pulling") {
		t.Fatal("G did not resume following the active project")
	}
	v.HandleKey(key("["))
	if v.cur.key != "alpha" || v.followActive {
		t.Fatal("previous project's output could not be inspected")
	}
	v.Update(composeEventsMsg{view: v, events: []compose.Event{{Project: "beta", State: "starting", Output: "beta Starting\n"}}})
	if v.cur.key != "alpha" {
		t.Fatal("active project stole an explicitly selected project")
	}
	v.HandleKey(key("tab"))
	if v.showOutput {
		t.Fatal("Tab did not open the project list")
	}
	v.HandleKey(key("enter"))
	if !v.showOutput || v.cur.key != "alpha" {
		t.Fatal("project list could not reopen selected output")
	}
}

func TestComposeProgressWrapsLongMessagesAndPreservesReadbackAcrossList(t *testing.T) {
	v := progressFixture(t)
	v.rawOutput = true
	v.SetSize(35, 10)
	message := "image Downloading sha256:" + strings.Repeat("a", 64) + " complete"
	output := strings.Repeat("existing layer\n", 30) + message + "\n"
	v.Update(composeEventsMsg{view: v, events: []compose.Event{{Project: "alpha", State: "pulling", Output: output}}})
	if !strings.Contains(ansi.Strip(v.View()), "complete") {
		t.Fatal("long Compose output was truncated horizontally")
	}
	v.HandleKey(key("g"))
	before := v.vp.View()
	v.HandleKey(key("tab"))
	v.HandleKey(key("tab"))
	if v.vp.View() != before {
		t.Fatal("opening the project list lost the scrollback position")
	}
}

func TestComposeProgressEscapeCancelsDirectlyAndCompletionReturns(t *testing.T) {
	v := progressFixture(t)
	a := newTestApp()
	a.composeUpdate = v
	cancelled := false
	v.operationCancel = func() { cancelled = true }
	press(a, "esc")
	if !cancelled || v.stage != "cancelling" || a.composeUpdate != v || !v.showOutput {
		t.Fatal("Esc did not cancel directly from live output")
	}
	v.Update(composeEventsMsg{view: v, events: []compose.Event{{Project: "alpha", State: "unknown", Output: "Container recreation interrupted\n"}}, done: true})
	if !strings.Contains(ansi.Strip(v.View()), "interrupted") {
		t.Fatal("interrupted operation output was lost")
	}
	press(a, "esc")
	if a.composeUpdate != nil || v.ctx.Err() == nil {
		t.Fatal("completed console did not return to Projects")
	}
}

func TestComposeUpdateBlockedTargetsAndClosedPreviewNeverRun(t *testing.T) {
	a, _, log := composeUIFixture(t)
	c := a.client.(*fakeClient)
	delete(c.containers[0].Labels, composeProjectLabel+".config_files")
	load := a.handleKey(key("u"))
	v := a.composeUpdate
	loadComposePreview(t, v, load)
	if v.ready() != 0 || v.HandleKey(key("y")) != nil || !strings.Contains(v.Status(), "1 blocked") {
		t.Fatal("blocked project could be confirmed")
	}
	press(a, "esc")
	load = a.handleKey(key("U"))
	v = a.composeUpdate
	msg := load()
	press(a, "esc")
	if v.Update(msg) != nil || v.ctx.Err() == nil {
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
	a.setFilter("no-such-project")
	press(a, "u")
	if a.composeUpdate != nil || !a.flashErr || !strings.Contains(a.flash, "select a project") {
		t.Fatal("u without a selection gave no explanation", a.flash)
	}
	a.setFilter("")
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
	v.Update(composePlanMsg{view: v, gen: v.gen, projects: []compose.Project{p}})
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
	v.Update(composePlanMsg{view: v, gen: v.gen, projects: []compose.Project{p}})
	if !strings.Contains(v.Footer(), "local/SSH") || v.HandleKey(key("h")) == nil || v.stage != "loading" || v.locations["demo"] != "ssh" {
		t.Fatal("execution-host change did not load a fresh preview")
	}
	if v.HandleKey(key("y")) != nil {
		t.Fatal("host change could be confirmed before preview loaded")
	}
	p.Remote, p.Location = true, "ssh"
	v.Update(composePlanMsg{view: v, gen: v.gen, projects: []compose.Project{p}})
	if v.HandleKey(key("h")) == nil || v.locations["demo"] != "local" {
		t.Fatal("could not switch back to local execution")
	}
}

type envClient struct {
	fakeClient
	env map[string][]string
}

func (c *envClient) Inspect(_ context.Context, id string) (container.InspectResponse, error) {
	return container.InspectResponse{Config: &container.Config{Env: c.env[id]}}, nil
}

func TestContainerEnvLookupNeedsOneAgreedValue(t *testing.T) {
	items := []container.Summary{
		composeContainer("runner", "ci", "runner", "running"),
		composeContainer("dind", "ci", "dind", "running"),
		composeContainer("other", "mixed", "a", "running"),
		composeContainer("other2", "mixed", "b", "running"),
	}
	c := &envClient{env: map[string][]string{
		"runner": {"PATH=/bin", "RUNNER_STACK=expense-tracker"},
		"dind":   {"PATH=/bin"},
		"other":  {"RUNNER_STACK=a"},
		"other2": {"RUNNER_STACK=b"},
	}}
	lookup := containerEnvLookup(t.Context(), c, items)
	if value, _, ok := lookup("ci", "RUNNER_STACK"); !ok || value != "expense-tracker" {
		t.Fatal("value set by one container was not recovered", value, ok)
	}
	if _, _, ok := lookup("mixed", "RUNNER_STACK"); ok {
		t.Fatal("containers disagreeing on a value still produced one")
	}
	if _, _, ok := lookup("ci", "TAG"); ok {
		t.Fatal("a variable no container sets was invented")
	}
}

func TestBlockedMissingVariableExplainsConfigFix(t *testing.T) {
	v, _ := newComposeUpdate(t.Context(), &fakeClient{}, "", 100, 30)
	defer v.Close()
	p := compose.Project{Name: "ci-runner", Directory: "/srv/ci", Issue: (&compose.MissingVariableError{Name: "RUNNER_STACK", Hint: "set by ./runners"}).Error()}
	v.Update(composePlanMsg{view: v, gen: v.gen, projects: []compose.Project{p}})
	plain := ansi.Strip(v.View())
	if !strings.Contains(plain, "blocked: needs RUNNER_STACK: set by ./runners") || !strings.Contains(plain, "–") {
		t.Fatal("blocked row does not give the short reason", plain)
	}
	v.HandleKey(key("enter"))
	plain = ansi.Strip(v.View())
	if !strings.Contains(plain, "composeProjects:") || !strings.Contains(plain, "RUNNER_STACK: <value>") {
		t.Fatal("details do not say how to set the variable", plain)
	}
}
