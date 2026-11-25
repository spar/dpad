// Package themes embeds the built-in color themes. Each theme is one YAML
// file in this directory; see docs/themes.md for the format.
package themes

import "embed"

//go:embed *.yaml
var FS embed.FS
