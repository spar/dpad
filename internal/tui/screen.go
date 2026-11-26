package tui

import tea "charm.land/bubbletea/v2"

// screen is anything drawn over the tabs: logs, a details pane, a picker or
// a previewed operation. The App shows the top one and sends it keys.
type screen interface {
	View() string
	Status() string
	Footer() string
	Update(tea.Msg) tea.Cmd
	SetSize(width, height int)
	// handleKey handles a key while the screen is on top. It closes the
	// screen or opens another through a.
	handleKey(a *App, msg tea.KeyPressMsg) tea.Cmd
}

// screens lists the open screens, top first. A screen opened from another
// (logs from details, details from a network's endpoints) sits above it, so
// closing it returns there.
func (a *App) screens() []screen {
	var open []screen
	if a.composeUpdate != nil {
		open = append(open, a.composeUpdate)
	}
	if a.composeEdit != nil {
		open = append(open, a.composeEdit)
	}
	if a.contextPicker != nil {
		open = append(open, a.contextPicker)
	}
	if a.themePicker != nil {
		open = append(open, a.themePicker)
	}
	if a.editorPicker != nil {
		open = append(open, a.editorPicker)
	}
	if a.settings != nil {
		open = append(open, a.settings)
	}
	if a.projectAction != nil {
		open = append(open, a.projectAction)
	}
	if a.composeDown != nil {
		open = append(open, a.composeDown)
	}
	if a.prune != nil {
		open = append(open, a.prune)
	}
	if a.logs != nil {
		open = append(open, a.logs)
	}
	if a.details != nil {
		open = append(open, a.details)
	}
	if a.networkDetails != nil {
		open = append(open, a.networkDetails)
	}
	if a.volumeDetails != nil {
		open = append(open, a.volumeDetails)
	}
	if a.imageDetails != nil {
		open = append(open, a.imageDetails)
	}
	return open
}

// screen returns the top screen, or nil when the tabs are showing.
func (a *App) screen() screen {
	if open := a.screens(); len(open) > 0 {
		return open[0]
	}
	return nil
}

// closeKey reports whether key leaves a pane.
func closeKey(key string) bool { return key == "esc" || key == "q" }

// cancelKey reports whether key leaves an operation, where n also answers
// its y/n preview.
func cancelKey(key string) bool { return closeKey(key) || key == "n" }
