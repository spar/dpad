package tui

import (
	"context"
	"slices"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"dockernext/internal/docker"
	"dockernext/internal/render"
)

const (
	maxLogLines = 5000 // lines kept in the pane
	maxLogBatch = 1000 // lines handed over at once
)

type (
	logBatchMsg struct {
		ch    <-chan docker.LogLine
		lines []docker.LogLine
	}
	logEndMsg struct {
		ch  <-chan docker.LogLine
		err error
	}
)

// LogsView follows one container's logs in a scrollable pane.
type LogsView struct {
	name   string
	ch     chan docker.LogLine
	errc   chan error
	cancel context.CancelFunc
	lines  []string
	vp     viewport.Model
	ended  bool
	err    error
}

func NewLogsView(ctx context.Context, c dockerClient, id, name string, width, height int) (*LogsView, tea.Cmd) {
	ctx, cancel := context.WithCancel(ctx)
	v := &LogsView{
		name:   name,
		ch:     make(chan docker.LogLine, 512),
		errc:   make(chan error, 1),
		cancel: cancel,
		vp:     viewport.New(viewport.WithWidth(width), viewport.WithHeight(height)),
	}
	go func() {
		v.errc <- c.Logs(ctx, id, docker.LogOptions{Follow: true, Tail: "500"}, v.ch)
		close(v.ch)
	}()
	return v, v.wait()
}

// logFlushInterval is how long wait collects lines before handing them over.
// Re-rendering the pane costs a pass over every buffered line, so a busy
// container is redrawn at most about 20 times a second, not once per line.
const logFlushInterval = 50 * time.Millisecond

// wait blocks for the next line, then collects lines for logFlushInterval
// (or until maxLogBatch) so the batch costs one re-render.
func (v *LogsView) wait() tea.Cmd {
	ch, errc := v.ch, v.errc
	return func() tea.Msg {
		l, ok := <-ch
		if !ok {
			return logEndMsg{ch, <-errc}
		}
		batch := []docker.LogLine{l}
		flush := time.After(logFlushInterval)
		for len(batch) < maxLogBatch {
			select {
			case l, ok := <-ch:
				if !ok {
					return logBatchMsg{ch, batch}
				}
				batch = append(batch, l)
			case <-flush:
				return logBatchMsg{ch, batch}
			}
		}
		return logBatchMsg{ch, batch}
	}
}

func (v *LogsView) Close() { v.cancel() }

func (v *LogsView) SetSize(width, height int) {
	v.vp.SetWidth(width)
	v.vp.SetHeight(height)
}

func (v *LogsView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case logBatchMsg:
		if msg.ch != v.ch {
			return nil
		}
		atBottom := v.vp.AtBottom()
		for _, l := range msg.lines {
			v.lines = append(v.lines, render.LogLine(l, lipgloss.Style{}, 0, false, nil))
		}
		if over := len(v.lines) - maxLogLines; over > 0 {
			v.lines = v.lines[over:]
		}
		v.vp.SetContentLines(slices.Clone(v.lines)) // the viewport may edit its slice
		if atBottom {
			v.vp.GotoBottom()
		}
		return v.wait()
	case logEndMsg:
		if msg.ch == v.ch {
			v.ended, v.err = true, msg.err
		}
		return nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "G", "end":
			v.vp.GotoBottom()
			return nil
		case "g", "home":
			v.vp.GotoTop()
			return nil
		}
	}
	var cmd tea.Cmd
	v.vp, cmd = v.vp.Update(msg)
	return cmd
}

func (v *LogsView) View() string { return v.vp.View() }

func (v *LogsView) Status() string {
	s := "logs: " + render.Accent.Render(v.name)
	switch {
	case v.err != nil:
		s += " · " + render.Error.Render(v.err.Error())
	case v.ended:
		s += " · stream ended"
	case v.vp.AtBottom():
		s += " · following"
	default:
		s += " · paused (G to follow)"
	}
	return s
}
