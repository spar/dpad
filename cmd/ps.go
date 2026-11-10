package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"dockernext/internal/docker"
	"dockernext/internal/render"
	"dockernext/internal/sortx"
)

func newPsCmd() *cobra.Command {
	var (
		all     bool
		key     string
		reverse bool
	)
	cmd := &cobra.Command{
		Use:   "ps",
		Short: "List containers, colored by state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := sortx.Validate(key, sortx.ContainerKeys); err != nil {
				return err
			}
			return withClient(cmd.Context(), func(ctx context.Context, c *docker.Client) error {
				items, err := c.Containers(ctx, all)
				if err != nil {
					return err
				}
				sortx.Containers(items, key, reverse)
				rows := make([][]string, len(items))
				for i, it := range items {
					rows[i] = render.ContainerRow(it)
				}
				fmt.Println(render.Table{Columns: render.ContainerColumns, Rows: rows})
				return nil
			})
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "show all containers (default shows just running)")
	addSortFlags(cmd, &key, &reverse, "status", sortx.ContainerKeys)
	return cmd
}
