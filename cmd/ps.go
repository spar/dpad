package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/spar/dpad/internal/docker"
	"github.com/spar/dpad/internal/render"
	"github.com/spar/dpad/internal/sortx"
)

func newPsCmd() *cobra.Command {
	var (
		all     bool
		sortKey string
		reverse bool
		filter  []string
	)
	cmd := &cobra.Command{
		Use:   "ps",
		Short: "List containers, colored by state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			key, err := sortx.Parse(sortKey, sortx.ContainerKeys)
			if err != nil {
				return err
			}
			filters, err := parseFilters(filter)
			if err != nil {
				return err
			}
			return withClient(cmd.Context(), func(ctx context.Context, c *docker.Client) error {
				items, err := c.FilteredContainers(ctx, all, filters)
				if err != nil {
					return err
				}
				sortx.Containers(items, key, reverse)
				rows := make([][]string, len(items))
				for i, it := range items {
					rows[i] = render.ContainerRow(it)
				}
				fmt.Fprintln(render.Out, render.Table{Columns: render.ContainerColumns, Rows: rows})
				return nil
			})
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "show all containers (default shows just running)")
	cmd.Flags().StringArrayVarP(&filter, "filter", "f", nil, "filter output based on conditions provided, as docker ps does (e.g. status=exited, label=env=prod)")
	addSortFlags(cmd, &sortKey, &reverse, sortx.Status, sortx.ContainerKeys)
	return cmd
}

// parseFilters turns repeated name=value flags into docker filter terms.
func parseFilters(flags []string) (map[string][]string, error) {
	filters := map[string][]string{}
	for _, f := range flags {
		name, value, ok := strings.Cut(f, "=")
		if !ok || name == "" {
			return nil, fmt.Errorf("bad format of filter %q (expected name=value)", f)
		}
		name = strings.ToLower(strings.TrimSpace(name))
		filters[name] = append(filters[name], value)
	}
	return filters, nil
}
