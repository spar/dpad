package theme

import (
	"fmt"
	"image/color"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"dpad/themes"
)

// TestBuiltinThemes checks every file in themes/, so a new theme only needs
// its YAML file to be covered.
func TestBuiltinThemes(t *testing.T) {
	files, err := fs.Glob(themes.FS, "*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no built-in themes: %v", err)
	}
	for _, file := range files {
		name := strings.TrimSuffix(file, ".yaml")
		t.Run(name, func(t *testing.T) {
			if !validName(name) {
				t.Fatalf("file name %q: use lowercase letters, digits and dashes", file)
			}
			data, _ := themes.FS.ReadFile(file)
			var own Theme
			if err := decode(data, &own); err != nil {
				t.Fatal(err)
			}
			if own.Name != name {
				t.Errorf("name is %q, want %q to match the file name", own.Name, name)
			}
			if own.Description == "" {
				t.Error("description is empty")
			}
			if own.Extends != "" {
				t.Error("built-in themes list every color instead of extending another theme")
			}
			for _, role := range missingRoles(reflect.ValueOf(own), "") {
				t.Errorf("%s is not set; built-in themes set every role", role)
			}
			if len(own.Names) == 0 {
				t.Error("names is empty")
			}
			th, err := Load(name, "")
			if err != nil {
				t.Fatal(err)
			}
			checkContrast(t, th)
		})
	}
}

// missingRoles lists the color roles of v left out of the file, by YAML path.
func missingRoles(v reflect.Value, prefix string) []string {
	var missing []string
	for i := range v.NumField() {
		f, field := v.Field(i), v.Type().Field(i)
		key, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if key == "-" || key == "" {
			continue
		}
		switch c := f.Interface().(type) {
		case Color:
			if !c.Set() {
				missing = append(missing, prefix+key)
			}
		default:
			if f.Kind() == reflect.Struct {
				missing = append(missing, missingRoles(f, prefix+key+".")...)
			}
		}
	}
	return missing
}

// checkContrast holds themes that set a hex background to minimum WCAG
// contrast ratios: 4 for text (WCAG AA asks 4.5, which rules out several
// well-known palettes' reds), 3 for secondary text. Colors given as ANSI names or indexes depend on the
// user's terminal and can't be checked.
func checkContrast(t *testing.T, th *Theme) {
	bg, ok := hexLuminance(th.Terminal.Background)
	if !ok {
		return
	}
	check := func(role string, c Color, against float64, minimum float64) {
		t.Helper()
		l, ok := hexLuminance(c)
		if !ok {
			return
		}
		if r := ratio(l, against); r < minimum {
			t.Errorf("%s %s has contrast %.2f against its background, want at least %.1f", role, c.Value, r, minimum)
		}
	}
	text := map[string]Color{
		"terminal.foreground": th.Terminal.Foreground, "ui.brand": th.UI.Brand, "ui.header": th.UI.Header,
		"ui.accent": th.UI.Accent, "ui.success": th.UI.Success, "ui.warning": th.UI.Warning, "ui.error": th.UI.Error,
		"states.running": th.States.Running, "states.failed": th.States.Failed, "states.paused": th.States.Paused,
		"states.restarting": th.States.Restarting, "states.created": th.States.Created, "states.dead": th.States.Dead,
		"health.healthy": th.Health.Healthy, "health.unhealthy": th.Health.Unhealthy, "health.starting": th.Health.Starting,
		"usage.low": th.Usage.Low, "usage.medium": th.Usage.Medium, "usage.high": th.Usage.High,
		"logs.error": th.Logs.Error, "logs.warn": th.Logs.Warn, "logs.info": th.Logs.Info,
		"columns.ports": th.Columns.Ports, "columns.tag": th.Columns.Tag,
	}
	for role, c := range text {
		check(role, c, bg, 4)
	}
	// Secondary text only needs to stay legible.
	for role, c := range map[string]Color{"ui.dim": th.UI.Dim, "states.exited": th.States.Exited, "logs.debug": th.Logs.Debug} {
		check(role, c, bg, 3)
	}
	for i, c := range th.Names {
		check(fmt.Sprintf("names[%d]", i), c, bg, 4)
	}
	// The active tab and the selected row put onAccent text on each tab color.
	onAccent, ok := hexLuminance(th.UI.OnAccent)
	if ok {
		v := reflect.ValueOf(th.Tabs)
		for i := range v.NumField() {
			check("ui.onAccent on tabs."+v.Type().Field(i).Tag.Get("yaml"), v.Field(i).Interface().(Color), onAccent, 4)
		}
	}
	if match, ok := hexLuminance(th.UI.MatchBackground); ok {
		check("ui.matchText on ui.matchBackground", th.UI.MatchText, match, 4)
	}
}

