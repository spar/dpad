// Package passthrough hands commands dpad doesn't enhance to the docker CLI.
package passthrough

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
)

// Run executes `docker args...` with inherited stdio and returns its exit code.
func Run(args []string) int {
	path, err := exec.LookPath("docker")
	if err != nil {
		fmt.Fprintf(os.Stderr, "dp: 'docker %s' needs the docker CLI, which is not installed or not in PATH.\n"+
			"dp's own ps, images, stats and logs, and the TUI, work without it.\n", strings.Join(args, " "))
		return 127
	}
	cmd := exec.Command(path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	// The terminal delivers ctrl+c to docker too; let docker decide how to exit.
	signal.Ignore(os.Interrupt)
	defer signal.Reset(os.Interrupt)

	if err := cmd.Run(); err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "dp:", err)
		return 1
	}
	return 0
}
