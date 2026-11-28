package cmd

import (
	"bufio"
	"context"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/moby/moby/api/types/container"
	"github.com/spf13/cobra"

	"github.com/spar/dpad/internal/docker"
	"github.com/spar/dpad/internal/render"
)

const (
	composeProjectLabel = docker.ComposeProjectLabel
	composeServiceLabel = docker.ComposeServiceLabel
	composeOneoffLabel  = docker.ComposeOneoffLabel
)

func newLogsCmd() *cobra.Command {
	var (
		opts       docker.LogOptions
		timestamps bool
		grep       string
	)
	cmd := &cobra.Command{
		Use:   "logs CONTAINER|SERVICE|PROJECT [...]",
		Short: "Colored logs from one or more containers",
		Long: `Colored logs from one or more containers.

Each argument is a container name or ID, a Compose service name (all of its
containers), a Compose project name (every container in it), or a unique
prefix of a container name.

Log levels (ERROR, WARN, INFO, DEBUG, JSON "level" fields, logfmt level=) are
highlighted. With several containers, each line is prefixed with the
container name in a stable color; stderr lines use a red separator.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var re *regexp.Regexp
			if grep != "" {
				var err error
				if re, err = regexp.Compile(grep); err != nil {
					return fmt.Errorf("invalid --grep: %w", err)
				}
			}
			return withClient(cmd.Context(), func(ctx context.Context, c *docker.Client) error {
				list, err := c.Containers(ctx, true)
				if err != nil {
					return err
				}
				names, err := resolveLogTargets(args, list)
				if err != nil {
					return err
				}
				return streamLogs(ctx, c, names, opts, timestamps, re)
			})
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&opts.Follow, "follow", "f", false, "follow log output")
	f.StringVarP(&opts.Tail, "tail", "n", "all", "number of lines to show from the end of each log")
	f.StringVar(&opts.Since, "since", "", "show logs since timestamp or relative (e.g. 42m)")
	f.BoolVarP(&timestamps, "timestamps", "t", false, "show timestamps")
	f.StringVar(&grep, "grep", "", "only show lines matching this regexp, highlighting matches")
	return cmd
}

// resolveLogTargets expands each argument to container names. An exact
// container name or ID wins, then a Compose service, then a Compose project,
// then a unique name prefix. Anything else is left for docker to report.
func resolveLogTargets(args []string, list []container.Summary) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	add := func(names ...string) {
		for _, n := range names {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	for _, arg := range args {
		names, err := resolveLogTarget(arg, list)
		if err != nil {
			return nil, err
		}
		add(names...)
	}
	return out, nil
}

func resolveLogTarget(arg string, list []container.Summary) ([]string, error) {
	var services, projects, prefixed []container.Summary
	for _, c := range list {
		name := docker.ContainerName(c.Names)
		if name == arg || strings.HasPrefix(c.ID, arg) && len(arg) >= 3 {
			return []string{arg}, nil
		}
		// `compose run` containers aren't part of the service's normal output.
		if !strings.EqualFold(c.Labels[composeOneoffLabel], "true") {
			if c.Labels[composeServiceLabel] == arg {
				services = append(services, c)
			}
			if c.Labels[composeProjectLabel] == arg {
				projects = append(projects, c)
			}
		}
		if strings.HasPrefix(name, arg) {
			prefixed = append(prefixed, c)
		}
	}
	if len(services) > 0 {
		inProjects := map[string]bool{}
		for _, c := range services {
			inProjects[c.Labels[composeProjectLabel]] = true
		}
		if len(inProjects) > 1 {
			return nil, fmt.Errorf("%s: service is in several Compose projects (%s); use a container name", arg, strings.Join(slices.Sorted(maps.Keys(inProjects)), ", "))
		}
		return containerNames(services), nil
	}
	if len(projects) > 0 {
		return containerNames(projects), nil
	}
	switch len(prefixed) {
	case 0:
		return []string{arg}, nil
	case 1:
		return containerNames(prefixed), nil
	}
	names := containerNames(prefixed)
	if len(names) > 5 {
		names = append(names[:5], "…")
	}
	return nil, fmt.Errorf("%s: matches several containers (%s)", arg, strings.Join(names, ", "))
}

func containerNames(list []container.Summary) []string {
	names := make([]string, len(list))
	for i, c := range list {
		names[i] = docker.ContainerName(c.Names)
	}
	slices.Sort(names)
	return names
}

func streamLogs(ctx context.Context, c *docker.Client, names []string, opts docker.LogOptions, timestamps bool, grep *regexp.Regexp) error {
	// One channel per container, closed when its stream ends. Each delivers
	// its container's lines in time order.
	sources := make([]<-chan docker.LogLine, len(names))
	var failed atomic.Bool
	for i, name := range names {
		ch := make(chan docker.LogLine, 256)
		sources[i] = ch
		go func() {
			defer close(ch)
			if err := c.Logs(ctx, name, opts, ch); err != nil && ctx.Err() == nil {
				failed.Store(true)
				fmt.Fprintf(os.Stderr, "dp: %s: %v\n", name, err)
			}
		}()
	}

	buf := bufio.NewWriter(os.Stdout)
	defer buf.Flush()
	out := render.NewWriter(buf) // printLine writes whole lines
	// Prefix only when merging several containers. Size it from the
	// arguments up front; it grows if a resolved name is longer (e.g. IDs given).
	prefix := 0
	if len(names) > 1 {
		for _, n := range names {
			prefix = max(prefix, len(n))
		}
	}
	colors := render.NameColors{}
	for _, n := range names {
		colors.Style(n)
	}
	printLine := func(l docker.LogLine) {
		if grep != nil && !grep.MatchString(l.Text) {
			return
		}
		if prefix > 0 {
			prefix = max(prefix, len(l.Container))
		}
		fmt.Fprintln(out, render.LogLine(l, colors.Style(l.Container), prefix, timestamps, grep))
	}

	if !opts.Follow {
		mergeLogs(sources, printLine)
		return logsErr(failed.Load())
	}

	// With -f, print lines as they arrive, flushing whenever output goes idle.
	ch := fanIn(sources)
	for {
		select {
		case l, ok := <-ch:
			if !ok {
				return logsErr(failed.Load())
			}
			printLine(l)
		default:
			buf.Flush()
			l, ok := <-ch
			if !ok {
				return logsErr(failed.Load())
			}
			printLine(l)
		}
	}
}

// mergeLogs emits lines from time-ordered sources in overall time order. It
// holds one pending line per source, so memory stays flat however long the
// logs are. Ties go to the earlier source.
func mergeLogs(sources []<-chan docker.LogLine, emit func(docker.LogLine)) {
	heads := make([]docker.LogLine, len(sources))
	open := make([]bool, len(sources))
	for i, src := range sources {
		heads[i], open[i] = <-src
	}
	for {
		next := -1
		for i := range sources {
			if open[i] && (next < 0 || heads[i].Time.Before(heads[next].Time)) {
				next = i
			}
		}
		if next < 0 {
			return
		}
		emit(heads[next])
		heads[next], open[next] = <-sources[next]
	}
}

// fanIn forwards lines from all sources onto one channel, closed once every
// source is.
func fanIn(sources []<-chan docker.LogLine) <-chan docker.LogLine {
	out := make(chan docker.LogLine, 256)
	var wg sync.WaitGroup
	for _, src := range sources {
		wg.Go(func() {
			for l := range src {
				out <- l
			}
		})
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

func logsErr(failed bool) error {
	if failed {
		return errSilent
	}
	return nil
}
