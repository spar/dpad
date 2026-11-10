// Package cmd defines the dn command line.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"dockernext/internal/docker"
	"dockernext/internal/passthrough"
	"dockernext/internal/render"
	"dockernext/internal/sortx"
	"dockernext/internal/tui"
)

var version = "dev"

var noColor bool

var (
	// errSilent signals failure when the details were already printed.
	errSilent = errors.New("")
	// errPassthrough asks run to hand the command to docker instead.
	errPassthrough = errors.New("passthrough to docker")
)

// Execute runs dn. Commands and flags dn doesn't know are forwarded to docker.
func Execute() { os.Exit(run(os.Args[1:])) }

// run executes dn with args and returns the process exit code.
func run(args []string) int {
	root := newRoot()
	if shouldPassthrough(root, args) {
		return passthrough.Run(dockerArgs(args, false))
	}
	root.SetArgs(args)
	err := root.Execute()
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errPassthrough):
		return passthrough.Run(dockerArgs(args, true))
	case !errors.Is(err, errSilent):
		fmt.Fprintln(os.Stderr, "dn:", err)
	}
	return 1
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "dn",
		Short: "dockernext: a colorful, interactive companion to the docker CLI",
		Long: `dockernext enhances common docker commands with colors and sorting,
and opens an interactive TUI when run without arguments.

Any command or flag dn doesn't handle itself is passed through to docker,
so "dn network ls" or "dn ps --format ..." behave exactly like docker.`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		PersistentPreRun: func(*cobra.Command, []string) {
			render.SetupColor(noColor)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withClient(cmd.Context(), func(ctx context.Context, c *docker.Client) error {
				return tui.Run(ctx, c)
			})
		},
	}
	root.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output (also honors NO_COLOR)")
	root.AddCommand(newPsCmd(), newImagesCmd(), newStatsCmd(), newLogsCmd())

	// Flags we don't implement (e.g. `ps --format`) mean the user wants docker's behavior.
	root.SetFlagErrorFunc(func(*cobra.Command, error) error { return errPassthrough })
	return root
}

// shouldPassthrough reports whether args target a docker command dn doesn't enhance.
func shouldPassthrough(root *cobra.Command, args []string) bool {
	// Leading dn flags don't decide anything: `dn --no-color network ls` is
	// still a docker command.
	for len(args) > 0 && isDNFlag(args[0]) {
		args = args[1:]
	}
	if len(args) == 0 {
		return false
	}
	if strings.HasPrefix(args[0], "-") {
		// Leading flags: ours (help/version) stay here, docker global
		// flags like --context or -H go to docker.
		switch strings.SplitN(args[0], "=", 2)[0] {
		case "-h", "--help", "-v", "--version":
			return false
		}
		return true
	}
	cmd, _, err := root.Find(args)
	return err != nil || cmd == root
}

// dockerArgs removes dn's own flags from args bound for docker, which would
// reject them. With anywhere set (dn's own commands) they are dropped up to
// "--"; otherwise only before the command name, because later arguments may
// belong to a program run inside a container.
func dockerArgs(args []string, anywhere bool) []string {
	out := make([]string, 0, len(args))
	for i, a := range args {
		if a == "--" || !anywhere && !strings.HasPrefix(a, "-") {
			return append(out, args[i:]...)
		}
		if !isDNFlag(a) {
			out = append(out, a)
		}
	}
	return out
}

// isDNFlag reports whether arg is a flag only dn understands.
func isDNFlag(arg string) bool {
	return arg == "--no-color" || strings.HasPrefix(arg, "--no-color=")
}

func withClient(ctx context.Context, fn func(context.Context, *docker.Client) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	c, err := docker.New()
	if err != nil {
		return err
	}
	defer c.Close()
	return fn(ctx, c)
}

func addSortFlags(cmd *cobra.Command, key *string, reverse *bool, def sortx.Key, keys []sortx.Key) {
	cmd.Flags().StringVar(key, "sort", string(def), "sort by: "+sortx.Join(keys, ", "))
	cmd.Flags().BoolVarP(reverse, "reverse", "r", false, "reverse the sort order")
}
