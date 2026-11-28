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
		want SSHTarget
		err  string
	}{
		{in: "ssh://server", want: SSHTarget{Host: "server", Socket: DefaultRemoteSocket}},
		{in: "ssh://me@server:2222", want: SSHTarget{User: "me", Host: "server", Port: "2222", Socket: DefaultRemoteSocket}},
		{in: "ssh://me@[::1]:22", want: SSHTarget{User: "me", Host: "::1", Port: "22", Socket: DefaultRemoteSocket}},
		{in: "ssh://server/var/run/docker.sock", want: SSHTarget{Host: "server", Socket: DefaultRemoteSocket}},
		{in: "ssh://me@[::1]:2222/run/user/1000/docker.sock", want: SSHTarget{User: "me", Host: "::1", Port: "2222", Socket: "/run/user/1000/docker.sock"}},
		{in: "ssh://server/run/custom%20socket%3Aone%5Ctwo.sock", want: SSHTarget{Host: "server", Socket: "/run/custom socket:one\\two.sock"}},
		{in: "ssh://me:secret@server", err: "passwords"},
		{in: "ssh://server/", err: "absolute socket file"},
		{in: "ssh://server/run/user/1000/", err: "absolute socket file"},
		{in: "ssh://server/run/%00docker.sock", err: "NUL/newline"},
		{in: "ssh://server/run/%0Adocker.sock", err: "NUL/newline"},
		{in: "ssh://server/run/%0Ddocker.sock", err: "NUL/newline"},
		{in: "ssh://server/run/${UID}/docker.sock", err: "environment expansions"},
		{in: "ssh://server/run/%24%7BUID%7D/docker.sock", err: "environment expansions"},
		{in: "ssh://server/run/docker.sock?x=1", err: "query"},
		{in: "ssh://server/run/docker.sock#fragment", err: "fragment"},
		{in: "ssh://server/run/%zz", err: "invalid URL escape"},
		{in: "ssh://server?x=1", err: "query"},
		{in: "ssh://", err: "no hostname"},
		{in: "tcp://server", err: "expected an ssh://"},
	}
	for _, tt := range tests {
		got, err := ParseSSHHost(tt.in)
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("ParseSSHHost(%q) error = %v, want %q", tt.in, err, tt.err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("ParseSSHHost(%q) = %+v, %v; want %+v", tt.in, got, err, tt.want)
		}
	}
}

func TestSSHArgs(t *testing.T) {
	tun := &sshTunnel{target: SSHTarget{User: "me", Host: "-oProxyCommand=x", Port: "2222", Socket: DefaultRemoteSocket}, network: "unix", local: "/tmp/d/docker.sock"}
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
	tun.target.Socket = "/run/custom socket:one\\two.sock"
	for _, batch := range []bool{false, true} {
		args := tun.args(batch)
		if got := args[slices.Index(args, "-L")+1]; got != `/tmp/d/docker.sock:/run/custom socket\:one\\two.sock` {
			t.Fatalf("forwarding path = %q", got)
		}
	}
}

func TestSSHArgsTCP(t *testing.T) {
	tun := &sshTunnel{target: SSHTarget{Host: "server", Socket: DefaultRemoteSocket}, network: "tcp", local: "127.0.0.1:49152"}
	args := tun.args(false)
	if got := args[slices.Index(args, "-L")+1]; got != "127.0.0.1:49152:/var/run/docker.sock" {
		t.Fatalf("forwarding spec = %q", got)
	}
	if slices.Contains(args, "StreamLocalBindUnlink=yes") {
		t.Fatal("a TCP forward has no socket file to unlink")
	}
}

func TestSSHTunnelWindowsPathNeverReachesSSH(t *testing.T) {
	useFakeSSH(t, "serve")
	setForwardTCP(t, true)
	tun, err := startSSHTunnel("ssh://server")
	if err != nil {
		t.Fatal(err)
	}
	defer tun.Close()
	if tun.dir != "" || !strings.HasPrefix(tun.local, "127.0.0.1:") || tun.host() != "tcp://"+tun.local {
		t.Fatalf("tunnel = %+v", tun)
	}
}

