package docker

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/moby/moby/client"
)

// Context is a saved daemon choice with a display-safe endpoint.
type Context struct {
	Name, Endpoint string
}

func dockerConfigDir() (string, error) {
	if dir := os.Getenv("DOCKER_CONFIG"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".docker"), nil
}

// Contexts lists locally saved contexts without contacting any daemon.
func Contexts() ([]Context, error) {
	dir, err := dockerConfigDir()
	if err != nil {
		return nil, err
	}
	choices := []Context{{Name: "default", Endpoint: client.DefaultDockerHost}}
	entries, err := os.ReadDir(filepath.Join(dir, "contexts", "meta"))
	if errors.Is(err, os.ErrNotExist) {
		return choices, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list docker contexts: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, "contexts", "meta", entry.Name(), "meta.json"))
		if err != nil {
			return nil, fmt.Errorf("read docker context %q: %w", entry.Name(), err)
		}
		var meta struct {
			Name      string
			Endpoints map[string]struct{ Host string }
		}
		if err := json.Unmarshal(b, &meta); err != nil {
			return nil, fmt.Errorf("read docker context %q: %w", entry.Name(), err)
		}
		if meta.Name == "" || meta.Name == "default" || meta.Endpoints["docker"].Host == "" {
			continue
		}
		choices = append(choices, Context{Name: meta.Name, Endpoint: connectionLabel("", meta.Endpoints["docker"].Host)})
	}
	slices.SortFunc(choices, func(a, b Context) int { return strings.Compare(a.Name, b.Name) })
	return choices, nil
}
