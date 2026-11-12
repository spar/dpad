package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/moby/moby/api/types/events"
)

func TestTimelineEventFields(t *testing.T) {
	at := time.Now()
	m := events.Message{Type: events.ContainerEventType, Action: events.ActionDie, Time: at.Unix(), TimeNano: at.UnixNano(), Actor: events.Actor{ID: "full-container-id", Attributes: map[string]string{
		"name": "web", "image": "alpine", "com.docker.compose.project": "shop", "com.docker.compose.service": "api", "exitCode": "137", "signal": "9", "secret": "must not retain",
	}}}
	got, ok := timelineEvent(m)
	if !ok || !got.At.Equal(at) || got.ID != m.Actor.ID || got.Name != "web" || got.Action != "die" || got.Project != "shop" || got.Service != "api" || got.ExitCode != "137" || got.Signal != "9" || got.Image != "alpine" {
		t.Fatalf("lost event fields: %+v", got)
	}
	if strings.Contains(fmt.Sprint(got), "must not retain") {
		t.Fatal("retained arbitrary label")
	}
	m.TimeNano = 0
	got, _ = timelineEvent(m)
	if got.At.Unix() != m.Time || got.At.Nanosecond() != 0 {
		t.Fatal("seconds fallback lost")
	}
	m.Action = events.ActionHealthStatusUnhealthy
	if e, ok := timelineEvent(m); !ok || e.Action != "health_status: unhealthy" {
		t.Fatal(e, ok)
	}
	for _, action := range timelineActions {
		m.Action = action
		if _, ok := timelineEvent(m); !ok {
			t.Fatal("lifecycle event rejected", action)
		}
	}
	for _, action := range []events.Action{events.ActionExecDie, "exec_create: /bin/sh", "attach"} {
		m.Action = action
		if _, ok := timelineEvent(m); ok {
			t.Fatal("noisy event accepted", action)
		}
	}
	m.Action = events.ActionCreate
	m.Type = events.ImageEventType
	if _, ok := timelineEvent(m); ok {
		t.Fatal("non-container event accepted")
	}
}

func TestTimelineBoundsText(t *testing.T) {
	for _, s := range []string{strings.Repeat("x", 4096), strings.Repeat("界", 4096), "line\none\r\x1b\t"} {
		got := eventText(s)
		if len(got) > 259 || !utf8.ValidString(got) || strings.ContainsAny(got, "\r\n\x1b\t") {
			t.Fatal("unsafe/unbounded event field", got)
		}
	}
}

func TestWatchEventsReconnectsClosedStreams(t *testing.T) {
	for _, closed := range []bool{false, true} {
		t.Run(fmt.Sprint(closed), func(t *testing.T) {
			f := newFakeEngine()
			ctx, cancel := context.WithCancel(t.Context())
			ch := (&Client{api: f}).WatchEvents(ctx)
			defer func() {
				cancel()
				for range ch {
				}
			}()
			next := func() EventUpdate {
				t.Helper()
				select {
				case u := <-ch:
					return u
				case <-time.After(3 * time.Second):
					t.Fatal("missing event update")
					return EventUpdate{}
				}
			}
			if !next().Subscribed {
				t.Fatal("no subscription signal")
			}
			send := func(action events.Action) {
				t.Helper()
				f.mu.Lock()
				msgs := f.events[len(f.events)-1]
				f.mu.Unlock()
				select {
				case msgs <- events.Message{Type: events.ContainerEventType, Action: action, Actor: events.Actor{ID: "web", Attributes: map[string]string{"name": "web"}}}:
				case <-time.After(time.Second):
					t.Fatal("subscription blocked")
				}
			}
			send(events.ActionStart)
			if next().Event.Action != "start" {
				t.Fatal("event was lost")
			}
			if closed {
				f.mu.Lock()
				close(f.events[len(f.events)-1])
				f.mu.Unlock()
			} else {
				f.fail(errors.New("daemon interrupted"))
			}
			if next().Err == nil {
				t.Fatal("interruption was silent")
			}
			if !next().Subscribed || f.subscriptions() != 2 {
				t.Fatal("did not resubscribe exactly once")
			}
			send(events.ActionOOM)
			if next().Event.Action != "oom" {
				t.Fatal("reconnected stream lost event")
			}
			cancel()
			for range ch {
			}
			if f.subscriptions() != 2 {
				t.Fatal("subscription after cancellation")
			}
		})
	}
}

func TestWatchEventsCancellationWithFullBuffer(t *testing.T) {
	f := newFakeEngine()
	ctx, cancel := context.WithCancel(t.Context())
	ch := (&Client{api: f}).WatchEvents(ctx)
	<-ch
	f.mu.Lock()
	msgs := f.events[0]
	f.mu.Unlock()
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		for range cap(ch) + 2 {
			select {
			case msgs <- events.Message{Type: events.ContainerEventType, Action: events.ActionStart, Actor: events.Actor{ID: "web"}}:
			case <-ctx.Done():
				return
			}
		}
	}()
	waitFor(t, "bounded queue to fill", func() bool { return len(ch) == cap(ch) })
	cancel()
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("blocked event sender leaked")
	}
	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("event reader did not stop under backpressure")
	}
}
