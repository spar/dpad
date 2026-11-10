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
				var (
					rows     [][]string
					total    int64
					dangling int
				)
				for _, img := range items {
					rows = append(rows, render.ImageRows(img)...)
					total += img.Size
					if len(img.RepoTags) == 0 {
						dangling++
					}
				}
				fmt.Println(render.Table{Columns: render.ImageColumns, Rows: rows})
				fmt.Println()
				fmt.Println(render.Dim.Render(fmt.Sprintf("%d images · %s total · %d dangling", len(items), render.Size(total), dangling)))
				return nil
			})
		},
	}
	addSortFlags(cmd, &sortKey, &reverse, sortx.Size, sortx.ImageKeys)
	return cmd
}
