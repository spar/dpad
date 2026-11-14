// Package compose updates discovered projects using their original Compose files.
package compose

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
)

const projectLabel = "com.docker.compose.project"

// Project is an immutable update preview; Issue makes a row ineligible.
type Project struct {
	Name, Directory   string
	Files, EnvFiles   []string
	Members, Services []string
	Images            []string
	Remote            bool
	Location          string // empty detects available files; "local" and "ssh" are explicit choices
	CLI               []string
	ConfigHash        [32]byte
	Issue             string
}

// Discover includes stopped projects and ignores the dashboard's list filter.
func Discover(items []container.Summary, selected string) []Project {
	projects := map[string]*Project{}
	for _, c := range items {
		name := c.Labels[projectLabel]
		if name == "" || selected != "" && name != selected {
			continue
		}
		dir := c.Labels[projectLabel+".working_dir"]
		files := splitPaths(c.Labels[projectLabel+".config_files"])
		envFiles := splitPaths(c.Labels[projectLabel+".environment_file"])
		p := projects[name]
		if p == nil {
			p = &Project{Name: name, Directory: dir, Files: files, EnvFiles: envFiles}
			projects[name] = p
		} else if p.Directory != dir || !slices.Equal(p.Files, files) || !slices.Equal(p.EnvFiles, envFiles) {
			p.Issue = "containers disagree about the Compose directory, files, or environment files"
		}
		if dir == "" || len(files) == 0 {
			p.Issue = "missing Compose working-directory or config-file labels"
		}
		active := c.State == container.StateRunning || c.State == container.StatePaused || c.State == container.StateRestarting
		if service := c.Labels["com.docker.compose.service"]; service != "" && active && !strings.EqualFold(c.Labels["com.docker.compose.oneoff"], "true") {
			p.Members = append(p.Members, service)
		}
	}
	out := make([]Project, 0, len(projects))
	for _, p := range projects {
		slices.Sort(p.Members)
		p.Members = slices.Compact(p.Members)
		out = append(out, *p)
	}
	slices.SortFunc(out, func(a, b Project) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

func splitPaths(raw string) []string {
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

// Prepare validates source files and resolved configuration without pulling or
// changing containers. Only configuration hashes and image names are retained.
func Prepare(ctx context.Context, env []string, projects []Project) []Project {
	out := slices.Clone(projects)
	for i := range out {
		p := &out[i]
		if p.Issue != "" {
			continue
		}
		if err := prepare(ctx, env, p); err != nil {
			p.Issue = err.Error()
		}
	}
	return out
}

func prepare(ctx context.Context, env []string, p *Project) error {
	p.Remote = false
	if !filepath.IsAbs(p.Directory) && !path.IsAbs(p.Directory) || strings.ContainsAny(p.Directory, "\x00\r\n") {
		return errors.New("compose directory must be an absolute path without control characters")
	}
	for i, file := range p.Files {
		if !filepath.IsAbs(file) && !path.IsAbs(file) {
			p.Files = slices.Clone(p.Files)
			p.Files[i] = sourcePath(p.Directory, file)
		}
	}
	for i, file := range p.EnvFiles {
		if !filepath.IsAbs(file) && !path.IsAbs(file) {
			p.EnvFiles = slices.Clone(p.EnvFiles)
			p.EnvFiles[i] = sourcePath(p.Directory, file)
		}
	}
	local := true
	for _, file := range append(append([]string{p.Directory}, p.Files...), p.EnvFiles...) {
		if _, err := os.Stat(file); err != nil {
			local = false
			break
		}
	}
	if p.Location == "local" && !local {
		return errors.New("compose files are unavailable on this machine")
	}
	if p.Location == "ssh" || !local {
		u, err := url.Parse(envValue(env, "DOCKER_HOST"))
		if err != nil || u.Scheme != "ssh" || u.Hostname() == "" {
			return errors.New("compose files are unavailable locally; remote execution requires an SSH Docker endpoint")
		}
		if !path.IsAbs(p.Directory) {
			return errors.New("remote Compose execution requires an absolute POSIX directory")
		}
		p.Remote = true
	}
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for _, cli := range [][]string{{"docker", "compose"}, {"docker-compose"}} {
		p.CLI = cli
		cmd, err := command(probeCtx, env, *p, "version", "--short")
		if err != nil {
			continue
		}
		b, err := cmd.Output()
		version := strings.TrimPrefix(strings.TrimSpace(string(b)), "v")
		if err == nil && len(version) > 1 && version[0] >= '2' && version[0] <= '9' && version[1] == '.' {
			return resolve(probeCtx, env, p)
		}
	}
	return errors.New("docker compose v2 or later is required on the execution host; SSH also requires noninteractive authentication")
}

func sourcePath(dir, file string) string {
	if path.IsAbs(dir) {
		return path.Join(dir, file)
	}
	return filepath.Join(dir, file)
}

func resolvedConfig(ctx context.Context, env []string, p Project, allProfiles bool) ([]byte, error) {
	args := baseArgs(p)
	if allProfiles {
		args = append(args, "--profile", "*")
	}
	cmd, err := command(ctx, env, p, append(args, "config", "--format", "json")...)
	if err != nil {
		return nil, err
	}
	var config, stderr limitedOutput
	config.limit, stderr.limit = 4<<20, 8192
	cmd.Stdout, cmd.Stderr = &config, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("read Compose configuration: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if config.overflow {
		return nil, errors.New("resolved Compose configuration exceeds 4 MiB")
	}
	return []byte(config.String()), nil
}

func resolve(ctx context.Context, env []string, p *Project) error {
	full, err := resolvedConfig(ctx, env, *p, true)
	if err != nil {
		return err
	}
	active, err := resolvedConfig(ctx, env, *p, false)
	if err != nil {
		return err
	}
	var model struct {
		Services map[string]struct {
			Image     string
			DependsOn map[string]json.RawMessage `json:"depends_on"`
		}
	}
	if err := json.Unmarshal(full, &model); err != nil {
		return fmt.Errorf("parse Compose configuration: %w", err)
	}
	var enabled struct{ Services map[string]json.RawMessage }
	if err := json.Unmarshal(active, &enabled); err != nil {
		return fmt.Errorf("parse active Compose configuration: %w", err)
	}
	p.ConfigHash = sha256.Sum256(append(append(full, '\n'), active...))
	p.Services, p.Images = nil, nil
	for name := range model.Services {
		if _, ok := enabled.Services[name]; ok || slices.Contains(p.Members, name) {
			p.Services = append(p.Services, name)
		}
	}
	included := map[string]bool{}
	for _, name := range p.Services {
		included[name] = true
	}
	for i := 0; i < len(p.Services); i++ {
		for dependency := range model.Services[p.Services[i]].DependsOn {
			if _, ok := model.Services[dependency]; !ok {
				return fmt.Errorf("dependency %q is absent from Compose configuration", dependency)
			}
			if !included[dependency] {
				included[dependency] = true
				p.Services = append(p.Services, dependency)
			}
		}
	}
	for _, name := range p.Services {
		if image := model.Services[name].Image; image != "" {
			p.Images = append(p.Images, image)
		}
	}
	slices.Sort(p.Services)
	slices.Sort(p.Images)
	p.Images = slices.Compact(p.Images)
	if len(p.Services) == 0 {
		return errors.New("no services selected by the current Compose configuration")
	}
	return nil
}

func baseArgs(p Project) []string {
	args := []string{"--ansi", "never", "--progress", "plain", "--project-directory", p.Directory, "--project-name", p.Name}
	for _, file := range p.Files {
		args = append(args, "--file", file)
	}
	for _, file := range p.EnvFiles {
		args = append(args, "--env-file", file)
	}
	return args
}

func envValue(env []string, key string) string {
	for _, entry := range env {
		name, value, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, key) {
			return value
		}
	}
	return ""
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func command(ctx context.Context, env []string, p Project, args ...string) (*exec.Cmd, error) {
	if !p.Remote {
		executable, err := exec.LookPath(p.CLI[0])
		if err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, executable, append(slices.Clone(p.CLI[1:]), args...)...)
		cmd.Env, cmd.Dir = slices.Clone(env), p.Directory
		configureCommand(cmd)
		return cmd, nil
	}
	u, err := url.Parse(envValue(env, "DOCKER_HOST"))
	if err != nil || u.Scheme != "ssh" || u.Hostname() == "" {
		return nil, errors.New("remote Compose execution requires the bound SSH endpoint")
	}
	socket := u.Path
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	sshArgs := []string{"-n", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=5", "-o", "ServerAliveCountMax=3"}
	if u.User != nil {
		sshArgs = append(sshArgs, "-l", u.User.Username())
	}
	if u.Port() != "" {
		sshArgs = append(sshArgs, "-p", u.Port())
	}
	remote := []string{"env", "DOCKER_HOST=unix://" + socket, "DOCKER_CONTEXT=", "DOCKER_TLS=", "DOCKER_TLS_VERIFY=", "DOCKER_CERT_PATH="}
	remote = append(remote, p.CLI...)
	remote = append(remote, args...)
	for i := range remote {
		remote[i] = quote(remote[i])
	}
	script := "cd " + quote(p.Directory) + " && exec " + strings.Join(remote, " ")
	executable, err := exec.LookPath("ssh")
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, executable, append(sshArgs, "--", u.Hostname(), script)...)
	cmd.Env = slices.Clone(env)
	configureCommand(cmd)
	return cmd, nil
}

// Same includes configuration and execution location so changed previews must
// be confirmed again before any image pull or container recreation.
func Same(a, b []Project) bool { return reflect.DeepEqual(a, b) }
