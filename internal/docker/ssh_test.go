package docker

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseSSHHost(t *testing.T) {
	tests := []struct {
		in   string
		want sshTarget
		err  string
	}{
		{in: "ssh://server", want: sshTarget{host: "server"}},
		{in: "ssh://me@server:2222", want: sshTarget{user: "me", host: "server", port: "2222"}},
		{in: "ssh://me@[::1]:22", want: sshTarget{user: "me", host: "::1", port: "22"}},
		{in: "ssh://me:secret@server", err: "passwords"},
		{in: "ssh://server/var/run/docker.sock", err: "extra path"},
		{in: "ssh://server?x=1", err: "query"},
		{in: "ssh://", err: "no hostname"},
		{in: "tcp://server", err: "expected an ssh://"},
	}
	for _, tt := range tests {
		got, err := parseSSHHost(tt.in)
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("parseSSHHost(%q) error = %v, want %q", tt.in, err, tt.err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("parseSSHHost(%q) = %+v, %v; want %+v", tt.in, got, err, tt.want)
		}
	}
}

func TestSSHArgs(t *testing.T) {
	tun := &sshTunnel{target: sshTarget{user: "me", host: "-oProxyCommand=x", port: "2222"}, socket: "/tmp/d/docker.sock"}
	args := tun.args(false)
	if slices.Contains(args, "BatchMode=yes") {
		t.Fatal("first start must allow prompts")
	}
	tail := []string{"-l", "me", "-p", "2222", "-L", "/tmp/d/docker.sock:/var/run/docker.sock", "--", "-oProxyCommand=x"}
	if !slices.Equal(args[len(args)-len(tail):], tail) {
		t.Fatalf("args = %q", args)
	}
	if !slices.Contains(tun.args(true), "BatchMode=yes") {
		t.Fatal("restart can prompt while the TUI owns the terminal")
	}
}

// useFakeSSH makes the tunnel run TestHelperSSH instead of ssh. mode is
// "serve" (forward to a fake daemon) or "fail".
func useFakeSSH(t *testing.T, mode string) (log string) {
	t.Helper()
	log = t.TempDir() + "/starts"
	orig := sshCommand
	sshCommand = func(args ...string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestHelperSSH$", "--"}, args...)...)
		cmd.Env = append(os.Environ(), "DN_FAKE_SSH="+mode, "DN_FAKE_SSH_LOG="+log)
		return cmd
	}
	t.Cleanup(func() { sshCommand = orig })
	return log
}

// TestHelperSSH is the fake ssh: it records its arguments, then listens on
// the -L socket and answers like a daemon.
func TestHelperSSH(t *testing.T) {
	mode := os.Getenv("DN_FAKE_SSH")
	if mode == "" {
		return
	}
	args := os.Args[slices.Index(os.Args, "--")+1:]
	f, _ := os.OpenFile(os.Getenv("DN_FAKE_SSH_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	fmt.Fprintln(f, strings.Join(args, " "))
	f.Close()
	if mode == "fail" {
		fmt.Fprintln(os.Stderr, "me@server: Permission denied (publickey).")
		os.Exit(255)
	}
	local, _, _ := strings.Cut(args[slices.Index(args, "-L")+1], ":")
	l, err := net.Listen("unix", local)
	if err != nil {
		os.Exit(1)
	}
	_ = http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Api-Version", "1.47")
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			fmt.Fprint(w, `[{"Id":"abc","Names":["/web"]}]`)
			return
		}
		fmt.Fprint(w, "OK")
	}))
	os.Exit(0)
}

func starts(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func TestSSHTunnelServesAndRestarts(t *testing.T) {
	log := useFakeSSH(t, "serve")
	t.Setenv("DOCKER_HOST", "ssh://me@server:2222")
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.ConnectionLabel() != "ssh://server:2222" {
		t.Errorf("label = %q", c.ConnectionLabel())
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	items, err := c.Containers(ctx, true)
	if err != nil || len(items) != 1 || items[0].ID != "abc" {
		t.Fatalf("containers over tunnel = %v, %v", items, err)
	}

	// The connection drops (sleep, network change): the next request starts a
	// new ssh, without prompting.
	p := c.tunnel.proc
	_ = p.cmd.Process.Kill()
	<-p.done
	if err := c.Ping(ctx); err != nil {
		t.Fatal("no recovery after ssh exited:", err)
	}
	s := starts(t, log)
	if len(s) != 2 || strings.Contains(s[0], "BatchMode") || !strings.Contains(s[1], "BatchMode=yes") {
		t.Fatalf("ssh starts = %q", s)
	}

	dir := c.tunnel.dir
	c.Close()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("socket directory left behind")
	}
	if c.tunnel.proc.running() {
		t.Fatal("ssh left running")
	}
	if err := c.Ping(ctx); err == nil {
		t.Fatal("closed tunnel restarted ssh")
	}
}

func TestSSHTunnelReportsSSHError(t *testing.T) {
	useFakeSSH(t, "fail")
	cfg := t.TempDir()
	t.Setenv("DOCKER_CONFIG", cfg)
	t.Setenv("DOCKER_HOST", "")
	writeContext(t, cfg, "home", "ssh://me@server")
	t.Setenv("DOCKER_CONTEXT", "home")
	_, err := New()
	if err == nil || !strings.Contains(err.Error(), "Permission denied (publickey)") {
		t.Fatalf("err = %v", err)
	}
}

func TestSSHTunnelThrottlesFailedRestarts(t *testing.T) {
	useFakeSSH(t, "serve")
	tun, err := startSSHTunnel("ssh://server")
	if err != nil {
		t.Fatal(err)
	}
	defer tun.Close()
	log := useFakeSSH(t, "fail")
	_ = tun.proc.cmd.Process.Kill()
	<-tun.proc.done
	for range 5 {
		if _, err := tun.dial(t.Context(), "unix", ""); err == nil {
			t.Fatal("dial succeeded with ssh failing")
		}
	}
	if s := starts(t, log); len(s) != 1 {
		t.Fatalf("failed restart retried %d times within a second", len(s))
	}
}
