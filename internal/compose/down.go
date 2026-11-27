package compose

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"

	"dpad/internal/debuglog"
)

// DownArgs is the Compose command Down runs, for showing in a preview.
func DownArgs(name string, volumes bool) []string {
	args := []string{"--ansi", "never", "--project-name", name, "down"}
	if volumes {
		args = append(args, "--volumes")
	}
	return args
}

// Down runs `compose down` for a project by name alone, so it still works
// after the project's folder is gone. It runs on this machine against the
// daemon in env (SSH endpoints included), from an empty folder and without
// COMPOSE_FILE, so Compose can't pick up an unrelated project's files.
func Down(ctx context.Context, env []string, name string, volumes bool) (string, error) {
	dir, err := os.MkdirTemp("", "dpad-down-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	env = slices.DeleteFunc(slices.Clone(env), func(entry string) bool {
		key, _, _ := strings.Cut(entry, "=")
		return strings.EqualFold(key, "COMPOSE_FILE") || strings.EqualFold(key, "COMPOSE_PROJECT_NAME")
	})
	var errs []error
	for _, cli := range [][]string{{"docker", "compose"}, {"docker-compose"}} {
		executable, err := exec.LookPath(cli[0])
		if err != nil {
			errs = append(errs, err)
			continue
		}
		run := func(args ...string) *exec.Cmd {
			cmd := exec.CommandContext(ctx, executable, append(slices.Clone(cli[1:]), args...)...)
			cmd.Env, cmd.Dir = env, dir
			configureCommand(cmd)
			debuglog.Info("compose command", "args", cmd.Args)
			return cmd
		}
		version, err := run("version", "--short").Output()
		if v := strings.TrimPrefix(strings.TrimSpace(string(version)), "v"); err != nil || len(v) < 2 || v[0] < '2' || v[0] > '9' || v[1] != '.' {
			errs = append(errs, errors.New(strings.Join(cli, " ")+" v2 or later is not available"))
			continue
		}
		out := &limitedOutput{limit: 32 << 10}
		cmd := run(DownArgs(name, volumes)...)
		cmd.Stdout, cmd.Stderr = out, out
		return out.String(), cmd.Run()
	}
	return "", errors.Join(errs...)
}
