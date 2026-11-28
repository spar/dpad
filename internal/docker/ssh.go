package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/spar/dpad/internal/debuglog"
)

// DefaultRemoteSocket is the daemon socket of an ssh:// endpoint that names none.
const DefaultRemoteSocket = "/var/run/docker.sock"

// forwardTCP forwards a loopback TCP port instead of a Unix socket. OpenSSH's
// -L parser splits a Windows path like C:\Users\... at the drive letter's
// colon and has no escape for it.
var forwardTCP = runtime.GOOS == "windows"

// sshCommand builds the ssh process; tests replace it with a fake.
var sshCommand = func(args ...string) *exec.Cmd { return exec.Command("ssh", args...) }

// SSHTarget is an ssh:// Docker endpoint: the login, the host and port, and
// the daemon socket on that host.
type SSHTarget struct{ User, Host, Port, Socket string }

// ParseSSHHost accepts ssh://[user@]host[:port][/socket/path]. The tunnel
// and remote Compose commands both go through it, so they accept the same hosts.
func ParseSSHHost(raw string) (SSHTarget, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return SSHTarget{}, err
	}
	if u.Scheme != "ssh" {
		return SSHTarget{}, fmt.Errorf("expected an ssh:// host, got %q", raw)
	}
	t := SSHTarget{Socket: DefaultRemoteSocket}
	if u.User != nil {
		if _, ok := u.User.Password(); ok {
			return SSHTarget{}, errors.New("ssh host: plain-text passwords are not supported")
		}
		t.User = u.User.Username()
	}
	t.Host, t.Port = u.Hostname(), u.Port()
	switch {
	case t.Host == "":
		return SSHTarget{}, fmt.Errorf("ssh host %q has no hostname", raw)
	case u.RawQuery != "" || u.Fragment != "":
		return SSHTarget{}, fmt.Errorf("ssh host %q: query and fragment are not supported", raw)
	}
	if u.Path != "" {
		if !strings.HasPrefix(u.Path, "/") || strings.HasSuffix(u.Path, "/") {
			return SSHTarget{}, errors.New("ssh socket path must name an absolute socket file")
		}
		// OpenSSH expands ${...} locally before parsing forwarding paths.
		if strings.Contains(u.Path, "${") || strings.ContainsAny(u.Path, "\x00\r\n") {
			return SSHTarget{}, errors.New("ssh socket path cannot contain environment expansions or NUL/newline characters")
		}
		t.Socket = u.Path
	}
	return t, nil
}

// sshTunnel forwards a private local socket (a loopback port on Windows) to
// the remote daemon socket over one ssh connection. Every API request is a
// channel on that connection, so per-container stats streams don't each start
// an ssh process.
type sshTunnel struct {
	target  SSHTarget
	network string // "unix" or "tcp"
	local   string // the socket path, or 127.0.0.1:port
	dir     string // holds the socket; empty for tcp

	mu       sync.Mutex
	proc     *sshProcess
	starting chan struct{} // closed when a restart finishes
	err      error         // result of the last start
	failedAt time.Time
	closed   bool
}

type sshProcess struct {
	cmd    *exec.Cmd
	done   chan struct{} // closed when ssh exits
	stderr bytes.Buffer  // read only after done
}

func (p *sshProcess) running() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func startSSHTunnel(host string) (*sshTunnel, error) {
	return startSSHTunnelContext(context.Background(), host)
}

func startSSHTunnelContext(ctx context.Context, host string) (*sshTunnel, error) {
	target, err := ParseSSHHost(host)
	if err != nil {
		return nil, err
	}
	t := &sshTunnel{target: target}
	if forwardTCP {
		// Unlike the Unix socket, a loopback port is open to every user on
		// this machine, as Docker's own tcp://localhost endpoints are.
		if t.local, err = freeLoopbackPort(); err != nil {
			return nil, err
		}
		t.network = "tcp"
	} else {
		// The socket grants root-equivalent access to the server, so it lives
		// in a directory only this user can enter.
		if t.dir, err = os.MkdirTemp("", "dp-ssh-"); err != nil {
			return nil, err
		}
		t.network, t.local = "unix", filepath.Join(t.dir, "docker.sock")
	}
	// The first start may prompt for a password or host key, which ssh does on
	// the terminal; the TUI hasn't taken it over yet.
	if t.proc, err = t.start(ctx, false); err != nil {
		_ = t.removeDir()
		return nil, err
	}
	return t, nil
}

func freeLoopbackPort() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := l.Addr().String()
	return addr, l.Close()
}

// host is the endpoint the API client is configured with. Requests go
// through dial, which this only labels.
func (t *sshTunnel) host() string { return t.network + "://" + t.local }

func (t *sshTunnel) removeDir() error {
	if t.dir == "" {
		return nil
	}
	return os.RemoveAll(t.dir)
}

