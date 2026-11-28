// Package theme loads color themes: the built-in ones embedded from the
// themes directory and the user's own from the dpad config directory.
package theme

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"image/color"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"go.yaml.in/yaml/v3"

	"github.com/spar/dpad/internal/config"
	"github.com/spar/dpad/themes"
)

// DefaultName is the theme used when none is chosen.
const DefaultName = "default"

// Theme is one theme file. Every color role is listed in docs/themes.md.
type Theme struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Author      string `yaml:"author"`
	Source      string `yaml:"source"`
	// Extends names the theme this one starts from; default when empty.
	Extends string `yaml:"extends"`

	Terminal struct {
		Background Color `yaml:"background"`
		Foreground Color `yaml:"foreground"`
	} `yaml:"terminal"`

	UI struct {
		Brand           Color `yaml:"brand"`
		Dim             Color `yaml:"dim"`
		Header          Color `yaml:"header"`
		Accent          Color `yaml:"accent"`
		Success         Color `yaml:"success"`
		Warning         Color `yaml:"warning"`
		Error           Color `yaml:"error"`
		OnAccent        Color `yaml:"onAccent"`
		MatchText       Color `yaml:"matchText"`
		MatchBackground Color `yaml:"matchBackground"`
	} `yaml:"ui"`

	Tabs struct {
		Containers Color `yaml:"containers"`
		Images     Color `yaml:"images"`
		Stats      Color `yaml:"stats"`
		Projects   Color `yaml:"projects"`
		Events     Color `yaml:"events"`
		Volumes    Color `yaml:"volumes"`
		Networks   Color `yaml:"networks"`
	} `yaml:"tabs"`

	States struct {
		Running    Color `yaml:"running"`
		Exited     Color `yaml:"exited"`
		Failed     Color `yaml:"failed"`
		Paused     Color `yaml:"paused"`
		Restarting Color `yaml:"restarting"`
		Created    Color `yaml:"created"`
		Dead       Color `yaml:"dead"`
	} `yaml:"states"`

	Health struct {
		Healthy   Color `yaml:"healthy"`
		Unhealthy Color `yaml:"unhealthy"`
		Starting  Color `yaml:"starting"`
	} `yaml:"health"`

	Usage struct {
		Low       Color `yaml:"low"`
		Medium    Color `yaml:"medium"`
		High      Color `yaml:"high"`
		Track     Color `yaml:"track"`
		Sparkline Color `yaml:"sparkline"`
	} `yaml:"usage"`

	Logs struct {
		Error Color `yaml:"error"`
		Warn  Color `yaml:"warn"`
		Info  Color `yaml:"info"`
		Debug Color `yaml:"debug"`
	} `yaml:"logs"`

	Columns struct {
		Ports Color `yaml:"ports"`
		Tag   Color `yaml:"tag"`
	} `yaml:"columns"`

	Names []Color `yaml:"names"`

	// Path is where a user theme was read from; empty for built-in themes.
	Path string `yaml:"-"`
}

// TabAccents returns the tab colors in tab order.
func (t *Theme) TabAccents() []color.Color {
	tabs := t.Tabs
	return []color.Color{
		tabs.Containers.C(), tabs.Images.C(), tabs.Stats.C(), tabs.Projects.C(),
		tabs.Events.C(), tabs.Volumes.C(), tabs.Networks.C(),
	}
}

// Color is a theme color: "default" (the terminal's own color), an ANSI
// color name such as "red" or "bright-blue", an xterm palette index from 0
// to 255, or a hex RGB value such as "#00ff41".
type Color struct {
	Value string
	c     color.Color
}

// C is the color to render with, or nil for the terminal default.
func (c Color) C() color.Color { return c.c }

// Set reports whether the color was given in a file.
func (c Color) Set() bool { return c.Value != "" }

var ansiNames = []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}

// ParseColor reads one color value.
func ParseColor(s string) (Color, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	switch {
	case v == "default":
		return Color{Value: s}, nil
	case strings.HasPrefix(v, "#"):
		hex := v[1:]
		if len(hex) == 3 {
			hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
		}
		if _, err := strconv.ParseUint(hex, 16, 32); err != nil || len(hex) != 6 {
			return Color{}, fmt.Errorf("%q is not a hex color like #00ff41", s)
		}
		return Color{Value: s, c: lipgloss.Color("#" + hex)}, nil
	}
	if n, err := strconv.Atoi(v); err == nil {
		if n < 0 || n > 255 {
			return Color{}, fmt.Errorf("color index %d is outside 0-255", n)
		}
		return Color{Value: s, c: lipgloss.Color(strconv.Itoa(n))}, nil
	}
	name := strings.TrimPrefix(v, "bright-")
	if v == "gray" || v == "grey" {
		name, v = "black", "bright-black"
	}
	if i := slices.Index(ansiNames, name); i >= 0 {
		if name != v {
			i += 8
		}
		return Color{Value: s, c: lipgloss.Color(strconv.Itoa(i))}, nil
	}
	return Color{}, fmt.Errorf("unknown color %q: use default, an ANSI name like red or bright-blue, 0-255, or #rrggbb", s)
}

