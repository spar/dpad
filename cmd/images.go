package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"dockernext/internal/docker"
	"dockernext/internal/render"
	"dockernext/internal/sortx"
)

func newImagesCmd() *cobra.Command {
	var (
		sortKey string
		reverse bool
	)
	cmd := &cobra.Command{
		Use:   "images",
		Short: "List images with sizes; dangling images dimmed",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			key, err := sortx.Parse(sortKey, sortx.ImageKeys)
			if err != nil {
				return err
			}
			return withClient(cmd.Context(), func(ctx context.Context, c *docker.Client) error {
				items, err := c.Images(ctx)
				if err != nil {
					return err
				}
				sortx.Images(items, key, reverse)
				var rows [][]string
				for _, img := range items {
					rows = append(rows, render.ImageRows(img)...)
				}
				fmt.Println(render.Table{Columns: render.ImageColumns, Rows: rows})
				fmt.Println()
				fmt.Println(render.Dim.Render(render.ImageSummary(items)))
				return nil
			})
		},
	}
	addSortFlags(cmd, &sortKey, &reverse, sortx.Size, sortx.ImageKeys)
	return cmd
}
