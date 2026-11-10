package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"dockernext/internal/docker"
	"dockernext/internal/passthrough"
	"dockernext/internal/render"
	"dockernext/internal/sortx"
	"dockernext/internal/tui"
)

func newStatsCmd() *cobra.Command {
	var (
		key      string
		reverse  bool
		noStream bool
	)
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Live, sortable container resource usage",
		Long: `Live, sortable container resource usage.

While running, press c/m/n/i/p/a to sort by cpu/mem/net/io/pids/name,
r to reverse, q to quit. Output is a single snapshot when not a terminal.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Selecting specific containers is docker's job.
			if len(args) > 0 {
				os.Exit(passthrough.Run(dockerArgs(os.Args[1:], true)))
			}
			if err := sortx.Validate(key, sortx.StatsKeys); err != nil {
				return err
			}
			return withClient(cmd.Context(), func(ctx context.Context, c *docker.Client) error {
				if !noStream && term.IsTerminal(int(os.Stdout.Fd())) {
					return tui.RunStats(ctx, c, key, reverse)
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
				fmt.Println(render.Table{
					Headers:  render.StatsHeaders,
					Rows:     rows,
					Align:    render.StatsAlign,
					MaxWidth: render.StatsMax,
					Selected: -1,
				})
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&noStream, "no-stream", false, "print a single snapshot and exit")
	addSortFlags(cmd, &key, &reverse, "cpu", sortx.StatsKeys)
	return cmd
}