func (c *Color) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode || n.Tag == "!!null" {
		return fmt.Errorf("line %d: a color must be a single value; quote hex colors (\"#00ff41\") since # starts a YAML comment", n.Line)
	}
	parsed, err := ParseColor(n.Value)
	if err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	*c = parsed
	return nil
}

// Dir is the user's theme directory: themes/ next to config.yaml.
func Dir() (string, error) {
	path, err := config.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "themes"), nil
}

// Default returns the built-in default theme.
func Default() *Theme {
	t, err := Load(DefaultName, "")
	if err != nil {
		panic(err)
	}
	return t
}

// Load returns the named theme. A name ending in .yaml or .yml is read as a
// file path. Otherwise a file in userDir wins over a built-in theme of the
// same name. Roles the file leaves out come from the theme it extends.
func Load(name, userDir string) (*Theme, error) {
	return load(name, userDir, nil)
}

func load(name, userDir string, seen []string) (*Theme, error) {
	data, path, err := read(name, userDir)
	if err != nil {
		return nil, err
	}
	id := cmp.Or(path, "built-in "+name)
	if slices.Contains(seen, id) {
		return nil, fmt.Errorf("theme %s extends itself", name)
	}
	var head struct {
		Extends string `yaml:"extends"`
	}
	if err := yaml.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("theme %s: %w", name, err)
	}
	t := &Theme{}
	if head.Extends != "" || name != DefaultName || path != "" {
		base := cmp.Or(head.Extends, DefaultName)
		dir := userDir
		if base == name {
			// A user theme that extends the built-in theme it replaces.
			dir = ""
		}
		parent, err := load(base, dir, append(seen, id))
		if err != nil {
			return nil, fmt.Errorf("theme %s: %w", name, err)
		}
		*t = *parent
		t.Names = slices.Clone(parent.Names)
		t.Name, t.Description, t.Author, t.Source, t.Extends = "", "", "", "", ""
	}
	if err := decode(data, t); err != nil {
		return nil, fmt.Errorf("theme %s: %w", name, err)
	}
	switch {
	case isPath(name):
		t.Name = cmp.Or(t.Name, strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
	case t.Name == "":
		t.Name = name
	case t.Name != name:
		return nil, fmt.Errorf("theme %s: name is %q but must match the file name", name, t.Name)
	}
	t.Path = path
	if len(t.Names) == 0 {
		return nil, fmt.Errorf("theme %s: names needs at least one color", name)
	}
	return t, nil
}

// decode reads a theme file into t strictly, so a misspelled role is an
// error rather than silently ignored.
func decode(data []byte, t *Theme) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	if err := checkValues(&doc); err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(t); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// checkValues rejects keys without a value, which YAML would otherwise
// read as empty: most often an unquoted hex color, since # starts a comment.
func checkValues(n *yaml.Node) error {
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if v := n.Content[i+1]; v.Kind == yaml.ScalarNode && v.Tag == "!!null" {
				return fmt.Errorf("line %d: %s has no value; quote hex colors (\"#00ff41\") since # starts a YAML comment", v.Line, n.Content[i].Value)
			}
		}
	}
	for _, c := range n.Content {
		if err := checkValues(c); err != nil {
			return err
		}
	}
	return nil
}

func read(name, userDir string) (data []byte, path string, err error) {
	if isPath(name) {
		data, err := os.ReadFile(name)
		return data, name, err
	}
	if !validName(name) {
		return nil, "", fmt.Errorf("invalid theme name %q", name)
	}
	if userDir != "" {
		for _, ext := range []string{".yaml", ".yml"} {
			p := filepath.Join(userDir, name+ext)
			data, err := os.ReadFile(p)
			if err == nil {
				return data, p, nil
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return nil, "", err
			}
		}
	}
	data, err = themes.FS.ReadFile(name + ".yaml")
	if err != nil {
		return nil, "", fmt.Errorf("unknown theme %q (run dp themes to list them)", name)
	}
	return data, "", nil
}

func isPath(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}

func validName(name string) bool {
	return name != "" && strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789-") == ""
}

// Listed is one theme from All: the theme, or why it could not be loaded.
type Listed struct {
	Name  string
	Theme *Theme
	Err   error
}

// All loads every theme Names lists.
func All(userDir string) []Listed {
	var out []Listed
	for _, name := range Names(userDir) {
		t, err := Load(name, userDir)
		out = append(out, Listed{Name: name, Theme: t, Err: err})
	}
	return out
}

// Names lists the built-in themes and those in userDir, default first.
func Names(userDir string) []string {
	var names []string
	add := func(file string) {
		ext := filepath.Ext(file)
		if name := strings.TrimSuffix(file, ext); (ext == ".yaml" || ext == ".yml") && validName(name) && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	if entries, err := fs.ReadDir(themes.FS, "."); err == nil {
		for _, e := range entries {
			add(e.Name())
		}
	}
	if userDir != "" {
		if entries, err := os.ReadDir(userDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					add(e.Name())
				}
			}
		}
	}
	slices.SortFunc(names, func(a, b string) int {
		switch {
		case a == DefaultName:
			return -1
		case b == DefaultName:
			return 1
		}
		return strings.Compare(a, b)
	})
	return names
}
