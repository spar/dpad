package compose

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
)

const testModel = `{"services":{"web":{"image":"web:latest"},"worker":{"image":"worker:stable","profiles":["jobs"]},"tool":{"image":"tool:latest","profiles":["tools"]}}}`

func composeFixture(t *testing.T) (dir, model, log string) {
	t.Helper()
	dir, bin := t.TempDir(), t.TempDir()
	model, log = filepath.Join(dir, "model.json"), filepath.Join(dir, "calls.jsonl")
	for _, file := range []string{"compose.yaml", "override.yaml", ".env.custom"} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(model, []byte(testModel), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, executable := range []string{"docker", "docker-compose", "ssh"} {
		script := "#!/bin/sh\nexec " + quote(os.Args[0]) + " -test.run=^TestComposeHelper$ -- " + quote(executable) + " \"$@\"\n"
		if err := os.WriteFile(filepath.Join(bin, executable), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DP_COMPOSE_HELPER", "1")
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	t.Setenv("DP_COMPOSE_MODEL", model)
	t.Setenv("DP_COMPOSE_CALLS", log)
	t.Setenv("DOCKER_HOST", "tcp://bound-daemon:2375")
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("COMPOSE_PROFILES", "")
	return dir, model, log
}

type recordedCall struct {
	Args                     []string
	Host, Context, Directory string
}

func TestComposeHelper(t *testing.T) {
	if os.Getenv("DP_COMPOSE_HELPER") == "" {
		return
	}
	args := os.Args[slices.Index(os.Args, "--")+1:]
	backend := args[0]
	if args[0] == "ssh" {
		script := args[len(args)-1]
		if real := os.Getenv("DP_COMPOSE_REAL_DIR"); real != "" {
			script = strings.Replace(script, "cd '/srv/dp-project'", "cd "+quote(real), 1)
		}
		cmd := exec.Command("/bin/sh", "-c", script)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	args = args[1:]
	dir, _ := os.Getwd()
	f, err := os.OpenFile(os.Getenv("DP_COMPOSE_CALLS"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(2)
	}
	_ = json.NewEncoder(f).Encode(recordedCall{args, os.Getenv("DOCKER_HOST"), os.Getenv("DOCKER_CONTEXT"), dir})
	f.Close()
	if slices.Contains(args, "version") {
		if backend == "docker" && os.Getenv("DP_COMPOSE_STANDALONE") == "1" {
			os.Exit(1)
		}
		fmt.Print("2.40.3")
		os.Exit(0)
	}
	if slices.Contains(args, "config") {
		b, _ := os.ReadFile(os.Getenv("DP_COMPOSE_MODEL"))
		if !slices.Contains(args, "--profile") {
			var model map[string]json.RawMessage
			_ = json.Unmarshal(b, &model)
			var services map[string]json.RawMessage
			_ = json.Unmarshal(model["services"], &services)
			for name, raw := range services {
				var service struct{ Profiles []string }
				_ = json.Unmarshal(raw, &service)
				if len(service.Profiles) > 0 {
					delete(services, name)
				}
			}
			model["services"], _ = json.Marshal(services)
			b, _ = json.Marshal(model)
		}
		os.Stdout.Write(b)
		os.Exit(0)
	}
	project := args[slices.Index(args, "--project-name")+1]
	step := "up"
	if slices.Contains(args, "pull") {
		step = "pull"
	}
	if os.Getenv("DP_COMPOSE_BLOCK_STEP") == step {
		_ = os.WriteFile(os.Getenv("DP_COMPOSE_ENTERED"), []byte(step), 0o600)
		time.Sleep(time.Hour)
	}
	if step == "pull" && os.Getenv("DP_COMPOSE_EDIT_ON_PULL") == "1" {
		_ = os.WriteFile(os.Getenv("DP_COMPOSE_MODEL"), []byte(strings.ReplaceAll(testModel, "web:latest", "web:changed")), 0o600)
	}
	fmt.Println(step + " " + project)
	if step == "pull" && project == os.Getenv("DP_COMPOSE_FAIL_PROJECT") {
		fmt.Fprintln(os.Stderr, "registry denied")
		os.Exit(1)
	}
	os.Exit(0)
}

func TestPrepareSupportsStandaloneComposeV2AndBlocksMissingSources(t *testing.T) {
	dir, _, _ := composeFixture(t)
	t.Setenv("DP_COMPOSE_STANDALONE", "1")
	projects := Prepare(t.Context(), os.Environ(), Discover([]container.Summary{labeledProject("demo", dir)}, ""))
	if projects[0].Issue != "" || !slices.Equal(projects[0].CLI, []string{"docker-compose"}) {
		t.Fatal(projects)
	}
	missing := labeledProject("missing", filepath.Join(dir, "does-not-exist"))
	projects = Prepare(t.Context(), os.Environ(), Discover([]container.Summary{missing}, ""))
	if projects[0].Issue == "" {
		t.Fatal("missing local files accepted without an SSH endpoint")
	}
}

func labeledProject(name, dir string) container.Summary {
	return container.Summary{ID: name, State: container.StateRunning, Labels: map[string]string{
		projectLabel: name, projectLabel + ".working_dir": dir,
		projectLabel + ".config_files":     filepath.Join(dir, "compose.yaml") + "," + filepath.Join(dir, "override.yaml"),
		projectLabel + ".environment_file": filepath.Join(dir, ".env.custom"),
		"com.docker.compose.service":       "web",
	}}
}

func calls(t *testing.T, file string) []recordedCall {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var result []recordedCall
	for line := range strings.SplitSeq(strings.TrimSpace(string(b)), "\n") {
		var c recordedCall
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatal(err)
		}
		result = append(result, c)
	}
	return result
}

func mutations(all []recordedCall) []string {
	var result []string
	for _, c := range all {
		for _, step := range []string{"pull", "up"} {
			if slices.Contains(c.Args, step) {
				result = append(result, step+" "+c.Args[slices.Index(c.Args, "--project-name")+1])
			}
		}
	}
	return result
}

func TestDiscoverAndPrepareUseOriginalSourcesAndActiveServices(t *testing.T) {
	dir, _, log := composeFixture(t)
	web := labeledProject("demo", dir)
	worker := labeledProject("demo", dir)
	worker.Labels["com.docker.compose.service"] = "worker"
	projects := Prepare(t.Context(), os.Environ(), Discover([]container.Summary{web, worker, {}}, ""))
	if len(projects) != 1 || projects[0].Issue != "" || projects[0].Remote || !slices.Equal(projects[0].Services, []string{"web", "worker"}) {
		t.Fatalf("prepared projects = %+v", projects)
	}
	if len(projects[0].Files) != 2 || len(projects[0].EnvFiles) != 1 || len(mutations(calls(t, log))) != 0 {
		t.Fatal("preview lost source files or performed writes")
	}
	worker.State = container.StateExited
	stopped := Prepare(t.Context(), os.Environ(), Discover([]container.Summary{web, worker}, ""))
	if !slices.Equal(stopped[0].Services, []string{"web"}) {
		t.Fatal("inactive profile service would be started unexpectedly")
	}
	expectedDir, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range calls(t, log) {
		actualDir, err := os.Stat(c.Directory)
		if err != nil {
			t.Fatal(err)
		}
		if c.Host != "tcp://bound-daemon:2375" || c.Context != "" || !os.SameFile(actualDir, expectedDir) {
			t.Fatal("command escaped the bound daemon or directory", c)
		}
	}
	other := labeledProject("other", dir)
	if got := Discover([]container.Summary{web, other}, "demo"); len(got) != 1 || got[0].Name != "demo" {
		t.Fatal(got)
	}
	worker.Labels[projectLabel+".working_dir"] = "/changed"
	if got := Discover([]container.Summary{web, worker}, ""); got[0].Issue == "" {
		t.Fatal("inconsistent project metadata accepted")
	}
	delete(web.Labels, projectLabel+".config_files")
	if got := Discover([]container.Summary{web}, ""); got[0].Issue == "" {
		t.Fatal("missing Compose labels accepted")
	}
}

func TestRunSkipsUpAfterFailedPullAndContinuesOtherProjects(t *testing.T) {
	dir, _, log := composeFixture(t)
	t.Setenv("DP_COMPOSE_FAIL_PROJECT", "a")
	projects := Prepare(t.Context(), os.Environ(), Discover([]container.Summary{labeledProject("a", dir), labeledProject("b", dir)}, ""))
	states := map[string]Event{}
	for event := range Run(t.Context(), t.Context(), os.Environ(), projects) {
		states[event.Project] = event
	}
	if states["a"].State != "failed" || !strings.Contains(states["a"].Output, "registry denied") || states["b"].State != "succeeded" {
		t.Fatal(states)
	}
	if !strings.Contains(states["b"].Output, "[1/2] Pull images\n$ docker compose pull") || !strings.Contains(states["b"].Output, "[2/2] Start updated services\n$ docker compose up -d") || !strings.Contains(states["b"].Output, "Update complete.") {
		t.Fatal("Compose phase headings or command output were lost", states["b"].Output)
	}
	if got := mutations(calls(t, log)); !slices.Equal(got, []string{"pull a", "pull b", "up b"}) {
		t.Fatal(got)
	}
	for _, c := range calls(t, log) {
		if slices.Contains(c.Args, "up") && (!slices.Contains(c.Args, "--no-build") || !slices.Contains(c.Args, "never")) {
			t.Fatal("up could build or pull unexpectedly")
		}
	}
}

func TestPrepareIncludesDependenciesWithoutUnrelatedProfileServices(t *testing.T) {
	dir, model, _ := composeFixture(t)
	config := `{"services":{"web":{"image":"web:latest","depends_on":{"migrate":{"condition":"service_completed_successfully"}}},"migrate":{"image":"migrate:latest","profiles":["tools"]},"debug":{"image":"debug:latest","profiles":["tools"]}}}`
	if err := os.WriteFile(model, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	projects := Prepare(t.Context(), os.Environ(), Discover([]container.Summary{labeledProject("demo", dir)}, ""))
	if projects[0].Issue != "" || !slices.Equal(projects[0].Services, []string{"migrate", "web"}) || !slices.Equal(projects[0].Images, []string{"migrate:latest", "web:latest"}) {
		t.Fatal(projects)
	}
}

func TestChangedConfigurationPreventsContainerRecreation(t *testing.T) {
	for _, when := range []string{"before pull", "after pull"} {
		t.Run(when, func(t *testing.T) {
			dir, model, log := composeFixture(t)
			if when == "after pull" {
				t.Setenv("DP_COMPOSE_EDIT_ON_PULL", "1")
			}
			projects := Prepare(t.Context(), os.Environ(), Discover([]container.Summary{labeledProject("demo", dir)}, ""))
			if when == "before pull" {
				if err := os.WriteFile(model, []byte(strings.ReplaceAll(testModel, "web:latest", "web:changed")), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var last Event
			for event := range Run(t.Context(), t.Context(), os.Environ(), projects) {
				last = event
			}
			if last.State != "failed" || !strings.Contains(last.Output, "configuration changed") {
				t.Fatal(last)
			}
			got := mutations(calls(t, log))
			if when == "before pull" && len(got) != 0 || when == "after pull" && !slices.Equal(got, []string{"pull demo"}) {
				t.Fatal(got)
			}
		})
	}
}

func TestSSHExecutionBindsRootlessSocketAndQuotesArguments(t *testing.T) {
	dir, _, log := composeFixture(t)
	t.Setenv("DOCKER_HOST", "ssh://me@server:2222/run/user/1000/docker.sock")
	t.Setenv("DP_COMPOSE_REAL_DIR", dir)
	p := labeledProject("remote", "/srv/dp-project")
	projects := Prepare(t.Context(), os.Environ(), Discover([]container.Summary{p}, ""))
	if len(projects) != 1 || !projects[0].Remote || projects[0].Issue != "" {
		t.Fatal(projects)
	}
	for range Run(t.Context(), t.Context(), os.Environ(), projects) {
	}
	for _, c := range calls(t, log) {
		if c.Host != "unix:///run/user/1000/docker.sock" || c.Context != "" {
			t.Fatal("remote Compose used the wrong daemon", c)
		}
	}
	value := "/tmp/a ' quote; $(printf injected)"
	output, err := exec.Command("/bin/sh", "-c", "printf '%s' "+quote(value)).Output()
	if err != nil || string(output) != value {
		t.Fatalf("remote quoting changed argument: %q, %v", output, err)
	}
}

func TestExecutionHostCanBeChosenWhenLocalAndSSHPathsOverlap(t *testing.T) {
	dir, _, _ := composeFixture(t)
	t.Setenv("DOCKER_HOST", "ssh://me@server/run/user/1000/docker.sock")
	projects := Discover([]container.Summary{labeledProject("demo", dir)}, "")
	automatic := Prepare(t.Context(), os.Environ(), projects)
	if automatic[0].Issue != "" || automatic[0].Remote {
		t.Fatal("available local files should be the default", automatic)
	}
	projects[0].Location = "ssh"
	remote := Prepare(t.Context(), os.Environ(), projects)
	if remote[0].Issue != "" || !remote[0].Remote {
		t.Fatal("explicit SSH selection was ignored", remote)
	}
	remote[0].Location = "local"
	local := Prepare(t.Context(), os.Environ(), remote)
	if local[0].Issue != "" || local[0].Remote {
		t.Fatal("explicit local selection was ignored", local)
	}
}

func TestCancellationStopsRemainingProjectsAndReportsUnknownUp(t *testing.T) {
	for _, step := range []string{"pull", "up"} {
		t.Run(step, func(t *testing.T) {
			dir, _, log := composeFixture(t)
			entered := filepath.Join(dir, "entered")
			t.Setenv("DP_COMPOSE_BLOCK_STEP", step)
			t.Setenv("DP_COMPOSE_ENTERED", entered)
			projects := Prepare(t.Context(), os.Environ(), Discover([]container.Summary{labeledProject("a", dir), labeledProject("b", dir)}, ""))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			events := Run(ctx, t.Context(), os.Environ(), projects)
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
			for {
				if _, err := os.Stat(entered); err == nil {
					break
				}
				select {
				case <-deadline.C:
					t.Fatal("command did not start")
				case <-time.After(10 * time.Millisecond):
				}
			}
			cancel()
			states := map[string]string{}
			for event := range events {
				states[event.Project] = event.State
			}
			want := "cancelled"
			if step == "up" {
				want = "unknown"
			}
			if states["a"] != want || states["b"] != "cancelled" {
				t.Fatal(states)
			}
			for _, mutation := range mutations(calls(t, log)) {
				if strings.HasSuffix(mutation, " b") {
					t.Fatal("cancelled project was started")
				}
			}
		})
	}
}

func TestOutputAndClosedViewAreBounded(t *testing.T) {
	var out limitedOutput
	out.limit = 8192
	data := []byte(strings.Repeat("x", 20000) + "last message")
	if n, err := out.Write(data); err != nil || n != len(data) || len(out.String()) > 8192 || !strings.HasSuffix(out.String(), "last message") {
		t.Fatal("output tail lost its bounds")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	projects := make([]Project, 200)
	for i := range projects {
		projects[i].Issue = "blocked"
	}
	select {
	case _, ok := <-Run(ctx, ctx, nil, projects):
		if ok {
			t.Fatal("closed view emitted work")
		}
	case <-time.After(time.Second):
		t.Fatal("closed view leaked its producer")
	}
}

func TestPrepareWithInstalledComposeConfigurationParser(t *testing.T) {
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("Docker CLI unavailable")
	}
	if err := exec.Command(docker, "compose", "version", "--short").Run(); err != nil {
		t.Skip("Compose plugin unavailable")
	}
	dir := t.TempDir()
	for file, data := range map[string]string{
		"compose.yaml":  "services:\n  web:\n    image: ${IMAGE_TAG}\n  worker:\n    image: alpine:3.20\n    profiles: [jobs]\n",
		"override.yaml": "services:\n  web:\n    labels:\n      test-update: true\n",
		".env.custom":   "IMAGE_TAG=nginx:alpine\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DOCKER_HOST", "unix:///nonexistent-compose-config-test.sock")
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("COMPOSE_PROFILES", "")
	projects := Prepare(t.Context(), os.Environ(), Discover([]container.Summary{labeledProject("config-test", dir)}, ""))
	if projects[0].Issue != "" || !slices.Equal(projects[0].Services, []string{"web"}) || !slices.Equal(projects[0].Images, []string{"nginx:alpine"}) {
		t.Fatal(projects)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env.custom"), []byte("IMAGE_TAG=nginx:alpine\nCOMPOSE_PROFILES=jobs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv("COMPOSE_PROFILES"); err != nil {
		t.Fatal(err)
	}
	projects = Prepare(t.Context(), os.Environ(), Discover([]container.Summary{labeledProject("config-test", dir)}, ""))
	if projects[0].Issue != "" || !slices.Equal(projects[0].Services, []string{"web", "worker"}) {
		t.Fatal("profiles from env files were lost", projects)
	}
}
