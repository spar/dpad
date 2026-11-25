// Package cmd defines the dp command line.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"dpad/internal/docker"
	"dpad/internal/passthrough"
	"dpad/internal/render"
	"dpad/internal/sortx"
	"dpad/internal/tui"
)

// version is stamped by `make build`; see buildVersion for other builds.
var version = "dev"

// buildVersion returns the stamped version, else the module version Go
// recorded (e.g. by `go install ...@v1.2.0` or a build in a git checkout).
func buildVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

var (
	noColor   bool
	themeName string
	// themeErr is why the chosen theme could not be used; the TUI shows it.
	themeErr error
)

var (
	// errSilent signals failure when the details were already printed.
	errSilent = errors.New("")
	// errPassthrough asks run to hand the command to docker instead.
	errPassthrough = errors.New("passthrough to docker")
)

// Execute runs dp. Commands and flags dp doesn't know are forwarded to docker.
func Execute() { os.Exit(run(os.Args[1:])) }

// run executes dp with args and returns the process exit code.
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
		fmt.Fprintln(os.Stderr, "dp:", err)
	}
	return 1
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "dp",
		Short: "dpad: a colorful, interactive companion to the docker CLI",
		Long: `dpad enhances common docker commands with colors and sorting,
and opens an interactive TUI when run without arguments.

Any command or flag dp doesn't handle itself is passed through to docker,
so "dp network ls" or "dp ps --format ..." behave exactly like docker.`,
		Version:       buildVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			render.SetupColor(noColor)
			themeErr = setupTheme(themeName)
			if themeErr != nil && cmd != cmd.Root() {
				fmt.Fprintf(os.Stderr, "dp: %v; using the default theme\n", themeErr)
			}
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withClient(cmd.Context(), func(ctx context.Context, c *docker.Client) error {
				return tui.Run(ctx, c, themeErr)
			})
		},
	}
	root.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output (also honors NO_COLOR)")
	root.PersistentFlags().StringVar(&themeName, "theme", "", "color theme, by name or path to a .yaml file (also $DPAD_THEME; dp themes lists them)")
	root.AddCommand(newPsCmd(), newImagesCmd(), newStatsCmd(), newLogsCmd(), newThemesCmd())

	// Flags we don't implement (e.g. `ps --format`) mean the user wants docker's behavior.
	root.SetFlagErrorFunc(func(*cobra.Command, error) error { return errPassthrough })
	return root
}

// shouldPassthrough reports whether args target a docker command dp doesn't enhance.
func shouldPassthrough(root *cobra.Command, args []string) bool {
	// Leading dp flags don't decide anything: `dp --no-color network ls` is
	// still a docker command.
	for len(args) > 0 && dpFlagArgs(args) > 0 {
		args = args[dpFlagArgs(args):]
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

// dockerArgs removes dp's own flags from args bound for docker, which would
// reject them. With anywhere set (dp's own commands) they are dropped up to
// "--"; otherwise only before the command name, because later arguments may
// belong to a program run inside a container.
func dockerArgs(args []string, anywhere bool) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); {
		a := args[i]
		if a == "--" || !anywhere && !strings.HasPrefix(a, "-") {
			return append(out, args[i:]...)
		}
		if n := dpFlagArgs(args[i:]); n > 0 {
			i += n
			continue
		}
		out = append(out, a)
		i++
	}
	return out
}

// dpFlagArgs returns how many of args, from the first, are a flag only dp
// understands, together with its value: 0 when args[0] isn't one.
func dpFlagArgs(args []string) int {
	switch a := args[0]; {
	case a == "--no-color" || strings.HasPrefix(a, "--no-color="), strings.HasPrefix(a, "--theme="):
		return 1
	case a == "--theme":
		return min(2, len(args))
	}
	return 0
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
