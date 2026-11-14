package docker

import (
	"crypto/sha256"
	"encoding/hex"
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
		var meta struct {
			contextMetadata
			Name string
		}
		if err := readContextMetadata(dir, entry.Name(), &meta); err != nil {
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

// contextEndpoint resolves DOCKER_CONTEXT or currentContext in config.json.
// Only the default context may fall back to the SDK's default endpoint.
func contextEndpoint() (string, string, error) {
	cfgDir, err := dockerConfigDir()
	if err != nil {
		return "", "", err
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
	return readContextEndpoint(cfgDir, name)
}

func readContextEndpoint(cfgDir, name string) (string, string, error) {
	if name == "" || name == "default" {
		return "default", "", nil
	}

	sum := sha256.Sum256([]byte(name))
	var meta contextMetadata
	if err := readContextMetadata(cfgDir, hex.EncodeToString(sum[:]), &meta); err != nil {
		return "", "", fmt.Errorf("read docker context %q: %w", name, err)
	}
	host := meta.Endpoints["docker"].Host
	if host == "" {
		return "", "", fmt.Errorf("docker context %q has no docker endpoint", name)
	}
	return name, host, nil
}

type contextMetadata struct {
	Endpoints map[string]struct{ Host string }
}

func readContextMetadata(cfgDir, directory string, target any) error {
	b, err := os.ReadFile(filepath.Join(cfgDir, "contexts", "meta", directory, "meta.json"))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}
