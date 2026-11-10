// Package passthrough hands commands dockernext doesn't enhance to the docker CLI.
package passthrough

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
)

// Run executes `docker args...` with inherited stdio and returns its exit code.
func Run(args []string) int {
	path, err := exec.LookPath("docker")
	if err != nil {
		fmt.Fprintln(os.Stderr, "dn: docker CLI not found in PATH")
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
		fmt.Fprintln(os.Stderr, "dn:", err)
		return 1
	}
	return 0
}
