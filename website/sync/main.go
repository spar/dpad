// Command sync copies the screenshots, theme previews and logo from docs/images
// into website/public/assets, with a light copy of each screenshot for the
// site's light mode, and builds the pages in website/pages and the sitemap.
//
// Run it from the repository root with `make website` after `make screenshots`.
package main

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
)

// shots are the docs/images screenshots the site shows.
var shots = []string{
	"compose-edit.svg", "containers.svg", "demo.svg", "details.svg", "events.svg",
	"logs.svg", "projects.svg", "prune.svg", "settings.svg", "stats.svg", "system.svg",
}

// lightShots picks the light copies of the screenshots. Keep it in step with
// `light` in public/assets/site.js.
//
//	ink:   docs/images/light, captured with the ink theme
//	paper: the dark screenshots recolored with the paper palette
//	white: the paper copies on a white window
const lightShots = "ink"

// paperPalette maps the screenshots' 16 ANSI color classes (p0 to p15) to
// colors from themes/paper.yaml. p0 is the text on a selected row.
var paperPalette = []string{
	"#ffffff", "#cf222e", "#1a7f37", "#8a5d00", "#0550ae", "#8250df", "#0a6f7c", "#424a53",
	"#6a737d", "#a40e26", "#116329", "#7d4e00", "#0969da", "#bf3989", "#1b7c83", "#1f2328",
}

// paperColors replaces the dark terminal window's own colors.
var paperColors = []string{
	"#d8dee9", "#1f2328", // foreground
	"#141721", "#fbfaf6", // background
	"#232838", "#e4e1da", // title bar rule
	"#1b1f2b", "#f0eee8", // title bar
	"#8a91a5", "#6a737d", // title text
	"stroke:#ffffff;stroke-opacity:.07", "stroke:#1f2328;stroke-opacity:.12",
}

var paletteRule = regexp.MustCompile(`\.p(\d+)\{fill:#[0-9a-f]{6}\}\.sp(\d+)\{stroke:#[0-9a-f]{6}\}`)

func main() {
	files, err := outputs(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "website sync:", err)
		os.Exit(1)
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "website sync:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(name, files[name], 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "website sync:", err)
			os.Exit(1)
		}
	}
	fmt.Printf("website sync: wrote %d files to website/public\n", len(files))
}

// outputs returns every file the site copies or derives, keyed by its path
// relative to root.
func outputs(root string) (map[string][]byte, error) {
	out := map[string][]byte{}
	read := func(rel string) ([]byte, error) { return os.ReadFile(filepath.Join(root, rel)) }
	dst := func(rel string) string { return filepath.Join(root, "website/public/assets", rel) }

	for _, name := range shots {
		b, err := read(filepath.Join("docs/images", name))
		if err != nil {
			return nil, err
		}
		light, err := lightCopy(root, name, b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out[dst("shots/"+name)] = b
		out[dst("shots-light/"+name)] = light
	}

	themes, err := filepath.Glob(filepath.Join(root, "docs/images/themes/*.svg"))
	if err != nil {
		return nil, err
	}
	for _, path := range themes {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		out[dst("themes/"+filepath.Base(path))] = b
	}

	for src, name := range map[string]string{"docs/images/logo.svg": "logo.svg", "docs/images/social-preview.png": "og-image.png", "docs/images/cli.svg": "cli.svg"} {
		b, err := read(src)
		if err != nil {
			return nil, err
		}
		out[dst(name)] = b
	}

	pages, err := renderPages(filepath.Join(root, "website/pages"))
	if err != nil {
		return nil, err
	}
	for name, b := range pages {
		out[filepath.Join(root, "website/public", name)] = b
	}
	return out, nil
}

func lightCopy(root, name string, dark []byte) ([]byte, error) {
	switch lightShots {
	case "ink":
		return os.ReadFile(filepath.Join(root, "docs/images/light", name))
	case "white":
		b, err := lighten(dark)
		b = bytes.ReplaceAll(b, []byte("#fbfaf6"), []byte("#ffffff")) // window
		b = bytes.ReplaceAll(b, []byte("#f0eee8"), []byte("#f6f8fa")) // title bar
		return b, err
	default:
		return lighten(dark)
	}
}

// lighten recolors a dark screenshot with the paper palette.
func lighten(svg []byte) ([]byte, error) {
	found := 0
	svg = paletteRule.ReplaceAllFunc(svg, func(rule []byte) []byte {
		m := paletteRule.FindSubmatch(rule)
		i, err := strconv.Atoi(string(m[1]))
		if err != nil || i >= len(paperPalette) {
			return rule
		}
		found++
		return fmt.Appendf(nil, ".p%d{fill:%s}.sp%d{stroke:%s}", i, paperPalette[i], i, paperPalette[i])
	})
	if found != len(paperPalette) {
		return nil, fmt.Errorf("found %d of %d palette colors; has the screenshot format changed?", found, len(paperPalette))
	}
	for i := 0; i < len(paperColors); i += 2 {
		svg = bytes.ReplaceAll(svg, []byte(paperColors[i]), []byte(paperColors[i+1]))
	}
	return svg, nil
}
