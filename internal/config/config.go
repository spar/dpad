// Package config loads the optional dpad configuration file.
package config

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"text/template"

	"go.yaml.in/yaml/v3"
)

// Config is the contents of config.yaml. A missing file is the zero Config
// with Mouse on.
type Config struct {
	Mouse           *bool                     `yaml:"mouse"`
	CustomCommands  []CustomCommand           `yaml:"customCommands"`
	ComposeProjects map[string]ComposeProject `yaml:"composeProjects"`
	// Theme names the color theme; see docs/themes.md.
	Theme string `yaml:"theme"`
	// Editor is the command that edits Compose files, such as "nano" or
	// "code --wait". Empty means $VISUAL, then $EDITOR.
	Editor string `yaml:"editor"`
	// CPUGraph is how the Containers tab draws CPU: braille, bars, meter, or
	// off to hide the CPU and memory columns. Empty means braille.
	CPUGraph string `yaml:"cpuGraph"`
	// Icons puts a Nerd Font icon before container names: auto (only in
	// terminals that ship the glyphs), nerd, or off. Empty means auto.
	Icons string `yaml:"icons"`
}

// ComposeProject holds per-project settings for Compose updates, keyed by
// Compose project name.
type ComposeProject struct {
	// Env is set while dpad runs Compose for the project, for variables a
	// wrapper script would normally export.
	Env map[string]string `yaml:"env"`
}

// MouseEnabled reports whether the TUI should capture the mouse.
func (c Config) MouseEnabled() bool { return c.Mouse == nil || *c.Mouse }

// CustomCommand is a user command bound to a key in the TUI.
type CustomCommand struct {
	Name    string `yaml:"name"`
	Key     string `yaml:"key"`
	Scope   string `yaml:"scope"`
	Command string `yaml:"command"`
	Attach  bool   `yaml:"attach"`
	Confirm bool   `yaml:"confirm"`

	Template *template.Template `yaml:"-"`
}

const (
	ScopeContainer = "container"
	ScopeProject   = "project"
)

// ReservedKeys are bound by the TUI and cannot be used by custom commands.
var ReservedKeys = []string{
	"q", "ctrl+c", "tab", "shift+tab", "left", "right", "up", "down", "h", "j", "k", "l",
	"/", "esc", "?", "ctrl+r", "enter", "space", "pgup", "pgdown", "ctrl+u", "ctrl+d", "home", "end", "g", "G",
	"d", "e", "S", "x", "X", "R", "P", "K", "D", "u", "U", "c", "C", "o",
	"s", "r", "t", "v", "m", "n", "i", "p", "a",
	"1", "2", "3", "4", "5", "6", "7", "8", "9",
}

// Path returns where the config file is read from: $DPAD_CONFIG, else
// $XDG_CONFIG_HOME/dpad/config.yaml, else ~/.config/dpad/config.yaml
// (%AppData%\dpad\config.yaml on Windows).
func Path() (string, error) {
	if p := os.Getenv("DPAD_CONFIG"); p != "" {
		return p, nil
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "dpad", "config.yaml"), nil
	}
	if runtime.GOOS == "windows" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "dpad", "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "dpad", "config.yaml"), nil
}

// Load reads the config file. Invalid custom commands are left out and
// reported in the error, so the rest of the file still applies.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	cfg, err := Parse(data)
	if err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Parse decodes and validates a config file's contents.
