package tui

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"

	"dpad/internal/docker"
	"dpad/internal/render"
)

const (
	maxLogLines    = 5000 // lines kept in the pane
	maxLogBatch    = 1000 // lines handed over at once
	logSearchHints = "enter keep · esc cancel"
)

type (
	logTarget     struct{ id, name string }
	logFailureMsg struct {
		ch  <-chan docker.LogLine
		err error
	}
	bufferedLog struct {
		line    docker.LogLine
		width   int // display width of line.Text
		matches bool
	}
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

// LogsView follows container logs in a scrollable, filterable pane.
type LogsView struct {
	name          string
	ch            chan docker.LogLine
	errc          chan error
	cancel        context.CancelFunc
	lines         []bufferedLog
	vp            logScroll
	ended         bool
	err           error
	search        textinput.Model
	searching     bool
	query         string
	previousQuery string
	pattern       *regexp.Regexp
	searchErr     error
	visible       int
	failures      chan error
	prefixWidth   int
	colors        render.NameColors
	project       *projectLogSession
	projectState  projectLogStatus
	remote        bool
	timestamps    bool

	// Paging glides to target over a few frames; gen invalidates stale frames.
	target    int
	animating bool
	gen       int
}

func NewLogsView(ctx context.Context, c dockerClient, id, name string, width, height int) (*LogsView, tea.Cmd) {
	return newLogsView(ctx, c, []logTarget{{id: id, name: name}}, name, width, height)
}

func newLogsView(ctx context.Context, c dockerClient, targets []logTarget, name string, width, height int) (*LogsView, tea.Cmd) {
	v, ctx := newLogPane(ctx, name, width, height)
	v.failures = make(chan error, len(targets))
	results := make(chan error, len(targets))
	for _, target := range targets {
		if len(targets) > 1 {
			v.prefixWidth = max(v.prefixWidth, len(target.name))
		}
		v.colors.Style(target.name)
		go func() {
			err := c.Logs(ctx, target.id, docker.LogOptions{Follow: true, Tail: "500"}, v.ch)
			if ctx.Err() != nil {
				err = nil
			}
			if err != nil {
				err = fmt.Errorf("%s: %w", target.name, err)
				v.failures <- err
			}
			results <- err
		}()
	}
	go func() {
		var errs []error
		for range targets {
			if err := <-results; err != nil {
				errs = append(errs, err)
			}
		}
		v.errc <- errors.Join(errs...)
		close(v.ch)
	}()
	return v, v.wait()
}

func newProjectLogsView(ctx context.Context, c dockerClient, project string, items []container.Summary, width, height int) (*LogsView, tea.Cmd) {
	v, ctx := newLogPane(ctx, "project "+project, width, height)
	v.prefixWidth = 1
	v.project = newProjectLogSession(ctx, c, project, items, v.ch, v.errc)
	return v, v.wait()
}

func newLogPane(ctx context.Context, name string, width, height int) (*LogsView, context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	v := &LogsView{
		name:   name,
		ch:     make(chan docker.LogLine, 512),
		errc:   make(chan error, 1),
		cancel: cancel,
		vp:     logScroll{width: width, height: height},
		search: textinput.New(),
		colors: render.NameColors{},
		remote: remoteTerminal(),
	}
	v.search.Prompt = "/"
	v.search.Placeholder = "regex (e.g. ERROR|WARN, (?i)timeout)"
	v.search.SetWidth(max(width-lipgloss.Width(logSearchHints)-2, 1))
	return v, ctx
}

// logFlushInterval is how long wait collects lines before handing them over.
// Re-rendering the pane costs a pass over every buffered line, so a busy
// container is redrawn at most about 20 times a second, not once per line.
const logFlushInterval = 50 * time.Millisecond

// wait blocks for the next line, then collects lines for logFlushInterval
// (or until maxLogBatch) so the batch costs one re-render.
func (v *LogsView) wait() tea.Cmd {
	ch, errc := v.ch, v.errc
	var projectStatus <-chan projectLogStatus
	if v.project != nil {
		projectStatus = v.project.status
	}
	return func() tea.Msg {
		var l docker.LogLine
		select {
		case status := <-projectStatus:
			return projectLogStatusMsg{ch: ch, status: status}
		case err := <-v.failures:
			return logFailureMsg{ch, err}
		case line, ok := <-ch:
			if !ok {
				return logEndMsg{ch, <-errc}
			}
			l = line
		}
		batch := []docker.LogLine{l}
		interval := logFlushInterval
		if v.remote {
			interval = 100 * time.Millisecond
		}
		flush := time.After(interval)
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
	v.search.SetWidth(max(width-lipgloss.Width(logSearchHints)-2, 1))
}

func (v *LogsView) setQuery(query string) {
	var pattern *regexp.Regexp
	if query != "" {
		var err error
		pattern, err = regexp.Compile(query)
		if err != nil {
			v.searchErr = err
			return
		}
	}
	v.searchErr = nil
	v.query, v.pattern = query, pattern
	v.animating = false
	v.gen++
	for i := range v.lines {
		v.lines[i].matches = v.matches(v.lines[i].line)
	}
	v.rebuild()
}

func (v *LogsView) matches(line docker.LogLine) bool {
	return v.pattern == nil || v.pattern.MatchString(line.Text)
}

// text styles one line. It runs only for lines being drawn, since styling
// every buffered line is what made busy logs and filter typing slow.
func (v *LogsView) text(line docker.LogLine) string {
	style := lipgloss.Style{}
	if v.prefixWidth > 0 {
		if v.project != nil {
			style = render.StableNameStyle(line.Container)
		} else {
			style = v.colors.Style(line.Container)
		}
	}
	return render.LogLine(line, style, v.prefixWidth, v.timestamps, v.pattern)
}

func (v *LogsView) rebuild() {
	atBottom := v.vp.AtBottom() && !v.animating
	shown := make([]int, 0, len(v.lines))
	widths := make([]int, 0, len(v.lines))
	for i, l := range v.lines {
		if l.matches {
			shown = append(shown, i)
			widths = append(widths, render.LogLineWidth(l.line, v.prefixWidth, v.timestamps, l.width))
		}
	}
	v.visible = len(shown)
	if len(shown) == 0 && v.query != "" {
		v.vp.SetContentLines([]string{"no matching log lines"})
	} else {
		lines := v.lines
		v.vp.SetLines(widths, func(i int) string { return v.text(lines[shown[i]].line) })
	}
	if atBottom {
		v.vp.GotoBottom()
	}
}

func (v *LogsView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case containersMsg:
		if v.project != nil {
			v.project.update(projectLogSnapshot(msg))
		}
		return nil
	case projectLogStatusMsg:
		if msg.ch != v.ch {
			return nil
		}
		v.projectState = msg.status
		return v.wait()
	case logFailureMsg:
		if msg.ch != v.ch {
			return nil
		}
		v.err = errors.Join(v.err, msg.err)
		return v.wait()
	case logBatchMsg:
		if msg.ch != v.ch {
			return nil
		}
		following := v.vp.AtBottom() && !v.animating
		if v.project != nil {
			width := v.prefixWidth
			for _, line := range msg.lines {
				width = max(width, len(line.Container))
			}
			v.prefixWidth = width
		}
		for _, line := range msg.lines {
			v.lines = append(v.lines, bufferedLog{line: line, width: ansi.StringWidth(line.Text), matches: v.matches(line)})
		}
		if over := len(v.lines) - maxLogLines; over > 0 {
			if !following {
				visible := 0
				for _, line := range v.lines[:over] {
					if line.matches {
						visible++
					}
				}
				v.vp.SetYOffset(max(v.vp.YOffset()-visible, 0))
				if v.animating {
					v.target = max(v.target-visible, 0)
				}
			}
			v.lines = v.lines[over:]
		}
		v.rebuild()
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
		if v.searching {
			switch msg.String() {
			case "enter":
				if v.searchErr == nil {
					v.searching = false
					v.search.Blur()
				}
				return nil
			case "esc":
				v.searching = false
				v.search.Blur()
				v.search.SetValue(v.previousQuery)
				v.setQuery(v.previousQuery)
				return nil
			}
			var cmd tea.Cmd
			v.search, cmd = v.search.Update(msg)
			v.setQuery(v.search.Value())
			return cmd
		}
		switch msg.String() {
		case "/":
			v.searching = true
			v.previousQuery = v.query
			v.search.SetValue(v.query)
			v.search.CursorEnd()
			return v.search.Focus()
		case "ctrl+l":
			v.search.SetValue("")
			v.setQuery("")
			return nil
		case "w":
			following := v.vp.AtBottom() && !v.animating
			v.animating = false
			v.vp.SetXOffset(0)
			v.vp.SoftWrap = !v.vp.SoftWrap
			if following {
				v.vp.GotoBottom()
			}
			return nil
		case "t":
			v.timestamps = !v.timestamps
			v.setQuery(v.query)
			return nil
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
	if v.searching {
		var cmd tea.Cmd
		previous := v.search.Value()
		v.search, cmd = v.search.Update(msg)
		if v.search.Value() != previous {
			v.setQuery(v.search.Value())
		}
		return cmd
	}
	v.vp.Update(msg)
	return nil
}

// scrollFrame is one animation frame, matching Bubble Tea's 60fps renderer.
const scrollFrame = time.Second / 60

// scrollBy moves the scroll target by delta lines and starts gliding toward it.
// Presses during a glide extend the target, so a held key scrolls continuously.
func (v *LogsView) scrollBy(delta int) tea.Cmd {
	if v.remote {
		v.animating = false
		v.gen++
		v.vp.SetYOffset(v.vp.YOffset() + delta)
		return nil
	}

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
	if v.project != nil {
		s += fmt.Sprintf(" · %d sources (%d following, %d reconnecting)", v.projectState.sources, v.projectState.following, v.projectState.retrying)
		if v.projectState.notice != "" {
			s += " · " + v.projectState.notice
		}
		if v.projectState.err != "" {
			s += " · " + render.Error.Render("last error: "+strings.ReplaceAll(v.projectState.err, "\n", " · "))
		}
	}
	if v.query != "" {
		s += fmt.Sprintf(" · filter: %s · %d/%d buffered lines", v.query, v.visible, len(v.lines))
	}
	if v.searchErr != nil {
		s += " · " + render.Error.Render("invalid regex: "+v.searchErr.Error())
	}
	switch {
	case v.err != nil:
		s += " · " + render.Error.Render(strings.ReplaceAll(v.err.Error(), "\n", " · "))
	case v.ended:
		s += " · stream ended"
	case v.project != nil && v.projectState.sources == 0 && v.vp.AtBottom():
		s += " · waiting for containers"
	case v.project != nil && v.projectState.following == 0 && v.projectState.retrying == 0 && v.vp.AtBottom():
		s += " · waiting for sources to start"
	case v.vp.AtBottom():
		s += " · following"
	default:
		s += " · paused (G to follow)"
	}
	return s
}
