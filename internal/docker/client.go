// Package docker wraps the Docker Engine SDK with the queries dpad needs.
package docker

import (
	"context"
	"errors"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"

	"dpad/internal/debuglog"
)

// Client is a thin wrapper around the Engine API client.
type Client struct {
	api    engine
	tunnel *sshTunnel // set for ssh:// endpoints
	label  string
	cliEnv []string
}

// engine is the part of the Engine API client dpad uses, so tests can
// substitute a fake daemon.
type engine interface {
	Close() error
	NetworkList(context.Context, client.NetworkListOptions) (client.NetworkListResult, error)
	NetworkInspect(context.Context, string, client.NetworkInspectOptions) (client.NetworkInspectResult, error)
	VolumeList(context.Context, client.VolumeListOptions) (client.VolumeListResult, error)
	VolumeInspect(context.Context, string, client.VolumeInspectOptions) (client.VolumeInspectResult, error)
	Ping(context.Context, client.PingOptions) (client.PingResult, error)
	ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error)
	ImageList(context.Context, client.ImageListOptions) (client.ImageListResult, error)
	ImageInspect(context.Context, string, ...client.ImageInspectOption) (client.ImageInspectResult, error)
	ImageRemove(context.Context, string, client.ImageRemoveOptions) (client.ImageRemoveResult, error)
	ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerStart(context.Context, string, client.ContainerStartOptions) (client.ContainerStartResult, error)
	ContainerStop(context.Context, string, client.ContainerStopOptions) (client.ContainerStopResult, error)
	ContainerRestart(context.Context, string, client.ContainerRestartOptions) (client.ContainerRestartResult, error)
	ContainerUnpause(context.Context, string, client.ContainerUnpauseOptions) (client.ContainerUnpauseResult, error)
	ContainerPause(context.Context, string, client.ContainerPauseOptions) (client.ContainerPauseResult, error)
	ContainerKill(context.Context, string, client.ContainerKillOptions) (client.ContainerKillResult, error)
	ContainerRemove(context.Context, string, client.ContainerRemoveOptions) (client.ContainerRemoveResult, error)
	ContainerLogs(context.Context, string, client.ContainerLogsOptions) (client.ContainerLogsResult, error)
	ContainerStats(context.Context, string, client.ContainerStatsOptions) (client.ContainerStatsResult, error)
	Events(context.Context, client.EventsListOptions) client.EventsResult
	VolumeRemove(context.Context, string, client.VolumeRemoveOptions) (client.VolumeRemoveResult, error)
	NetworkRemove(context.Context, string, client.NetworkRemoveOptions) (client.NetworkRemoveResult, error)
	DiskUsage(context.Context, client.DiskUsageOptions) (client.DiskUsageResult, error)
	BuildCachePrune(context.Context, client.BuildCachePruneOptions) (client.BuildCachePruneResult, error)
}

var _ engine = (*client.Client)(nil)

// New connects to the same daemon the docker CLI would use: DOCKER_HOST if set,
// otherwise the endpoint of the current docker context. An ssh:// endpoint is
// reached through an ssh tunnel to the remote daemon socket.
func New() (*Client, error) {
	name, host := "", os.Getenv("DOCKER_HOST")
	if host == "" {
		var err error
		name, host, err = contextEndpoint()
		if err != nil {
			return nil, err
		}
	}
	return newClient(context.Background(), name, host, []client.Opt{client.FromEnv})
}

// NewForContext selects a context for this client without changing Docker's
// saved selection or process environment.
func NewForContext(ctx context.Context, name string) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := dockerConfigDir()
	if err != nil {
		return nil, err
	}
	name, host, err := readContextEndpoint(dir, name)
	if err != nil {
		return nil, err
	}
	return newClient(ctx, name, host, []client.Opt{client.WithTLSClientConfigFromEnv(), client.WithAPIVersionFromEnv()})
}

