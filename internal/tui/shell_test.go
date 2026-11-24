package tui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"

	"dpad/internal/docker"
)

func shellDockerFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return path
}

func TestPrepareShellChecksStateAndPassesLiteralArguments(t *testing.T) {
	path := shellDockerFixture(t)
	f := &fakeClient{
		inspect: container.InspectResponse{State: &container.State{Running: true}},
		cliEnv:  []string{"DOCKER_HOST=unix:///bound.sock", "DOCKER_CONTEXT="},
	}
	executable := "/bin/sh; echo should-not-run"
	p, err := prepareShell(context.Background(), f, "container-id", executable)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{path, "exec", "-it", "container-id", executable}; !reflect.DeepEqual(p.cmd.Args, want) {
		t.Fatalf("shell arguments = %q, want %q", p.cmd.Args, want)
	}
	if !reflect.DeepEqual(p.cmd.Env, f.cliEnv) {
		t.Fatal("shell did not use the bound client's CLI environment")
	}
	for _, state := range []*container.State{nil, {}, {Running: true, Paused: true}, {Running: true, Restarting: true}} {
		f.inspect.State = state
		if p, err := prepareShell(context.Background(), f, "id", "/bin/sh"); p != nil || err == nil {
			t.Fatalf("non-running state accepted: %+v", state)
		}
	}
	f.inspectErr = errors.New("container removed")
	if _, err := prepareShell(context.Background(), f, "id", "/bin/sh"); !errors.Is(err, f.inspectErr) {
		t.Fatal("inspect error was lost")
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := prepareShell(context.Background(), f, "id", "/bin/sh"); err == nil || !strings.Contains(err.Error(), "docker CLI not found") {
		t.Fatal("missing Docker CLI was not reported")
	}
}

func TestShellStaysOnBoundDaemonAfterContextChanges(t *testing.T) {
	path := shellDockerFixture(t)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' \"$DOCKER_HOST\" \"$DOCKER_CONTEXT\" \"$DOCKER_CONFIG\" \"$DOCKER_API_VERSION\" \"$DOCKER_TLS_VERIFY\" \"$DOCKER_CERT_PATH\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Api-Version", "1.47")
		if strings.HasSuffix(r.URL.Path, "/containers/web/json") {
			fmt.Fprint(w, `{"Id":"web","State":{"Running":true}}`)
			return
		}
		fmt.Fprint(w, "OK")
	}))
	defer daemon.Close()
	host := strings.Replace(daemon.URL, "http://", "tcp://", 1)
	cfg := t.TempDir()
	configPath := filepath.Join(cfg, "config.json")
	sum := sha256.Sum256([]byte("bound"))
	contextDir := filepath.Join(cfg, "contexts", "meta", hex.EncodeToString(sum[:]))
	if err := os.MkdirAll(contextDir, 0o700); err != nil {
		t.Fatal(err)
	}
	metadata := filepath.Join(contextDir, "meta.json")
	if err := os.WriteFile(metadata, []byte(`{"Endpoints":{"docker":{"Host":"`+host+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"currentContext":"bound"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", cfg)
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_API_VERSION", "1.47")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	c, err := docker.New()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// Both changing the saved selection and editing the selected context must
	// leave the shell on the endpoint already used for container inspection.
	if err := os.WriteFile(configPath, []byte(`{"currentContext":"another-daemon"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadata, []byte(`{"Endpoints":{"docker":{"Host":"tcp://127.0.0.1:1"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:2")
	t.Setenv("DOCKER_CONTEXT", "another-daemon")
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	t.Setenv("DOCKER_API_VERSION", "1.48")
	t.Setenv("DOCKER_TLS_VERIFY", "1")
	t.Setenv("DOCKER_CERT_PATH", "/changed/certificates")
	p, err := prepareShell(t.Context(), c, "web", "/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	p.SetStdout(&output)
	if err := p.Run(); err != nil {
		t.Fatal(err)
	}
	if want := strings.Join([]string{host, "", cfg, "1.47", "", "", ""}, "\n"); output.String() != want {
		t.Fatalf("shell connection environment = %q, want %q", output.String(), want)
	}
}

func TestAppShellPromptCapturesTargetAndCanCancelPreparation(t *testing.T) {
	shellDockerFixture(t)
	f := &fakeClient{inspect: container.InspectResponse{State: &container.State{Running: true}}}
	a := newTestApp()
	a.ctx, a.client = context.Background(), f
	a.Update(containersMsg{items: []container.Summary{{ID: "web", Names: []string{"/web"}}}})
	press(a, "/", "w", "e", "b", "enter", "e")
	if a.shell == nil || a.shell.id != "web" || a.shell.input.Value() != "/bin/sh" {
		t.Fatal("shell prompt did not capture selected container")
	}
	if !strings.Contains(ansi.Strip(a.footer()), "enter open") {
		t.Fatal("shell controls are not shown")
	}
	a.shell.input.SetValue(" ")
	_, cmd := a.Update(key("enter"))
	if cmd != nil || a.shell.launching || !a.flashErr {
		t.Fatal("empty executable accepted")
	}
	a.shell.input.SetValue("/bin/bash")
	_, prepare := a.Update(key("enter"))
	if prepare == nil || !a.shell.launching {
		t.Fatal("enter did not prepare shell")
	}
	press(a, "esc")
	_, launch := a.Update(prepare())
	if launch != nil || a.shell != nil || a.tabs[0].Filter() != "web" {
		t.Fatal("cancelled preparation launched a shell or lost list state")
	}
	press(a, "e", "enter")
	// The ready message must belong to the current prompt.
	_, launch = a.Update(shellReadyMsg{prompt: &shellPrompt{}})
	if launch != nil {
		t.Fatal("stale shell request launched a process")
	}
	a.Update(shellReadyMsg{prompt: a.shell, err: errors.New("container stopped")})
	if a.shell == nil || a.shell.launching || !strings.Contains(a.flash, "container stopped") {
		t.Fatal("preparation error did not leave prompt available to retry or cancel")
	}
}

type shellSessionModel struct {
	*App
	ready shellReadyMsg
	done  bool
}

func (m *shellSessionModel) Init() tea.Cmd {
	return func() tea.Msg { return m.ready }
}

func (m *shellSessionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.App.Update(msg)
	if _, ok := msg.(shellDoneMsg); ok {
		m.done = true
		return m, tea.Quit
	}
	return m, cmd
}

func TestShellSessionReturnsToDashboard(t *testing.T) {
	for _, mode := range []string{"success", "failure"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			a := newTestApp()
			a.ctx = ctx
			a.Update(containersMsg{items: []container.Summary{{ID: "web", Names: []string{"/web"}}}})
			a.setFilter("web")
			a.tabs[0].HandleKey("j")
			a.openShell("web", "web")
			m := &shellSessionModel{App: a, ready: shellReadyMsg{prompt: a.shell, process: &shellProcess{cmd: shellHelperCommand(ctx, mode)}}}
			var out bytes.Buffer
			if _, err := tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(&out), tea.WithContext(ctx)).Run(); err != nil {
				t.Fatal(err)
			}
			if !m.done || a.shell != nil || a.tabs[0].Filter() != "web" || a.active != 0 {
				t.Fatal("shell did not return to original dashboard state")
			}
			if id, _, _ := a.tabs[0].Selected(); id != "web" {
				t.Fatal("shell changed container selection")
			}
			if a.flashErr != (mode == "failure") || !strings.Contains(a.flash, "shell /bin/sh in web: exited") {
				t.Fatalf("missing shell completion: %q", a.flash)
			}
			if mode == "failure" && !strings.Contains(a.flash, "not found") {
				t.Fatal("shell failure detail was lost after returning")
			}
		})
	}
}

func TestShellShortcutTargetsDetailsAndContainerRowsOnly(t *testing.T) {
	a := newTestApp()
	a.ctx, a.client = context.Background(), &fakeClient{}
	press(a, "e", "2", "e")
	if a.shell != nil || strings.Contains(a.footer(), "shell") {
		t.Fatal("empty list or Images offered a shell")
	}
	a.Update(containersMsg{items: []container.Summary{composeContainer("web", "shop", "web", "running")}})
	press(a, "4", "e")
	if a.shell != nil {
		t.Fatal("project header opened a shell")
	}
	press(a, "j", "e")
	if a.shell == nil || a.shell.id != "web" {
		t.Fatal("project container row did not open shell prompt")
	}
	press(a, "esc", "d", "e")
	if a.shell == nil || a.shell.id != "web" || a.details == nil {
		t.Fatal("details shell target was not retained")
	}
	press(a, "esc")
	if a.details == nil {
		t.Fatal("cancelling shell left details")
	}
	a.details.Close()
	press(a, "esc", "3")
	a.Update(statsBatchMsg{{Stats: docker.Stats{ID: "stats-id", Name: "stats"}}})
	press(a, "e")
	if a.shell == nil || a.shell.id != "stats-id" {
		t.Fatal("Stats container did not open shell prompt")
	}
}

func shellHelperCommand(ctx context.Context, mode string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestShellProcessHelper$")
	cmd.Env = append(os.Environ(), "DP_SHELL_TEST_HELPER="+mode)
	return cmd
}

func TestShellProcessHelper(t *testing.T) {
	switch os.Getenv("DP_SHELL_TEST_HELPER") {
	case "success":
		fmt.Fprintln(os.Stdout, "shell session finished")
		os.Exit(0)
	case "failure":
		fmt.Fprintln(os.Stderr, "\x1b[31mexec: /bin/bash: not found\x1b[0m")
		os.Exit(127)
	case "wait":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
}

func TestShellProcessStdioErrorsAndCancellation(t *testing.T) {
	var out, stderr bytes.Buffer
	p := &shellProcess{cmd: shellHelperCommand(context.Background(), "failure")}
	p.SetStdin(strings.NewReader(""))
	p.SetStdout(&out)
	p.SetStderr(&stderr)
	err := p.Run()
	if err == nil || !strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "\x1b") || !strings.Contains(stderr.String(), "not found") {
		t.Fatalf("shell error not captured and forwarded: %v / %q", err, stderr.String())
	}
	var tail shellProcess
	for _, data := range []string{strings.Repeat("x", 10000), "last error"} {
		if n, err := tail.Write([]byte(data)); err != nil || n != len(data) {
			t.Fatalf("stderr capture write = %d, %v", n, err)
		}
	}
	if tail.stderr.Len() > 4096 || !strings.HasSuffix(tail.stderr.String(), "last error") {
		t.Fatal("stderr capture is not bounded")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	p = &shellProcess{cmd: shellHelperCommand(ctx, "wait")}
	if err := p.Run(); err == nil || ctx.Err() == nil {
		t.Fatal("shell process did not respect cancellation")
	}
}
