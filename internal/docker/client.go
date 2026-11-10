// Package docker wraps the Docker Engine SDK with the queries dockernext needs.
package docker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
)

// Client is a thin wrapper around the Engine API client.
type Client struct {
	api *client.Client
}

// New connects to the same daemon the docker CLI would use: DOCKER_HOST if set,
// otherwise the endpoint of the current docker context.
func New() (*Client, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if os.Getenv("DOCKER_HOST") == "" {
		if host := contextHost(); host != "" {
			opts = append(opts, client.WithHost(host))
		}
	}
	api, err := client.New(opts...)
	if err != nil {
		return nil, err
	}
	return &Client{api: api}, nil
}

func (c *Client) Close() error { return c.api.Close() }

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

// contextHost resolves the docker endpoint of the active docker context
// (DOCKER_CONTEXT or currentContext in config.json). Returns "" for the default context.
func contextHost() string {
	cfgDir := os.Getenv("DOCKER_CONFIG")
	if cfgDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		cfgDir = filepath.Join(home, ".docker")
	}

	name := os.Getenv("DOCKER_CONTEXT")
	if name == "" {
		var cfg struct {
			CurrentContext string `json:"currentContext"`
		}
		if b, err := os.ReadFile(filepath.Join(cfgDir, "config.json")); err == nil {
			_ = json.Unmarshal(b, &cfg)
		}
		name = cfg.CurrentContext
	}
	if name == "" || name == "default" {
		return ""
	}

	sum := sha256.Sum256([]byte(name))
	b, err := os.ReadFile(filepath.Join(cfgDir, "contexts", "meta", hex.EncodeToString(sum[:]), "meta.json"))
	if err != nil {
		return ""
	}
	var meta struct {
		Endpoints map[string]struct {
			Host string `json:"Host"`
		} `json:"Endpoints"`
	}
	if json.Unmarshal(b, &meta) != nil {
		return ""
	}
	return meta.Endpoints["docker"].Host
}
