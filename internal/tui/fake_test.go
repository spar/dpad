package tui

import (
	"context"
	"sync"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"

	"dockernext/internal/docker"
)

// fakeClient records container actions; list calls return fixed data.
type fakeClient struct {
	containers []container.Summary
	images     []image.Summary
	inspect    container.InspectResponse
	inspectErr error

	mu      sync.Mutex
	actions []string // "verb id"
}

func (f *fakeClient) Containers(context.Context, bool) ([]container.Summary, error) {
	return f.containers, nil
}

func (f *fakeClient) Images(context.Context) ([]image.Summary, error) { return f.images, nil }

func (f *fakeClient) Inspect(_ context.Context, id string) (container.InspectResponse, error) {
	_ = f.record("inspect", id)
	return f.inspect, f.inspectErr
}

func (f *fakeClient) record(verb, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, verb+" "+id)
	return nil
}

func (f *fakeClient) Start(_ context.Context, id string) error   { return f.record("start", id) }
func (f *fakeClient) Stop(_ context.Context, id string) error    { return f.record("stop", id) }
func (f *fakeClient) Restart(_ context.Context, id string) error { return f.record("restart", id) }

func (f *fakeClient) Logs(context.Context, string, docker.LogOptions, chan<- docker.LogLine) error {
	return nil
}

func (f *fakeClient) WatchStats(context.Context) (<-chan docker.StatsUpdate, error) {
	return make(chan docker.StatsUpdate), nil
}

func (f *fakeClient) ContainerChanges(context.Context) (<-chan struct{}, error) {
	return make(chan struct{}), nil
}
