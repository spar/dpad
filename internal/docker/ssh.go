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
	"strings"
	"sync"
	"time"
)

const defaultRemoteSocket = "/var/run/docker.sock"

// sshCommand builds the ssh process; tests replace it with a fake.
var sshCommand = func(args ...string) *exec.Cmd { return exec.Command("ssh", args...) }

type sshTarget struct{ user, host, port, socket string }

// parseSSHHost accepts ssh://[user@]host[:port][/socket/path].
func parseSSHHost(raw string) (sshTarget, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return sshTarget{}, err
	}
	if u.Scheme != "ssh" {
		return sshTarget{}, fmt.Errorf("expected an ssh:// host, got %q", raw)
	}
	t := sshTarget{socket: defaultRemoteSocket}
	if u.User != nil {
		if _, ok := u.User.Password(); ok {
			return sshTarget{}, errors.New("ssh host: plain-text passwords are not supported")
		}
		t.user = u.User.Username()
	}
	t.host, t.port = u.Hostname(), u.Port()
	switch {
	case t.host == "":
		return sshTarget{}, fmt.Errorf("ssh host %q has no hostname", raw)
	case u.RawQuery != "" || u.Fragment != "":
		return sshTarget{}, fmt.Errorf("ssh host %q: query and fragment are not supported", raw)
	}
	if u.Path != "" {
		if !strings.HasPrefix(u.Path, "/") || strings.HasSuffix(u.Path, "/") {
			return sshTarget{}, errors.New("ssh socket path must name an absolute socket file")
		}
		// OpenSSH expands ${...} locally before parsing forwarding paths.
		if strings.Contains(u.Path, "${") || strings.ContainsAny(u.Path, "\x00\r\n") {
			return sshTarget{}, errors.New("ssh socket path cannot contain environment expansions or NUL/newline characters")
		}
		t.socket = u.Path
	}
	return t, nil
}

// sshTunnel forwards a private local socket to the remote daemon socket over
// one ssh connection. Every API request is a channel on that connection, so
// per-container stats streams don't each start an ssh process.
type sshTunnel struct {
	target sshTarget
	dir    string
	socket string

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
	target, err := parseSSHHost(host)
	if err != nil {
		return nil, err
	}
	// The socket grants root-equivalent access to the server, so it lives in a
	// directory only this user can enter.
	dir, err := os.MkdirTemp("", "dn-ssh-")
	if err != nil {
		return nil, err
	}
	t := &sshTunnel{target: target, dir: dir, socket: filepath.Join(dir, "docker.sock")}
	// The first start may prompt for a password or host key, which ssh does on
	// the terminal; the TUI hasn't taken it over yet.
	if t.proc, err = t.start(context.Background(), false); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return t, nil
}

func (t *sshTunnel) args(batch bool) []string {
	args := []string{"-n", "-N", "-T",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "StreamLocalBindUnlink=yes",
		"-o", "ConnectTimeout=10",
		// Notice a dead connection (sleep, network change) within 15s so the
		// next request can start a new one.
		"-o", "ServerAliveInterval=5",
		"-o", "ServerAliveCountMax=3",
	}
	if batch {
		args = append(args, "-o", "BatchMode=yes")
	}
	if t.target.user != "" {
		args = append(args, "-l", t.target.user)
	}
	if t.target.port != "" {
		args = append(args, "-p", t.target.port)
	}
	// OpenSSH uses colons as field separators and backslashes as escapes.
	escape := strings.NewReplacer("\\", "\\\\", ":", "\\:")
	return append(args, "-L", escape.Replace(t.socket)+":"+escape.Replace(t.target.socket), "--", t.target.host)
}

// start runs ssh and waits until it listens on the local socket or exits.
func (t *sshTunnel) start(ctx context.Context, batch bool) (*sshProcess, error) {
	// A socket left by a dead ssh would look ready before the new one binds.
	_ = os.Remove(t.socket)
	p := &sshProcess{cmd: sshCommand(t.args(batch)...), done: make(chan struct{})}
	p.cmd.Stderr = &p.stderr
	if err := p.cmd.Start(); err != nil {
		return nil, fmt.Errorf("ssh tunnel to %s: %w", t.target.host, err)
	}
	go func() {
		_ = p.cmd.Wait()
		close(p.done)
	}()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(t.socket); err == nil {
			return p, nil
		}
		select {
		case <-p.done:
			msg := strings.TrimSpace(p.stderr.String())
			if msg == "" {
				msg = p.cmd.ProcessState.String()
			}
			return nil, fmt.Errorf("ssh tunnel to %s: %s", t.target.host, msg)
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
	return d.DialContext(ctx, "unix", t.socket)
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
	return os.RemoveAll(t.dir)
}
