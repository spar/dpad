package docker

import (
	"context"
	"io"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

// ContainerEvent retains only fields useful to the timeline, not arbitrary labels.
type ContainerEvent struct {
	At                                                          time.Time
	ID, Name, Action, Image, Project, Service, ExitCode, Signal string
}

type EventUpdate struct {
	Event      ContainerEvent
	Subscribed bool
	Err        error
}

var timelineActions = []events.Action{
	events.ActionCreate, events.ActionStart, events.ActionStop, events.ActionRestart,
	events.ActionDie, events.ActionDestroy, events.ActionPause, events.ActionUnPause,
	events.ActionRename, events.ActionHealthStatus, events.ActionOOM, events.ActionKill,
	events.ActionUpdate,
}

func eventText(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	for i := range s {
		if i >= 256 {
			return strings.Clone(s[:i])
		}
	}
	return s
}

func timelineEvent(m events.Message) (ContainerEvent, bool) {
	action := events.Action(strings.SplitN(string(m.Action), ":", 2)[0])
	if m.Type != events.ContainerEventType || m.Actor.ID == "" || !slices.Contains(timelineActions, action) {
		return ContainerEvent{}, false
	}
	at := time.Unix(m.Time, 0)
	if m.TimeNano != 0 {
		at = time.Unix(0, m.TimeNano)
	}
	a := m.Actor.Attributes
	return ContainerEvent{At: at, ID: m.Actor.ID, Name: eventText(a["name"]), Action: eventText(string(m.Action)), Image: eventText(a["image"]), Project: eventText(a["com.docker.compose.project"]), Service: eventText(a["com.docker.compose.service"]), ExitCode: eventText(a["exitCode"]), Signal: eventText(a["signal"])}, true
}

// WatchEvents follows lifecycle and health events with bounded backpressure.
// Reconnection starts a new live subscription; callers must surface gaps.
func (c *Client) WatchEvents(ctx context.Context) <-chan EventUpdate {
	out := make(chan EventUpdate, 128)
	go func() {
		defer close(out)
		send := func(u EventUpdate) bool {
			select {
			case out <- u:
				return true
			case <-ctx.Done():
				return false
			}
		}
		names := make([]string, len(timelineActions))
		for i, a := range timelineActions {
			names[i] = string(a)
		}
		delay := resubscribeDelay
		for ctx.Err() == nil {
			ectx, cancel := context.WithCancel(ctx)
			ev := c.api.Events(ectx, client.EventsListOptions{Filters: make(client.Filters).Add("type", string(events.ContainerEventType)).Add("event", names...)})
			if !send(EventUpdate{Subscribed: true}) {
				cancel()
				return
			}
			var streamErr error
		stream:
			for {
				select {
				case <-ctx.Done():
					cancel()
					return
				case m, ok := <-ev.Messages:
					if !ok {
						streamErr = io.EOF
						break stream
					}
					if e, ok := timelineEvent(m); ok {
						delay = resubscribeDelay
						if !send(EventUpdate{Event: e}) {
							cancel()
							return
						}
					}
				case err, ok := <-ev.Err:
					streamErr = err
					if !ok || err == nil {
						streamErr = io.EOF
					}
					break stream
				}
			}
			cancel()
			if ctx.Err() != nil || !send(EventUpdate{Err: streamErr}) {
				return
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			delay = min(delay*2, maxResubscribeDelay)
		}
	}()
	return out
}