func hexLuminance(c Color) (float64, bool) {
	v := strings.TrimPrefix(strings.ToLower(c.Value), "#")
	if len(v) != 6 || v == strings.ToLower(c.Value) {
		return 0, false
	}
	n, err := strconv.ParseUint(v, 16, 32)
	if err != nil {
		return 0, false
	}
	channel := func(shift uint) float64 {
		s := float64(n>>shift&0xff) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(16) + 0.7152*channel(8) + 0.0722*channel(0), true
}

func ratio(a, b float64) float64 { return (max(a, b) + 0.05) / (min(a, b) + 0.05) }

func TestParseColor(t *testing.T) {
	for in, want := range map[string]string{
		"default": "<nil>", "red": "1", "bright-blue": "12", "gray": "8", "Grey": "8", "white": "7",
		"200": "200", "#00ff41": "#00ff41", "#0F4": "#00ff44",
	} {
		c, err := ParseColor(in)
		if err != nil {
			t.Errorf("ParseColor(%q): %v", in, err)
			continue
		}
		var expect color.Color
		if want != "<nil>" {
			expect = lipgloss.Color(want)
		}
		if !reflect.DeepEqual(c.C(), expect) {
			t.Errorf("ParseColor(%q) = %v, want %s", in, c.C(), want)
		}
	}
	for _, in := range []string{"", "purple", "256", "-1", "#12345", "#gggggg", "bright-default"} {
		if _, err := ParseColor(in); err == nil {
			t.Errorf("ParseColor(%q) accepted an invalid color", in)
		}
	}
}

func writeTheme(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUserThemes(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "mine.yaml", "extends: matrix\ndescription: mine\nui:\n  error: magenta\n")
	writeTheme(t, dir, "default.yaml", "extends: default\nui:\n  brand: \"#123456\"\n")
	writeTheme(t, dir, "partial.yml", "description: only tabs\ntabs:\n  stats: red\n")

	mine, err := Load("mine", dir)
	if err != nil {
		t.Fatal(err)
	}
	matrix, _ := Load("matrix", "")
	if mine.UI.Error.Value != "magenta" || mine.UI.Brand != matrix.UI.Brand || mine.Name != "mine" || mine.Path == "" {
		t.Errorf("mine did not extend matrix with its own error color: %+v", mine.UI)
	}

	def, err := Load("default", dir)
	if err != nil {
		t.Fatal(err)
	}
	if def.UI.Brand.Value != "#123456" || def.UI.Error.Value != "red" {
		t.Errorf("a user default.yaml should override the built-in one it extends: %+v", def.UI)
	}

	partial, err := Load("partial", dir)
	if err != nil {
		t.Fatal(err)
	}
	if partial.Tabs.Stats.Value != "red" || partial.Tabs.Images.Value != "bright-blue" {
		t.Errorf("partial theme did not start from default: %+v", partial.Tabs)
	}

	byPath, err := Load(filepath.Join(dir, "mine.yaml"), "")
	if err != nil || byPath.Name != "mine" {
		t.Errorf("load by path = %v, %v", byPath, err)
	}

	names := Names(dir)
	if names[0] != "default" || !slices.Contains(names, "mine") || !slices.Contains(names, "partial") || !slices.Contains(names, "matrix") {
		t.Errorf("Names = %q", names)
	}
}

func TestThemeErrors(t *testing.T) {
	dir := t.TempDir()
	for file, body := range map[string]string{
		"typo.yaml":     "ui:\n  eror: red\n",
		"unquoted.yaml": "ui:\n  error: #ff0000\n",
		"loop-a.yaml":   "extends: loop-b\n",
		"loop-b.yaml":   "extends: loop-a\n",
		"renamed.yaml":  "name: other\n",
		"bad.yaml":      "tabs:\n  stats: chartreuse\n",
		"nobase.yaml":   "extends: missing\n",
	} {
		writeTheme(t, dir, file, body)
	}
	for name, want := range map[string]string{
		"typo":     "field eror not found",
		"unquoted": "quote hex colors",
		"loop-a":   "extends itself",
		"renamed":  "must match the file name",
		"bad":      `unknown color "chartreuse"`,
		"nobase":   `unknown theme "missing"`,
		"absent":   `unknown theme "absent"`,
		"../etc":   "invalid theme name",
	} {
		if _, err := Load(name, dir); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Load(%q) error = %v, want it to mention %q", name, err, want)
		}
	}
}
