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
	scrollFrameMsg struct {
		v   *LogsView
		gen int
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

	// Paging glides to target over a few frames; gen invalidates stale frames.
	target    int
	animating bool
	gen       int
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
		atBottom := v.vp.AtBottom() && !v.animating
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
	case scrollFrameMsg:
		if msg.v != v || msg.gen != v.gen || !v.animating {
			return nil
		}
		return v.scrollStep()
	case logEndMsg:
		if msg.ch == v.ch {
			v.ended, v.err = true, msg.err
		}
		return nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "G", "end":
			v.animating = false
			v.vp.GotoBottom()
			return nil
		case "g", "home":
			v.animating = false
			v.vp.GotoTop()
			return nil
		case "pgdown", "space", "f":
			return v.scrollBy(v.vp.Height())
		case "pgup", "b":
			return v.scrollBy(-v.vp.Height())
		case "ctrl+d", "d":
			return v.scrollBy(v.vp.Height() / 2)
		case "ctrl+u", "u":
			return v.scrollBy(-v.vp.Height() / 2)
		}
	}
	var cmd tea.Cmd
	v.vp, cmd = v.vp.Update(msg)
	return cmd
}

// scrollFrame is one animation frame, matching Bubble Tea's 60fps renderer.
const scrollFrame = time.Second / 60

// scrollBy moves the scroll target by delta lines and starts gliding toward it.
// Presses during a glide extend the target, so a held key scrolls continuously.
func (v *LogsView) scrollBy(delta int) tea.Cmd {
	from := v.vp.YOffset()
	if v.animating {
		from = v.target
	}
	maxY := max(v.vp.TotalLineCount()-v.vp.Height(), 0)
	v.target = min(max(from+delta, 0), maxY)
	if v.animating {
		return nil
	}
	v.animating = true
	v.gen++
	return v.scrollStep()
}

// scrollStep covers half the remaining distance (at least 2 lines) per frame,
// so a page lands in about 5 frames and eases out at the end.
func (v *LogsView) scrollStep() tea.Cmd {
	diff := v.target - v.vp.YOffset()
	if diff == 0 {
		v.animating = false
		return nil
	}
	step := max((abs(diff)+1)/2, min(2, abs(diff)))
	if diff < 0 {
		step = -step
	}
	before := v.vp.YOffset()
	v.vp.SetYOffset(before + step)
	if y := v.vp.YOffset(); y == v.target || y == before {
		v.animating = false
		return nil
	}
	msg := scrollFrameMsg{v, v.gen}
	return tea.Tick(scrollFrame, func(time.Time) tea.Msg { return msg })
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
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
