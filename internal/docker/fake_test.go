package docker

import (
	"context"
	"encoding/json"
	"io"
	"sync"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

// fakeEngine is an in-memory daemon for tests. Unset methods panic via the
// embedded nil interface.
type fakeEngine struct {
	engine

	mu         sync.Mutex
	containers []container.Summary
	listErr    error
	statsCalls map[string]int
	// hold, when set for an ID, keeps that container's stats stream open
	// after its context is cancelled until the channel is closed.
	hold   map[string]chan struct{}
	events []chan events.Message
	evErrs []chan error
}

func newFakeEngine(ids ...string) *fakeEngine {
	f := &fakeEngine{statsCalls: map[string]int{}, hold: map[string]chan struct{}{}}
	for _, id := range ids {
		f.containers = append(f.containers, container.Summary{ID: id, Names: []string{"/" + id}, State: "running"})
	}
	return f
}

func (f *fakeEngine) ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return client.ContainerListResult{Items: append([]container.Summary(nil), f.containers...)}, f.listErr
}

func (f *fakeEngine) setContainers(ids ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.containers = nil
	for _, id := range ids {
		f.containers = append(f.containers, container.Summary{ID: id, Names: []string{"/" + id}, State: "running"})
	}
}

func (f *fakeEngine) calls(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statsCalls[id]
}

// ContainerStats streams one sample, then waits for ctx (and any hold).
func (f *fakeEngine) ContainerStats(ctx context.Context, id string, _ client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
	f.mu.Lock()
	f.statsCalls[id]++
	hold := f.hold[id]
	f.mu.Unlock()
	pr, pw := io.Pipe()
	go func() {
		json.NewEncoder(pw).Encode(container.StatsResponse{PidsStats: container.PidsStats{Current: 1}})
		<-ctx.Done()
		if hold != nil {
			<-hold
		}
		pw.Close()
	}()
	return client.ContainerStatsResult{Body: pr}, nil
}

// Events opens a new subscription; the test drives it with send and fail.
func (f *fakeEngine) Events(context.Context, client.EventsListOptions) client.EventsResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	msgs, errs := make(chan events.Message), make(chan error, 1)
	f.events = append(f.events, msgs)
	f.evErrs = append(f.evErrs, errs)
	return client.EventsResult{Messages: msgs, Err: errs}
}

func (f *fakeEngine) subscriptions() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

// send delivers a container event on the latest subscription.
func (f *fakeEngine) send(action events.Action, id string) {
	f.mu.Lock()
	ch := f.events[len(f.events)-1]
	f.mu.Unlock()
	ch <- events.Message{Action: action, Actor: events.Actor{ID: id, Attributes: map[string]string{"name": id}}}
}

// fail breaks the latest subscription, as a daemon restart would.
func (f *fakeEngine) fail(err error) {
	f.mu.Lock()
	ch := f.evErrs[len(f.evErrs)-1]
	f.mu.Unlock()
	ch <- err
}
