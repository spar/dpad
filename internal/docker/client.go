// Package docker wraps the Docker Engine SDK with the queries dockernext needs.
package docker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
)

// Client is a thin wrapper around the Engine API client.
type Client struct {
	api    engine
	tunnel *sshTunnel // set for ssh:// endpoints
	label  string
	cliEnv []string
}

// engine is the part of the Engine API client dockernext uses, so tests can
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
	ContainerLogs(context.Context, string, client.ContainerLogsOptions) (client.ContainerLogsResult, error)
	ContainerStats(context.Context, string, client.ContainerStatsOptions) (client.ContainerStatsResult, error)
	Events(context.Context, client.EventsListOptions) client.EventsResult
}

var _ engine = (*client.Client)(nil)

// New connects to the same daemon the docker CLI would use: DOCKER_HOST if set,
// otherwise the endpoint of the current docker context. An ssh:// endpoint is
// reached through an ssh tunnel to the remote daemon socket.
func New() (*Client, error) {
	opts := []client.Opt{client.FromEnv} // API version negotiation is on by default
	name, host := "", os.Getenv("DOCKER_HOST")
	if host == "" {
		var err error
		name, host, err = contextEndpoint()
		if err != nil {
			return nil, err
		}
		if host != "" && !isSSHHost(host) {
			opts = append(opts, client.WithHost(host))
		}
	}
	var tunnel *sshTunnel
	if isSSHHost(host) {
		var err error
		if tunnel, err = startSSHTunnel(host); err != nil {
			return nil, err
		}
		opts = append(opts, client.WithHost("unix://"+tunnel.socket), client.WithDialContext(tunnel.dial))
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
	return &Client{api: api, tunnel: tunnel, label: connectionLabel(name, daemon), cliEnv: env}, nil
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
	res, err := c.api.ContainerList(ctx, client.ContainerListOptions{All: all})
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

// contextEndpoint resolves DOCKER_CONTEXT or currentContext in config.json.
// Only the default context may fall back to the SDK's default endpoint.
func contextEndpoint() (string, string, error) {
	cfgDir := os.Getenv("DOCKER_CONFIG")
	if cfgDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", err
		}
		cfgDir = filepath.Join(home, ".docker")
	}

	name := os.Getenv("DOCKER_CONTEXT")
	if name == "" {
		var cfg struct {
			CurrentContext string `json:"currentContext"`
		}
		b, err := os.ReadFile(filepath.Join(cfgDir, "config.json"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", "", fmt.Errorf("read docker config: %w", err)
		}
		if err == nil {
			if err := json.Unmarshal(b, &cfg); err != nil {
				return "", "", fmt.Errorf("read docker config: %w", err)
			}
		}
		name = cfg.CurrentContext
	}
	if name == "" || name == "default" {
		return "default", "", nil
	}

	sum := sha256.Sum256([]byte(name))
	b, err := os.ReadFile(filepath.Join(cfgDir, "contexts", "meta", hex.EncodeToString(sum[:]), "meta.json"))
	if err != nil {
		return "", "", fmt.Errorf("read docker context %q: %w", name, err)
	}
	var meta struct {
		Endpoints map[string]struct {
			Host string `json:"Host"`
		} `json:"Endpoints"`
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		return "", "", fmt.Errorf("read docker context %q: %w", name, err)
	}
	host := meta.Endpoints["docker"].Host
	if host == "" {
		return "", "", fmt.Errorf("docker context %q has no docker endpoint", name)
	}
	return name, host, nil
}
