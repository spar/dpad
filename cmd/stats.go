package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"dockernext/internal/docker"
	"dockernext/internal/render"
	"dockernext/internal/sortx"
	"dockernext/internal/tui"
)

func newStatsCmd() *cobra.Command {
	var (
		sortKey  string
		reverse  bool
		noStream bool
		totals   bool
	)
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Live, sortable container resource usage",
		Long: `Live, sortable container resource usage.

While running, press c/m/n/i/p/a to sort by cpu/mem/net/io/pids/name,
r to reverse, t to toggle I/O rates/totals, q to quit.
Live I/O shows bytes/sec by default; --totals starts with cumulative totals.
Snapshots and piped output always show totals because rates need two samples.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Selecting specific containers is docker's job.
			if len(args) > 0 {
				return errPassthrough
			}
			key, err := sortx.Parse(sortKey, sortx.StatsKeys)
			if err != nil {
				return err
			}
			return withClient(cmd.Context(), func(ctx context.Context, c *docker.Client) error {
				if !noStream && term.IsTerminal(int(os.Stdout.Fd())) {
					return tui.RunStats(ctx, c, key, reverse, totals)
				}
				items, err := c.StatsSnapshot(ctx)
				if err != nil {
					return err
				}
				sortx.Stats(items, key, reverse)
				rows := make([][]string, len(items))
				for i, s := range items {
					rows[i] = render.StatsRow(s)
				}
				fmt.Fprintln(render.Out, render.Table{Columns: render.StatsColumns, Rows: rows})
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&noStream, "no-stream", false, "print a single snapshot and exit")
	cmd.Flags().BoolVar(&totals, "totals", false, "start live stats with cumulative I/O totals instead of rates (t toggles)")
	addSortFlags(cmd, &sortKey, &reverse, sortx.CPU, sortx.StatsKeys)
	return cmd
}
