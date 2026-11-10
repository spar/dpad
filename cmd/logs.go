package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sync"

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
	ch := make(chan docker.LogLine, 256)
	var (
		wg     sync.WaitGroup
		failed bool
		mu     sync.Mutex
	)
	for _, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Logs(ctx, name, opts, ch); err != nil && ctx.Err() == nil {
				mu.Lock()
				failed = true
				mu.Unlock()
				fmt.Fprintf(os.Stderr, "dn: %s: %v\n", name, err)
			}
		}()
	}
	go func() {
		wg.Wait()
		close(ch)
	}()

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
	print := func(l docker.LogLine) {
		if grep != nil && !grep.MatchString(l.Text) {
			return
		}
		if prefix > 0 {
			prefix = max(prefix, len(l.Container))
		}
		fmt.Fprintln(out, render.LogLine(l, colors.Style(l.Container), prefix, timestamps, grep))
	}

	if opts.Follow {
		// Print as lines arrive, flushing whenever the stream goes idle.
		for {
			select {
			case l, ok := <-ch:
				if !ok {
					return logsErr(failed)
				}
				print(l)
			default:
				out.Flush()
				l, ok := <-ch
				if !ok {
					return logsErr(failed)
				}
				print(l)
			}
		}
	}

	// Without -f, merge all containers chronologically.
	var lines []docker.LogLine
	for l := range ch {
		lines = append(lines, l)
	}
	slices.SortStableFunc(lines, func(a, b docker.LogLine) int { return a.Time.Compare(b.Time) })
	for _, l := range lines {
		print(l)
	}
	return logsErr(failed)
}

func logsErr(failed bool) error {
	if failed {
		return errSilent
	}
	return nil
}