func setForwardTCP(t *testing.T, on bool) {
	t.Helper()
	orig := forwardTCP
	forwardTCP = on
	t.Cleanup(func() { forwardTCP = orig })
}

// useFakeSSH makes the tunnel run TestHelperSSH instead of ssh. mode is
// "serve" (forward to a fake daemon) or "fail".
func useFakeSSH(t *testing.T, mode string) (log string) {
	t.Helper()
	log = t.TempDir() + "/starts"
	orig := sshCommand
	sshCommand = func(args ...string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestHelperSSH$", "--"}, args...)...)
		cmd.Env = append(os.Environ(), "DP_FAKE_SSH="+mode, "DP_FAKE_SSH_LOG="+log)
		return cmd
	}
	t.Cleanup(func() { sshCommand = orig })
	return log
}

// TestHelperSSH is the fake ssh: it records its arguments, then listens on
// the -L socket and answers like a daemon.
func TestHelperSSH(t *testing.T) {
	mode := os.Getenv("DP_FAKE_SSH")
	if mode == "" {
		return
	}
	args := os.Args[slices.Index(os.Args, "--")+1:]
	f, _ := os.OpenFile(os.Getenv("DP_FAKE_SSH_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	fmt.Fprintln(f, strings.Join(args, " "))
	f.Close()
	if mode == "fail" {
		fmt.Fprintln(os.Stderr, "me@server: Permission denied (publickey).")
		os.Exit(255)
	}
	spec := args[slices.Index(args, "-L")+1]
	network, local := "unix", spec[:strings.Index(spec, ":")]
	if strings.HasPrefix(spec, "127.0.0.1:") {
		network, local = "tcp", spec[:strings.Index(spec[len("127.0.0.1:"):], ":")+len("127.0.0.1:")]
	}
	l, err := net.Listen(network, local)
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
	for _, tcp := range []bool{false, true} {
		for _, socket := range []string{"", "/run/user/1000/docker.sock"} {
			for _, fromContext := range []bool{false, true} {
				t.Run(fmt.Sprintf("tcp=%t/socket=%s/context=%t", tcp, socket, fromContext), func(t *testing.T) {
					setForwardTCP(t, tcp)
					testSSHTunnelServesAndRestarts(t, socket, fromContext)
				})
			}
		}
	}
}

func testSSHTunnelServesAndRestarts(t *testing.T, socket string, fromContext bool) {
	t.Helper()
	log := useFakeSSH(t, "serve")
	endpoint := "ssh://me@server:2222" + socket
	label := "ssh://server:2222" + socket
	t.Setenv("DOCKER_HOST", endpoint)
	if fromContext {
		cfg := t.TempDir()
		t.Setenv("DOCKER_CONFIG", cfg)
		t.Setenv("DOCKER_HOST", "")
		t.Setenv("DOCKER_CONTEXT", "home")
		writeContext(t, cfg, "home", endpoint)
		label = "home @ " + label
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.ConnectionLabel() != label {
		t.Errorf("label = %q", c.ConnectionLabel())
	}
	if !slices.Contains(c.CLIEnvironment(), "DOCKER_HOST="+endpoint) || !slices.Contains(c.CLIEnvironment(), "DOCKER_CONTEXT=") {
		t.Fatal("CLI environment lost the original SSH user/socket or retained context selection")
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
	if socket == "" {
		socket = DefaultRemoteSocket
	}
	for _, start := range s {
		if !strings.Contains(start, "-L "+c.tunnel.local+":"+socket+" -- server") {
			t.Fatalf("ssh did not forward to %q: %s", socket, start)
		}
	}

	dir := c.tunnel.dir
	c.Close()
	if _, err := os.Stat(dir); dir != "" && !os.IsNotExist(err) {
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
