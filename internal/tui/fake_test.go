package tui

import (
	"context"
	"sync"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/volume"

	"dockernext/internal/docker"
)

// fakeClient records container actions; list calls return fixed data.
type fakeClient struct {
	containers     []container.Summary
	images         []image.Summary
	inspect        container.InspectResponse
	inspectErr     error
	imageInfo      docker.ImageInfo
	imageErr       error
	pingErr        error
	volumes        []volume.Volume
	volumeInfo     volume.Volume
	volumeErr      error
	volumeWarnings []string
	networks       []network.Summary
	networkInfo    network.Inspect
	networkErr     error
	cliEnv         []string
	closed         int

	mu      sync.Mutex
	actions []string // "verb id"
}

func (f *fakeClient) ConnectionLabel() string    { return "test-daemon" }
func (f *fakeClient) CLIEnvironment() []string   { return f.cliEnv }
func (f *fakeClient) Close() error               { f.closed++; return nil }
func (f *fakeClient) Ping(context.Context) error { return f.pingErr }

func (f *fakeClient) Containers(context.Context, bool) ([]container.Summary, error) {
	return f.containers, nil
}

func (f *fakeClient) Images(context.Context) ([]image.Summary, error) { return f.images, nil }

func (f *fakeClient) ImageDetails(_ context.Context, id string) (docker.ImageInfo, error) {
	_ = f.record("image inspect", id)
	return f.imageInfo, f.imageErr
}

func (f *fakeClient) RemoveImage(_ context.Context, removal docker.ImageRemoval) error {
	_ = f.record("image remove", removal.Reference)
	return f.imageErr
}

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

func (f *fakeClient) WatchEvents(context.Context) <-chan docker.EventUpdate {
	return make(chan docker.EventUpdate)
}

func (f *fakeClient) Volumes(context.Context) ([]volume.Volume, []string, error) {
	_ = f.record("volumes", "")
	return f.volumes, f.volumeWarnings, f.volumeErr
}
func (f *fakeClient) Volume(_ context.Context, name string) (volume.Volume, error) {
	_ = f.record("volume inspect", name)
	return f.volumeInfo, f.volumeErr
}

func (f *fakeClient) Networks(context.Context) ([]network.Summary, error) {
	_ = f.record("networks", "")
	return f.networks, f.networkErr
}
func (f *fakeClient) Network(_ context.Context, id string) (network.Inspect, error) {
	_ = f.record("network inspect", id)
	return f.networkInfo, f.networkErr
}