func Parse(data []byte) (Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	var problems []error
	seen := map[string]string{}
	valid := cfg.CustomCommands[:0]
	for i, c := range cfg.CustomCommands {
		c.Scope = cmp.Or(c.Scope, ScopeContainer)
		label := c.Name
		if label == "" {
			label = fmt.Sprintf("custom command %d", i+1)
		}
		scopeKey := c.Scope + " " + c.Key
		switch {
		case c.Name == "":
			problems = append(problems, fmt.Errorf("%s: name is required", label))
			continue
		case c.Key == "":
			problems = append(problems, fmt.Errorf("%s: key is required", label))
			continue
		case slices.Contains(ReservedKeys, c.Key):
			problems = append(problems, fmt.Errorf("%s: key %q is already used by dpad", label, c.Key))
			continue
		case c.Scope != ScopeContainer && c.Scope != ScopeProject:
			problems = append(problems, fmt.Errorf("%s: scope must be %q or %q", label, ScopeContainer, ScopeProject))
			continue
		case strings.TrimSpace(c.Command) == "":
			problems = append(problems, fmt.Errorf("%s: command is required", label))
			continue
		case seen[scopeKey] != "":
			problems = append(problems, fmt.Errorf("%s: key %q is already used by %s", label, c.Key, seen[scopeKey]))
			continue
		}
		t, err := template.New(c.Name).Parse(c.Command)
		if err == nil {
			err = t.Execute(io.Discard, Vars{})
		}
		if err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", label, err))
			continue
		}
		c.Template = t
		seen[scopeKey] = c.Name
		valid = append(valid, c)
	}
	cfg.CustomCommands = valid
	return cfg, errors.Join(problems...)
}

// Vars are the values a command template can use, e.g. {{.ID}}. Each is
// inserted already quoted for the shell that runs the command.
type Vars struct {
	ID, Name, Image, Project, Service, WorkingDir string
}

// Render fills in the command for the selected container or project.
func (c CustomCommand) Render(v Vars) (string, error) {
	q := Vars{quote(v.ID), quote(v.Name), quote(v.Image), quote(v.Project), quote(v.Service), quote(v.WorkingDir)}
	var b strings.Builder
	if err := c.Template.Execute(&b, q); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Shell returns the program and arguments that run a rendered command:
// sh on macOS and Linux, PowerShell on Windows.
func Shell(command string) []string {
	if runtime.GOOS == "windows" {
		return []string{"powershell.exe", "-NoProfile", "-Command", command}
	}
	return []string{"/bin/sh", "-c", command}
}

// quote makes s a single literal word. Both sh and PowerShell treat
// single-quoted text literally; only the quote itself needs escaping.
// PowerShell also ends a quoted string at typographic single quotes.
func quote(s string) string {
	if runtime.GOOS == "windows" {
		for _, q := range []string{"'", "\u2018", "\u2019", "\u201a", "\u201b"} {
			s = strings.ReplaceAll(s, q, q+q)
		}
		return "'" + s + "'"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// SaveTheme sets theme: in the config file, creating the file if needed.
func SaveTheme(name string) error { return save("theme", name) }

// SaveEditor sets editor: in the config file; an empty command removes it.
func SaveEditor(command string) error { return save("editor", command) }

// SaveCPUGraph sets cpuGraph: in the config file.
func SaveCPUGraph(style string) error { return save("cpuGraph", style) }

// SaveIcons sets icons: in the config file.
func SaveIcons(mode string) error { return save("icons", mode) }

// save sets one top-level key in the config file, or removes it when value
// is empty. It edits only that line, so the rest of the file, comments
// included, stays as the user wrote it.
func save(key, value string) error {
	path, err := Path()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	encoded, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	line := key + ": " + strings.TrimSuffix(string(encoded), "\n")
	existing := regexp.MustCompile(`(?m)^` + key + `:.*$`)
	switch {
	case value == "" && !existing.Match(data):
		return nil
	case value == "":
		data = regexp.MustCompile(`(?m)^`+key+`:.*(\n|$)`).ReplaceAll(data, nil)
	case existing.Match(data):
		data = existing.ReplaceAllLiteral(data, []byte(line))
	default:
		if len(data) > 0 && data[len(data)-1] != '\n' {
			data = append(data, '\n')
		}
		data = append(data, line+"\n"...)
	}
	var check map[string]any
	err = yaml.Unmarshal(data, &check)
	if got, _ := check[key].(string); err != nil || got != value {
		return fmt.Errorf("%s: could not set %s: %s; set it by hand", path, key, value)
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
