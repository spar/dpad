package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"sync"
	"sync/atomic"

	"github.com/spf13/cobra"

	"dockernext/internal/docker"
	"dockernext/internal/render"
)

func newLogsCmd() *cobra.Command {
	var (
		opts       docker.LogOptions
		timestamps bool
		grep       string
	)
	cmd := &cobra.Command{
		Use:   "logs CONTAINER [CONTAINER...]",
		Short: "Colored logs from one or more containers",
		Long: `Colored logs from one or more containers.

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
				return streamLogs(ctx, c, args, opts, timestamps, re)
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
				fmt.Fprintf(os.Stderr, "dn: %s: %v\n", name, err)
			}
		}()
	}

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
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
			out.Flush()
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
		wg.Add(1)
		go func() {
			defer wg.Done()
			for l := range src {
				out <- l
			}
		}()
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
