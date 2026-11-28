package cmd

import (
	"cmp"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/spar/dpad/internal/render"
	"github.com/spar/dpad/internal/theme"
)

// setupTheme applies the theme named by the --theme flag, else $DPAD_THEME,
// else configured, the config file's. The default theme stays when none is
// named or the named one can't be loaded.
func setupTheme(flag, configured string) error {
	name := cmp.Or(flag, os.Getenv("DPAD_THEME"), configured)
	if name == "" {
		return nil
	}
	dir, err := theme.Dir()
	if err != nil {
		return err
	}
	t, err := theme.Load(name, dir)
	if err != nil {
		return err
	}
	render.ApplyTheme(t)
	return nil
}

func newThemesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "themes",
		Short: "List color themes; choose one with --theme, $DPAD_THEME or theme: in config.yaml",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			dir, err := theme.Dir()
			if err != nil {
				return err
			}
			var rows [][]string
			for _, entry := range theme.All(dir) {
				name, t := entry.Name, entry.Theme
				if entry.Err != nil {
					rows = append(rows, []string{name, "", render.Error.Render(entry.Err.Error()), ""})
					continue
				}
				if name == render.Theme().Name {
					name = render.Bold.Render(name + " *")
				}
				rows = append(rows, []string{name, render.Swatch(t), t.Description, render.Dim.Render(cmp.Or(t.Path, "built-in"))})
			}
			cols := []render.Column{{Header: "THEME"}, {Header: "PREVIEW"}, {Header: "DESCRIPTION"}, {Header: "FROM"}}
			fmt.Fprintln(render.Out, render.Table{Columns: cols, Rows: rows})
			fmt.Fprintln(render.Out)
			fmt.Fprintln(render.Out, render.Dim.Render("* active · your own themes go in "+dir))
			return nil
		},
	}
}
