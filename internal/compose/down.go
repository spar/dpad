package compose

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"

	"dpad/internal/debuglog"
	"dpad/internal/docker"
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
// When this machine has no Compose CLI but the endpoint is ssh://, it runs
// on the SSH host instead, as remote projects do.
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
	installed := false
	for _, cli := range composeCLIs {
		executable, err := exec.LookPath(cli[0])
		if err != nil {
			errs = append(errs, err)
			continue
		}
		installed = true
		run := func(args ...string) *exec.Cmd {
			cmd := exec.CommandContext(ctx, executable, append(slices.Clone(cli[1:]), args...)...)
			cmd.Env, cmd.Dir = env, dir
			configureCommand(cmd)
			debuglog.Info("compose command", "args", cmd.Args)
			return cmd
		}
		version, err := run("version", "--short").Output()
		if _, ok := composeV2(version); err != nil || !ok {
			errs = append(errs, errors.New(strings.Join(cli, " ")+" v2 or later is not available"))
			continue
		}
		out := &limitedOutput{limit: 32 << 10}
		cmd := run(DownArgs(name, volumes)...)
		cmd.Stdout, cmd.Stderr = out, out
		err = cmd.Run()
		return out.String(), err
	}
	if target, err := docker.ParseSSHHost(envValue(env, "DOCKER_HOST")); err == nil && !installed {
		return downRemote(ctx, env, target, name, volumes)
	}
	return "", errors.Join(errs...)
}

func downRemote(ctx context.Context, env []string, target docker.SSHTarget, name string, volumes bool) (string, error) {
	args := make([]string, 0, 8)
	for _, arg := range DownArgs(name, volumes) {
		args = append(args, quote(arg))
	}
	down := strings.Join(args, " ")
	script := "cd / && unset COMPOSE_FILE COMPOSE_PROJECT_NAME && " +
		"export DOCKER_HOST=" + quote("unix://"+target.Socket) + " DOCKER_CONTEXT= DOCKER_TLS= DOCKER_TLS_VERIFY= DOCKER_CERT_PATH= && " +
		"if docker compose version >/dev/null 2>&1; then exec docker compose " + down + "; else exec docker-compose " + down + "; fi"
	cmd, err := sshScript(ctx, env, script, false)
	if err != nil {
		return "", err
	}
	out := &limitedOutput{limit: 32 << 10}
	cmd.Stdout, cmd.Stderr = out, out
	err = cmd.Run()
	return out.String(), err
}