// Options are the ssh options every connection dpad makes uses: the login,
// the port, a connect timeout, and keepalives that notice a dead connection
// (sleep, network change) within 15s so the next request can start a new
// one. batch turns off prompts, for when the TUI owns the terminal.
func (t SSHTarget) Options(batch bool) []string {
	args := []string{
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=5",
		"-o", "ServerAliveCountMax=3",
	}
	if batch {
		args = append(args, "-o", "BatchMode=yes")
	}
	if t.User != "" {
		args = append(args, "-l", t.User)
	}
	if t.Port != "" {
		args = append(args, "-p", t.Port)
	}
	return args
}

func (t *sshTunnel) args(batch bool) []string {
	args := []string{"-n", "-N", "-T", "-o", "ExitOnForwardFailure=yes"}
	// OpenSSH uses colons as field separators and backslashes as escapes.
	escape := strings.NewReplacer("\\", "\\\\", ":", "\\:")
	local := t.local
	if t.network == "unix" {
		args = append(args, "-o", "StreamLocalBindUnlink=yes")
		local = escape.Replace(local)
	}
	args = append(args, t.target.Options(batch)...)
	return append(args, "-L", local+":"+escape.Replace(t.target.Socket), "--", t.target.Host)
}

// listening reports whether ssh has bound the local end.
func (t *sshTunnel) listening() bool {
	if t.network == "unix" {
		_, err := os.Stat(t.local)
		return err == nil
	}
	// The probe opens and closes one channel to the daemon socket.
	c, err := net.DialTimeout("tcp", t.local, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// start runs ssh and waits until it listens on the local socket or exits.
func (t *sshTunnel) start(ctx context.Context, batch bool) (*sshProcess, error) {
	if t.network == "unix" {
		// A socket left by a dead ssh would look ready before the new one binds.
		_ = os.Remove(t.local)
	}
	p := &sshProcess{cmd: sshCommand(t.args(batch)...), done: make(chan struct{})}
	p.cmd.Stderr = &p.stderr
	began := time.Now()
	debuglog.Info("ssh tunnel starting", "host", t.target.Host, "batch", batch)
	if err := p.cmd.Start(); err != nil {
		return nil, fmt.Errorf("ssh tunnel to %s: %w", t.target.Host, err)
	}
	go func() {
		err := p.cmd.Wait()
		debuglog.Info("ssh tunnel exited", "host", t.target.Host, "after", time.Since(began).Round(time.Millisecond), "err", err, "stderr", strings.TrimSpace(p.stderr.String()))
		close(p.done)
	}()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if t.listening() {
			debuglog.Info("ssh tunnel ready", "host", t.target.Host, "ms", time.Since(began).Milliseconds())
			return p, nil
		}
		select {
		case <-p.done:
			msg := strings.TrimSpace(p.stderr.String())
			if msg == "" {
				msg = p.cmd.ProcessState.String()
			}
			return nil, fmt.Errorf("ssh tunnel to %s: %s", t.target.Host, msg)
		case <-ctx.Done():
			_ = p.cmd.Process.Kill()
			<-p.done
			return nil, ctx.Err()
		case <-tick.C:
		}
	}
}

// ensure restarts ssh if it has exited. Concurrent callers share one restart,
// and a failed restart is retried at most once a second.
func (t *sshTunnel) ensure(ctx context.Context) error {
	t.mu.Lock()
	switch {
	case t.closed:
		t.mu.Unlock()
		return net.ErrClosed
	case t.starting == nil && t.proc != nil && t.proc.running():
		t.mu.Unlock()
		return nil
	case t.starting == nil && t.err != nil && time.Since(t.failedAt) < time.Second:
		err := t.err
		t.mu.Unlock()
		return err
	case t.starting == nil:
		t.starting = make(chan struct{})
		go t.restart(t.starting)
	}
	starting := t.starting
	t.mu.Unlock()

	select {
	case <-starting:
	case <-ctx.Done():
		return ctx.Err()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

func (t *sshTunnel) restart(starting chan struct{}) {
	// No prompts: the TUI owns the terminal by now.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p, err := t.start(ctx, true)

	t.mu.Lock()
	defer t.mu.Unlock()
	t.starting = nil
	t.err = err
	switch {
	case err != nil:
		t.failedAt = time.Now()
	case t.closed:
		_ = p.cmd.Process.Kill()
	default:
		t.proc = p
	}
	close(starting)
}

func (t *sshTunnel) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	if err := t.ensure(ctx); err != nil {
		return nil, err
	}
	var d net.Dialer
	return d.DialContext(ctx, t.network, t.local)
}

func (t *sshTunnel) Close() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	t.closed = true
	p := t.proc
	t.mu.Unlock()
	if p != nil && p.running() {
		_ = p.cmd.Process.Kill()
		<-p.done
	}
	return t.removeDir()
}
