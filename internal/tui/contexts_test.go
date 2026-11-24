package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/moby/moby/api/types/container"

	"dpad/internal/docker"
)

func TestContextPickerFiltersSelectsAndCancels(t *testing.T) {
	a := newTestApp()
	a.setFilter("original-filter")
	press(a, "c")
	p := a.contextPicker
	if p == nil || !strings.Contains(a.render(), "loading Docker contexts") {
		t.Fatal("picker did not open")
	}
	a.Update(contextsMsg{picker: p, items: []docker.Context{
		{Name: "default", Endpoint: "unix:///var/run/docker.sock"},
		{Name: "remote", Endpoint: "ssh://server/run/user/1000/docker.sock"},
	}})
	press(a, "/", "r", "e", "m", "o", "t", "e", "enter")
	_, cmd := a.Update(key("enter"))
	if cmd == nil || a.nextContext != "remote" {
		t.Fatal("filtered context was not selected")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("selection did not end the previous program queue")
	}
	if a.tabs[0].Filter() != "original-filter" {
		t.Fatal("picker modified the tab filter")
	}
	a.nextContext = ""
	press(a, "esc", "c")
	current := a.contextPicker
	a.Update(contextsMsg{picker: p, items: []docker.Context{{Name: "stale"}}})
	if current.loaded {
		t.Fatal("stale picker result replaced current choices")
	}
	a.Update(contextsMsg{picker: current, err: errors.New("unreadable context store")})
	_, cmd = a.Update(key("enter"))
	if cmd != nil || a.nextContext != "" || !strings.Contains(a.render(), "unreadable context store") {
		t.Fatal("picker selected a context after a loading error")
	}
	press(a, "esc")
	if a.contextPicker != nil || a.tabs[0].Filter() != "original-filter" {
		t.Fatal("cancel did not return to the original view")
	}
}

func TestContextPickerWaitsForContainerAction(t *testing.T) {
	a := newTestApp()
	a.client = &fakeClient{}
	a.Update(containersMsg{items: []container.Summary{{ID: "web", Names: []string{"/web"}}}})
	press(a, "S", "y", "c")
	if a.contextPicker != nil || !strings.Contains(a.flash, "wait for the container action") {
		t.Fatal("context picker opened with an action in flight")
	}
	press(a, "S", "y")
	a.Update(actionDoneMsg{text: "done"})
	press(a, "c")
	if a.contextPicker != nil {
		t.Fatal("context picker opened while another action was still in flight")
	}
	a.Update(actionDoneMsg{text: "done"})
	press(a, "c")
	if a.contextPicker == nil {
		t.Fatal("context picker stayed blocked after the action finished")
	}
}

type contextStreamClient struct {
	*fakeClient
	statsStopped, eventsStopped chan struct{}
}

func (c *contextStreamClient) WatchStats(ctx context.Context) (<-chan docker.StatsUpdate, error) {
	ch := make(chan docker.StatsUpdate)
	go func() { <-ctx.Done(); close(ch); close(c.statsStopped) }()
	return ch, nil
}

func (c *contextStreamClient) WatchEvents(ctx context.Context) <-chan docker.EventUpdate {
	ch := make(chan docker.EventUpdate)
	go func() { <-ctx.Done(); close(ch); close(c.eventsStopped) }()
	return ch
}

func TestContextSwitchCancelsStreamsAndStartsWithFreshViews(t *testing.T) {
	old := &contextStreamClient{fakeClient: &fakeClient{}, statsStopped: make(chan struct{}), eventsStopped: make(chan struct{})}
	next := &fakeClient{}
	var previous *App
	runs := 0
	err := runSessions(t.Context(), old, func(a *App) error {
		runs++
		if runs == 1 {
			previous = a
			a.tabs[4].Init()()
			a.tabs[2].Focus()()
			a.Update(containersMsg{items: []container.Summary{{ID: "old", Names: []string{"/old"}}}})
			a.Update(statsBatchMsg{{Stats: docker.Stats{ID: "old", Name: "old", CPUPercent: 10}}})
			a.tabs[4].(*EventsView).append(eventRow{event: docker.ContainerEvent{ID: "old"}})
			a.setFilter("old")
			a.active = 2
			a.confirm = &pending{verb: "stop", id: "old"}
			a.nextContext = "next"
			return nil
		}
		if a.client != next || a.active != 2 || previous.ctx.Err() == nil || a.ctx.Err() != nil {
			t.Fatal("switch did not replace the session and preserve the active tab")
		}
		for _, stopped := range []chan struct{}{old.statsStopped, old.eventsStopped} {
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("old daemon stream was not cancelled")
			}
		}
		if old.closed != 1 || a.confirm != nil || len(a.tabs[0].(*ContainersView).items) != 0 || len(a.tabs[2].(*StatsView).history) != 0 || a.tabs[4].(*EventsView).count != 0 {
			t.Fatal("old client or daemon-specific data survived the switch")
		}
		for _, tab := range a.tabs {
			if tab.Filter() != "" {
				t.Fatal("old filter survived the switch")
			}
		}
		// A late reply can only update the old model, never the replacement.
		previous.Update(containersMsg{items: []container.Summary{{ID: "late"}}})
		if len(a.tabs[0].(*ContainersView).items) != 0 {
			t.Fatal("late old-daemon response entered the new session")
		}
		return nil
	}, func(_ context.Context, name string) (dockerClient, error) {
		if name != "next" || previous.ctx.Err() == nil {
			t.Fatal("old requests were not cancelled before connecting")
		}
		return next, nil
	})
	if err != nil || runs != 2 || next.closed != 1 {
		t.Fatalf("session lifecycle = %d runs, %d closes, %v", runs, next.closed, err)
	}
}

func TestContextSwitchFailureKeepsPreviousDaemon(t *testing.T) {
	c := &fakeClient{}
	runs := 0
	err := runSessions(t.Context(), c, func(a *App) error {
		runs++
		if runs == 1 {
			a.nextContext = "missing"
			return nil
		}
		if a.client != c || c.closed != 0 || !a.flashErr || !strings.Contains(a.flash, "cannot switch to missing") || !strings.Contains(a.flash, "context not found") {
			t.Fatal("failed selection did not return to the original daemon with its error")
		}
		return nil
	}, func(context.Context, string) (dockerClient, error) {
		return nil, errors.New("context not found")
	})
	if err != nil || runs != 2 || c.closed != 1 {
		t.Fatalf("failed switch lifecycle = %d, %d, %v", runs, c.closed, err)
	}
}
