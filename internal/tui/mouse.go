package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// bodyTop is the screen row where tab bodies start, below the tab bar, its
// rule and the connection bar.
const bodyTop = 3

const doubleClick = 400 * time.Millisecond

type lastClick struct {
	tab, line int
	at        time.Time
}

func (a *App) handleMouse(msg tea.MouseMsg) tea.Cmd {
	m := msg.Mouse()
	switch msg.(type) {
	case tea.MouseWheelMsg:
		code := map[tea.MouseButton]rune{tea.MouseWheelUp: tea.KeyUp, tea.MouseWheelDown: tea.KeyDown}[m.Button]
		if code == 0 || a.typing() {
			return nil
		}
		// Scrolling shouldn't dismiss a result shown in the status line.
		flash, flashErr := a.flash, a.flashErr
		cmd := a.handleKey(tea.KeyPressMsg{Code: code})
		if a.flash == "" {
			a.flash, a.flashErr = flash, flashErr
		}
		return cmd
	case tea.MouseClickMsg:
		if m.Button != tea.MouseLeft {
			return nil
		}
		if a.help {
			a.help = false
			return nil
		}
		if a.overlay() || a.typing() {
			return nil
		}
		if m.Y <= 1 {
			if i := a.tabAt(m.X); i >= 0 && i != a.active {
				return a.switchTab(i)
			}
			return nil
		}
		line := m.Y - bodyTop
		if line < 0 || !a.tabs[a.active].clickRow(line) {
			return nil
		}
		now := time.Now()
		double := a.click.tab == a.active && a.click.line == line && now.Sub(a.click.at) < doubleClick
		a.click = lastClick{a.active, line, now}
		if double {
			a.click = lastClick{}
			return a.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
	}
	return nil
}

// typing reports whether keys currently go to a prompt, where a synthetic
// arrow key would edit text or answer a confirmation.
func (a *App) typing() bool {
	return a.confirm != nil || a.shell != nil || a.filtering || a.help ||
		(a.logs != nil && a.logs.searching) || (a.contextPicker != nil && a.contextPicker.filtering)
}

// overlay reports whether a screen covers the tabs.
func (a *App) overlay() bool {
	return a.composeUpdate != nil || a.contextPicker != nil || a.projectAction != nil || a.prune != nil ||
		a.logs != nil || a.activeDetails() != nil
}

// tabAt returns the tab drawn at column x of the tab bar, or -1.
func (a *App) tabAt(x int) int {
	parts, owners := a.tabBarParts()
	pos := 0
	for i, p := range parts {
		w := lipgloss.Width(p)
		if x >= pos && x < pos+w {
			return owners[i]
		}
		pos += w + 1
	}
	return -1
}
