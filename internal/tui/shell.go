package tui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type shellPrompt struct {
	id, name  string
	input     textinput.Model
	launching bool
}

type shellReadyMsg struct {
	prompt  *shellPrompt
	process *shellProcess
	err     error
}

type shellDoneMsg struct {
	name, executable string
	err              error
}

func (a *App) openShell(id, name string) tea.Cmd {
	input := textinput.New()
	input.Prompt = "shell: "
	input.SetValue("/bin/sh")
	input.CursorEnd()
	input.SetWidth(max(a.width-38, 1))
	a.shell = &shellPrompt{id: id, name: name, input: input}
	return a.shell.input.Focus()
}

func (a *App) updateShell(msg tea.KeyPressMsg) tea.Cmd {
	p := a.shell
	switch msg.String() {
	case "esc":
		a.shell = nil
		return nil
	case "enter":
		if p.launching {
			return nil
		}
		executable := strings.TrimSpace(p.input.Value())
		if executable == "" {
			a.flash, a.flashErr = "enter a shell executable, for example /bin/sh", true
			return nil
		}
		p.input.SetValue(executable)
		p.launching = true
		return func() tea.Msg {
			process, err := prepareShell(a.ctx, a.client, p.id, executable)
			return shellReadyMsg{prompt: p, process: process, err: err}
		}
	}
	if p.launching {
		return nil
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return cmd
}

func prepareShell(ctx context.Context, c dockerClient, id, executable string) (*shellProcess, error) {
	path, err := exec.LookPath("docker")
	if err != nil {
		return nil, fmt.Errorf("docker CLI not found in PATH: %w", err)
	}
	info, err := c.Inspect(ctx, id)
	if err != nil {
		return nil, err
	}
	if info.State == nil || !info.State.Running || info.State.Paused || info.State.Restarting {
		return nil, fmt.Errorf("container must be running and not paused or restarting to open a shell")
	}
	cmd := exec.CommandContext(ctx, path, "exec", "-it", id, executable)
	cmd.Env = c.CLIEnvironment()
	return &shellProcess{cmd: cmd}, nil
}

// shellProcess keeps recent stderr available after the alternate screen returns.
type shellProcess struct {
	cmd    *exec.Cmd
	stderr bytes.Buffer
}

func (p *shellProcess) SetStdin(r io.Reader)  { p.cmd.Stdin = r }
func (p *shellProcess) SetStdout(w io.Writer) { p.cmd.Stdout = w }
func (p *shellProcess) SetStderr(w io.Writer) { p.cmd.Stderr = io.MultiWriter(w, p) }

func (p *shellProcess) Write(data []byte) (int, error) {
	const limit = 4096
	n := len(data)
	if n >= limit {
		p.stderr.Reset()
		data = data[n-limit:]
	} else if p.stderr.Len()+n > limit {
		p.stderr.Next(p.stderr.Len() + n - limit)
	}
	p.stderr.Write(data)
	return n, nil
}

func (p *shellProcess) Run() error {
	if err := p.cmd.Run(); err != nil {
		if detail := strings.Join(strings.Fields(ansi.Strip(p.stderr.String())), " "); detail != "" {
			return fmt.Errorf("%w: %s", err, ansi.Truncate(detail, 200, "…"))
		}
		return err
	}
	return nil
}
