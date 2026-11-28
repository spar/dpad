package tui

import (
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/spar/dpad/internal/config"
	"github.com/spar/dpad/internal/render"
)

// applyIcons turns container icons on for icons: nerd, or for auto when the
// terminal draws Nerd Font glyphs without the user installing a font.
func (a *App) applyIcons() {
	render.SetIcons(a.icons == render.IconsNerd || a.icons == render.IconsAuto && nerdFontBuiltIn(a.terminal))
	if a.settings != nil {
		a.settings.rowCache = nil
	}
}

func (a *App) cycleIcons() {
	i := slices.Index(render.IconModes, a.icons)
	a.icons = render.IconModes[(i+1)%len(render.IconModes)]
	a.applyIcons()
	if err := config.SaveIcons(a.icons); err != nil {
		a.flash, a.flashErr = "icons: "+a.icons+" for now; could not save: "+oneLine(err.Error()), true
	}
}

func (a *App) iconsValue() string {
	value := a.icons
	if a.icons == render.IconsAuto {
		value += " → " + render.IconsOff
		if render.IconsOn() {
			value = a.icons + " → " + render.IconsNerd
		}
	}
	return value + " " + render.Dim.Render("· Nerd Font icons before names; enter cycles")
}

// terminalFromEnv names the terminal before it answers XTVERSION, from
// variables that survive ssh (TERM) or are set by the terminal itself.
func terminalFromEnv() string {
	if os.Getenv("TERM") == "xterm-ghostty" {
		return "ghostty"
	}
	return os.Getenv("TERM_PROGRAM")
}

// nerdFontBuiltIn reports whether the terminal ships Nerd Font symbols, so
// icons draw without a patched font: Ghostty, WezTerm, and kitty since 0.36.
// name is an XTVERSION reply such as "kitty(0.36.4)" or "ghostty 1.1.3", or
// $TERM_PROGRAM. Inside tmux or screen the reply names the multiplexer, which
// says nothing about the font, so icons stay off there.
func nerdFontBuiltIn(name string) bool {
	name = strings.ToLower(name)
	switch {
	case strings.HasPrefix(name, "ghostty"), strings.HasPrefix(name, "wezterm"):
		return true
	case strings.HasPrefix(name, "kitty("):
		major, minor, _ := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(name, "kitty("), ")"), ".")
		minor, _, _ = strings.Cut(minor, ".")
		ma, err1 := strconv.Atoi(major)
		mi, err2 := strconv.Atoi(minor)
		return err1 == nil && err2 == nil && (ma > 0 || mi >= 36)
	}
	return false
}