func newClient(ctx context.Context, name, host string, opts []client.Opt) (*Client, error) {
	if host != "" && !isSSHHost(host) {
		opts = append(opts, client.WithHost(host))
	}
	var tunnel *sshTunnel
	if isSSHHost(host) {
		var err error
		if tunnel, err = startSSHTunnelContext(ctx, host); err != nil {
			return nil, err
		}
		opts = append(opts, client.WithHost("unix://"+tunnel.socket), client.WithDialContext(tunnel.dial))
	}
	if debuglog.On() {
		opts = append(opts, logRequests()...)
	}
	api, err := client.New(opts...)
	if err != nil {
		_ = tunnel.Close()
		return nil, err
	}
	daemon := api.DaemonHost()
	if tunnel != nil {
		daemon = host
	}
	env := []string{"DOCKER_HOST=" + daemon, "DOCKER_CONTEXT="}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "DOCKER_HOST") && !strings.EqualFold(key, "DOCKER_CONTEXT") {
			env = append(env, entry)
		}
	}
	label := connectionLabel(name, daemon)
	debuglog.Info("docker client", "endpoint", label)
	return &Client{api: api, tunnel: tunnel, label: label, cliEnv: env}, nil
}

func isSSHHost(host string) bool { return strings.HasPrefix(host, "ssh://") }

func connectionLabel(name, host string) string {
	if u, err := url.Parse(host); err == nil {
		u.User = nil
		u.RawQuery = ""
		u.Fragment = ""
		host = u.String()
	}
	if name != "" {
		return name + " @ " + host
	}
	return host
}

func (c *Client) ConnectionLabel() string { return c.label }

// CLIEnvironment binds Docker subprocesses to this client's daemon and startup
// settings, even if the saved current context changes while the TUI is open.
func (c *Client) CLIEnvironment() []string { return slices.Clone(c.cliEnv) }

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.api.Ping(ctx, client.PingOptions{})
	return err
}

func (c *Client) Close() error {
	return errors.Join(c.api.Close(), c.tunnel.Close())
}

// Containers lists containers; all includes stopped ones.
func (c *Client) Containers(ctx context.Context, all bool) ([]container.Summary, error) {
	return c.FilteredContainers(ctx, all, nil)
}

// FilteredContainers lists containers matching docker ps style filters,
// e.g. {"status": {"exited"}, "label": {"env=prod"}}.
func (c *Client) FilteredContainers(ctx context.Context, all bool, filters map[string][]string) ([]container.Summary, error) {
	opts := client.ContainerListOptions{All: all}
	if len(filters) > 0 {
		opts.Filters = client.Filters{}
		for term, values := range filters {
			opts.Filters.Add(term, values...)
		}
	}
	res, err := c.api.ContainerList(ctx, opts)
	return res.Items, err
}

// Images lists top-level images.
func (c *Client) Images(ctx context.Context) ([]image.Summary, error) {
	res, err := c.api.ImageList(ctx, client.ImageListOptions{})
	return res.Items, err
}

// Inspect returns a container's configuration and current state.
func (c *Client) Inspect(ctx context.Context, id string) (container.InspectResponse, error) {
	res, err := c.api.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	return res.Container, err
}

// Start starts a stopped container, or resumes a paused one.
func (c *Client) Start(ctx context.Context, id string) error {
	info, err := c.api.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return err
	}
	if info.Container.State != nil && info.Container.State.Paused {
		_, err = c.api.ContainerUnpause(ctx, id, client.ContainerUnpauseOptions{})
		return err
	}
	_, err = c.api.ContainerStart(ctx, id, client.ContainerStartOptions{})
	return err
}

func (c *Client) Stop(ctx context.Context, id string) error {
	_, err := c.api.ContainerStop(ctx, id, client.ContainerStopOptions{})
	return err
}

func (c *Client) Restart(ctx context.Context, id string) error {
	_, err := c.api.ContainerRestart(ctx, id, client.ContainerRestartOptions{})
	return err
}

func (c *Client) Pause(ctx context.Context, id string) error {
	_, err := c.api.ContainerPause(ctx, id, client.ContainerPauseOptions{})
	return err
}

// Kill sends SIGKILL, for containers that ignore the stop signal.
func (c *Client) Kill(ctx context.Context, id string) error {
	_, err := c.api.ContainerKill(ctx, id, client.ContainerKillOptions{})
	return err
}

// Remove deletes a container, killing it first if it is running. Its
// anonymous volumes are kept, as with docker rm.
func (c *Client) Remove(ctx context.Context, id string) error {
	_, err := c.api.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
	return err
}
