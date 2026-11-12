package tui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"dockernext/internal/render"
)

type daemonConnection struct {
	label                              string
	connected, everConnected, inFlight bool
	err                                error
	generation                         uint64
	delay                              time.Duration
}
type daemonProbeMsg struct {
	generation uint64
	err        error
}
type daemonProbeTickMsg struct{ generation uint64 }

var credentialURL = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/\s"@]+@`)

func connectionError(err error) string {
	if err == nil {
		return ""
	}
	if u, ok := errors.AsType[*url.Error](err); ok {
		err = u.Err
	}
	return strings.Join(strings.Fields(credentialURL.ReplaceAllString(err.Error(), "${1}")), " ")
}

func (a *App) probeDaemon() tea.Cmd {
	if a.client == nil || a.daemon.inFlight || a.ctx.Err() != nil {
		return nil
	}
	a.daemon.inFlight = true
	a.daemon.generation++
	gen := a.daemon.generation
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(a.ctx, 2*time.Second)
		defer cancel()
		return daemonProbeMsg{generation: gen, err: a.client.Ping(ctx)}
	}
}

func (a *App) applyDaemonProbe(msg daemonProbeMsg) tea.Cmd {
	if msg.generation != a.daemon.generation || a.ctx.Err() != nil {
		return nil
	}
	a.daemon.inFlight = false
	recovered := !a.daemon.connected
	a.daemon.err = msg.err
	delay := 2 * time.Second
	if msg.err == nil {
		a.daemon.connected, a.daemon.everConnected = true, true
		a.daemon.delay = time.Second
	} else {
		a.daemon.connected = false
		delay = a.daemon.delay
		if delay == 0 {
			delay = time.Second
		}
		a.daemon.delay = min(delay*2, 5*time.Second)
	}
	gen := a.daemon.generation
	cmds := []tea.Cmd{tea.Tick(delay, func(time.Time) tea.Msg { return daemonProbeTickMsg{generation: gen} })}
	if msg.err == nil && recovered {
		cmds = append(cmds, func() tea.Msg { return refreshMsg{} })
		if a.details != nil {
			cmds = append(cmds, a.details.load())
		}
		if a.imageDetails != nil {
			cmds = append(cmds, a.imageDetails.load())
		}
	}
	return tea.Batch(cmds...)
}

func (a *App) connectionBar() string {
	label := a.daemon.label
	if label == "" {
		label = "docker"
	}
	state := render.Dim.Render("connecting")
	if a.daemon.connected {
		state = render.Accent.Render("connected")
	} else if a.daemon.err != nil {
		if a.daemon.everConnected {
			state = render.Warn.Render("reconnecting")
		} else {
			state = render.Error.Render("disconnected; retrying")
		}
	}
	text := fmt.Sprintf("daemon: %s · %s", label, state)
	if a.daemon.err != nil {
		text += " · " + connectionError(a.daemon.err)
	}
	return ansi.Truncate(text, a.width, "…")
}
