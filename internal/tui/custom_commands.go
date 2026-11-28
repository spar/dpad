package tui

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"

	"dpad/internal/config"
	"dpad/internal/docker"
	"dpad/internal/render"
)

type commandDoneMsg struct {
	name, target string
	err          error
}

// configure applies the config file, reporting problems in the status line.
func (a *App) configure(cfg config.Config, err error) {
	a.mouse = cfg.MouseEnabled()
	a.commands = cfg.CustomCommands
	a.composeProjects = cfg.ComposeProjects
	a.editor = cfg.Editor
	graph := cmp.Or(cfg.CPUGraph, defaultCPUGraph)
	if !slices.Contains(render.GraphStyles, graph) {
		err = errors.Join(err, fmt.Errorf("cpuGraph: %q is not one of %s", graph, strings.Join(render.GraphStyles, ", ")))
		graph = defaultCPUGraph
	}
	a.containers().setGraph(graph)
	a.icons = cmp.Or(cfg.Icons, render.IconsAuto)
	if !slices.Contains(render.IconModes, a.icons) {
		err = errors.Join(err, fmt.Errorf("icons: %q is not one of %s", a.icons, strings.Join(render.IconModes, ", ")))
		a.icons = render.IconsAuto
	}
	a.applyIcons()
	switch cfg.HostPanel {
	case "", "on":
	case "off":
		a.system.hidden = true
	default:
		err = errors.Join(err, fmt.Errorf("hostPanel: %q is not on or off", cfg.HostPanel))
	}
	if err != nil && a.flash == "" {
		a.flash, a.flashErr = "config: "+strings.Join(strings.Fields(err.Error()), " "), true
	}
}

// customCommandTarget resolves what a command of scope would act on in the
// active tab: a container row, or a project row in Projects.
func (a *App) customCommandTarget(scope string) (string, config.Vars, bool) {
	if scope == config.ScopeProject {
		projects, ok := a.tabs[a.active].(*ProjectsView)
		if !ok {
			return "", config.Vars{}, false
		}
		p := projects.selectedProject()
		if p == nil {
			return "", config.Vars{}, false
		}
		vars := config.Vars{Project: p.name}
		for _, c := range p.containers {
			if dir := c.Labels[docker.ComposeWorkingDirLabel]; dir != "" {
				vars.WorkingDir = dir
				break
			}
		}
		return "project " + p.name, vars, true
	}
	id, name, ok := a.tabs[a.active].Selected()
	if !ok {
		return "", config.Vars{}, false
	}
	vars := config.Vars{ID: id, Name: name}
	if c, ok := a.knownContainer(id); ok {
		vars.Image = c.Image
		vars.Project = c.Labels[composeProjectLabel]
		vars.Service = c.Labels[composeServiceLabel]
		vars.WorkingDir = c.Labels[docker.ComposeWorkingDirLabel]
	}
	return name, vars, true
}

func (a *App) knownContainer(id string) (container.Summary, bool) {
	for _, t := range a.tabs {
		if v, ok := t.(*ContainersView); ok {
			for _, c := range v.items {
				if c.ID == id {
					return c, true
				}
			}
		}
	}
	return container.Summary{}, false
}

// runCustomCommand runs the command bound to key for the selected row, if any.
func (a *App) runCustomCommand(key string) (tea.Cmd, bool) {
	matched := false
	for _, c := range a.commands {
		if c.Key != key {
			continue
		}
		matched = true
		target, vars, ok := a.customCommandTarget(c.Scope)
		if !ok {
			continue
		}
		line := c.Render(vars)
		start := func() tea.Cmd { return a.startCustomCommand(c, target, line) }
		if c.Confirm {
			a.confirm = &pending{question: fmt.Sprintf("Run %s on %s?", c.Name, target), start: start}
			return nil, true
		}
		return start(), true
	}
	if matched {
		a.flash, a.flashErr = "no custom command for "+key+" applies to this row", true
	}
	return nil, matched
}

func (a *App) startCustomCommand(c config.CustomCommand, target, line string) tea.Cmd {
	args := config.Shell(line)
	cmd := exec.CommandContext(a.ctx, args[0], args[1:]...)
	cmd.Env = a.client.CLIEnvironment()
	if c.Attach {
		return tea.Exec(&shellProcess{cmd: cmd}, func(err error) tea.Msg {
			return commandDoneMsg{name: c.Name, target: target, err: err}
		})
	}
	a.flash = fmt.Sprintf("%s on %s…", c.Name, target)
	a.actionsInFlight++
	return func() tea.Msg {
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		text := fmt.Sprintf("%s on %s: done", c.Name, target)
		if last := lastLine(out.String()); last != "" {
			text += ": " + last
		}
		if err != nil {
			err = fmt.Errorf("%s on %s: %w", c.Name, target, err)
			if last := lastLine(out.String()); last != "" {
				err = fmt.Errorf("%w: %s", err, last)
			}
		}
		return actionDoneMsg{text: text, err: err}
	}
}

// lastLine is the last non-blank line of a command's output, for the status.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(ansi.Strip(s)), "\n")
	return ansi.Truncate(strings.TrimSpace(lines[len(lines)-1]), 200, "…")
}

func (a *App) customCommandHelp() [][2]string {
	var help [][2]string
	for _, c := range a.commands {
		_, projects := a.tabs[a.active].(*ProjectsView)
		if (c.Scope == config.ScopeProject && projects) || (c.Scope == config.ScopeContainer && containerTab(a.tabs[a.active])) {
			help = append(help, [2]string{c.Key, c.Name + " (custom)"})
		}
	}
	return help
}

func containerTab(t tab) bool {
	switch t.(type) {
	case *ContainersView, *StatsView, *ProjectsView, *EventsView:
		return true
	}
	return false
}
