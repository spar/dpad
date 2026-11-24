package tui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"dpad/internal/docker"
	"dpad/internal/render"
	"dpad/internal/sortx"
)

const eventCapacity = 1000
const eventBatchInterval = 100 * time.Millisecond
const maxEventBatch = 1024

type eventRow struct {
	seq    uint64
	event  docker.ContainerEvent
	notice string
}
type eventsStartedMsg struct{ ch <-chan docker.EventUpdate }
type eventsBatchMsg struct {
	ch      <-chan docker.EventUpdate
	updates []docker.EventUpdate
}

// EventsView keeps a bounded session timeline, including subscription gaps.
type EventsView struct {
	list[eventRow]
	ctx         context.Context
	client      dockerClient
	ch          <-chan docker.EventUpdate
	ring        [eventCapacity]eventRow
	next, count int
	sequence    uint64
	interrupted bool
	streamErr   error
	started     bool
}

func NewEventsView(ctx context.Context, c dockerClient) *EventsView {
	v := &EventsView{ctx: ctx, client: c}
	v.list = list[eventRow]{
		key: sortx.Key("time"), keys: []sortx.Key{"time"}, sortCol: map[sortx.Key]int{"time": 0},
		match: func(r eventRow, f string) bool {
			e := r.event
			gap := ""
			if r.notice != "" {
				gap = "stream gap"
			}
			return matches(f, e.ID, e.Name, e.Project, e.Service, e.Action, e.Image, e.ExitCode, e.Signal, r.notice, gap)
		},
		sort: func(items []eventRow, _ sortx.Key, reverse bool) {
			if reverse {
				slices.Reverse(items)
			}
		},
		rows: func(r eventRow) []row {
			e := r.event
			name := e.Name
			if name == "" {
				name = e.ID
				if len(name) > 12 {
					name = name[:12]
				}
			}
			project := e.Project
			if e.Service != "" {
				project += " / " + e.Service
			}
			action := e.Action
			switch {
			case action == "oom", action == "health_status: unhealthy", action == "die" && e.ExitCode != "" && e.ExitCode != "0":
				action = render.Error.Render(action)
			case action == "kill", action == "pause", action == "restart":
				action = render.Warn.Render(action)
			}
			detail := e.Image
			if e.ExitCode != "" {
				detail = "exit=" + e.ExitCode
			}
			if e.Signal != "" {
				detail = "signal=" + e.Signal
			}
			if r.notice != "" {
				action = render.Warn.Render("stream gap")
				detail = r.notice
			}
			stamp := e.At.Local().Format("2006-01-02 15:04:05.000")
			cells := []string{stamp, name, project, action, detail}
			if v.width < 110 {
				cells = []string{e.At.Local().Format("15:04:05.000"), name, action, detail}
			}
			return []row{{key: strconv.FormatUint(r.seq, 10), id: e.ID, name: name, cells: cells}}
		},
		empty: "waiting for container events… (recording from this session)", loading: "subscribing to container events…",
	}
	v.SetSize(120, 28)
	return v
}
func (v *EventsView) Title() string { return "Events" }
func (v *EventsView) Init() tea.Cmd {
	if v.started {
		return nil
	}
	v.started = true
	return func() tea.Msg { return eventsStartedMsg{ch: v.client.WatchEvents(v.ctx)} }
}
func (v *EventsView) Focus() tea.Cmd { return nil }

func (v *EventsView) SetSize(width, height int) {
	v.list.SetSize(width, height)
	if width < 110 {
		v.columns = []render.Column{{Header: "TIME", Max: 12}, {Header: "CONTAINER", Max: 20}, {Header: "EVENT", Max: 26}, {Header: "DETAILS", Max: 64}}
	} else {
		v.columns = []render.Column{{Header: "TIME", Max: 23}, {Header: "CONTAINER", Max: 28}, {Header: "PROJECT / SERVICE", Max: 32}, {Header: "EVENT", Max: 30}, {Header: "DETAILS", Max: 64}}
	}
	v.rowCache = nil
}

func (v *EventsView) View() string {
	if v.loaded && v.filter != "" {
		if rows, _ := v.visible(); len(rows) == 0 {
			return render.Dim.Render("no events match the filter")
		}
	}
	return v.list.View()
}

func waitEvents(ctx context.Context, ch <-chan docker.EventUpdate) tea.Cmd {
	return func() tea.Msg {
		var first docker.EventUpdate
		select {
		case <-ctx.Done():
			return nil
		case u, ok := <-ch:
			if !ok {
				return nil
			}
			first = u
		}
		batch := make([]docker.EventUpdate, 0, 16)
		batch = append(batch, first)
		interval := eventBatchInterval
		if remoteTerminal() {
			interval = 250 * time.Millisecond
		}
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			if len(batch) == maxEventBatch {
				select {
				case <-ctx.Done():
					return nil
				case <-timer.C:
					return eventsBatchMsg{ch: ch, updates: batch}
				}
			}
			select {
			case <-ctx.Done():
				return nil
			case u, ok := <-ch:
				if !ok {
					return eventsBatchMsg{ch: ch, updates: batch}
				}
				batch = append(batch, u)
			case <-timer.C:
				return eventsBatchMsg{ch: ch, updates: batch}
			}
		}
	}
}

func (v *EventsView) append(r eventRow) {
	v.sequence++
	r.seq = v.sequence
	v.ring[v.next] = r
	v.next = (v.next + 1) % eventCapacity
	v.count = min(v.count+1, eventCapacity)
}
func (v *EventsView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case eventsStartedMsg:
		if v.ch != nil || v.ctx.Err() != nil {
			return nil
		}
		v.ch = msg.ch
		v.loaded = true
		return waitEvents(v.ctx, v.ch)
	case eventsBatchMsg:
		if msg.ch != v.ch || v.ctx.Err() != nil {
			return nil
		}
		changed := false
		for _, u := range msg.updates {
			if u.Err != nil {
				v.streamErr = u.Err
				if !v.interrupted {
					v.append(eventRow{event: docker.ContainerEvent{At: time.Now()}, notice: "subscription interrupted; events may be missing"})
					v.interrupted = true
					changed = true
				}
			} else if u.Event.ID != "" {
				v.streamErr = nil
				v.interrupted = false
				v.append(eventRow{event: u.Event})
				changed = true
			} else if u.Subscribed {
				v.streamErr = nil
			}
		}
		if changed {
			items := v.items[:0]
			for i := range v.count {
				items = append(items, v.ring[(v.next-1-i+eventCapacity)%eventCapacity])
			}
			v.setItems(items, nil)
		}
		return waitEvents(v.ctx, v.ch)
	}
	return nil
}
func (v *EventsView) HandleKey(key string) tea.Cmd {
	if key == "g" {
		v.cur.key = ""
		v.setSort("time", false)
		return nil
	}
	return v.list.HandleKey(key)
}
func (v *EventsView) Status() string {
	rows, _ := v.visible()
	order := "newest first"
	if v.reverse {
		order = "oldest first"
	}
	state := "following"
	if v.streamErr != nil {
		state = "retrying: " + connectionError(v.streamErr)
	}
	return fmt.Sprintf("%d/%d events · retaining last %d · %s · %s", len(rows), v.count, eventCapacity, order, state)
}
func (v *EventsView) Help() [][2]string {
	return [][2]string{{"r", "reverse time order"}, {"g", "return to top and follow new events (newest first)"}, {"/", "filter by container, Compose project/service, event type, or image"}, {"d / enter", "open the event's container details (removed containers may be unavailable)"}}
}
