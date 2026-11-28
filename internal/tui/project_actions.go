package tui

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/moby/moby/api/types/container"

	"github.com/spar/dpad/internal/docker"
	"github.com/spar/dpad/internal/render"
)

type projectTarget struct {
	id, name, service string
	state             container.ContainerState
}
type projectPlanMsg struct {
	view     *projectActionView
	targets  []projectTarget
	skipped  int
	checking bool
	err      error
}
type projectResultsMsg struct {
	view    *projectActionView
	results []projectResult
	done    bool
}

type projectActionView struct {
	operation[projectTarget]
	project, verb string
	skipped       int
}

func projectTargets(items []container.Summary, project, verb string) ([]projectTarget, int) {
	targets := []projectTarget{}
	skipped := 0
	for _, c := range items {
		if c.Labels[composeProjectLabel] != project {
			continue
		}
		eligible := false
		switch verb {
		case "start":
			eligible = c.State == container.StateExited || c.State == container.StateCreated || c.State == container.StatePaused
		case "stop":
			eligible = c.State == container.StateRunning || c.State == container.StatePaused || c.State == container.StateRestarting
		case "restart":
			eligible = c.State == container.StateRunning
		}
		if !eligible {
			skipped++
			continue
		}
		targets = append(targets, projectTarget{c.ID, docker.ContainerName(c.Names), c.Labels[composeServiceLabel], c.State})
	}
	slices.SortFunc(targets, func(a, b projectTarget) int { return cmp.Or(cmp.Compare(a.name, b.name), cmp.Compare(a.id, b.id)) })
	return targets, skipped
}

func newProjectAction(ctx context.Context, c dockerClient, project, verb string, width, height int) (*projectActionView, tea.Cmd) {
	v := &projectActionView{operation: newOperation(ctx, c, func(t projectTarget) string { return t.id }), project: project, verb: verb}
	v.columns = []render.Column{{Header: "CONTAINER", Max: 35}, {Header: "SERVICE", Max: 30}, {Header: "STATE"}, {Header: "RESULT"}}
	v.rows = func(r opRow[projectTarget]) []row {
		return []row{{key: r.target.id, cells: []string{r.target.name, r.target.service, string(r.target.state), r.status}}}
	}
	v.empty = "no eligible containers"
	v.SetSize(width, height)
	return v, v.plan(false)
}

func (v *projectActionView) plan(checking bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(v.ctx, 5*time.Second)
		defer cancel()
		items, err := v.client.Containers(ctx, true)
		targets, skipped := projectTargets(items, v.project, v.verb)
		return projectPlanMsg{v, targets, skipped, checking, err}
	}
}

func (v *projectActionView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case projectPlanMsg:
		if msg.view != v || v.ctx.Err() != nil {
			return nil
		}
		if msg.err != nil {
			v.stage, v.note = stageError, msg.err.Error()
			return nil
		}
		same := slices.Equal(v.targets, msg.targets)
		v.skipped = msg.skipped
		v.showTargets(msg.targets)
		if !msg.checking || !same {
			v.stage = stagePreview
			if msg.checking {
				v.note = "Targets changed. Review this updated preview and confirm again."
			}
			return nil
		}
		v.start(func(ctx context.Context) <-chan projectResult {
			return runProjectActions(ctx, v.ctx, v.client, v.verb, slices.Clone(v.targets))
		})
		return v.wait()
	case projectResultsMsg:
		if msg.view != v || v.ctx.Err() != nil {
			return nil
		}
		v.applyResults(msg.results, msg.done)
		if msg.done {
			v.note = "Finished. Return to Projects to view current state or run another action."
			return func() tea.Msg { return refreshMsg{} }
		}
		return v.wait()
	}
	return nil
}

func (v *projectActionView) wait() tea.Cmd {
	return v.waitResults(func(batch []projectResult, done bool) tea.Msg {
		return projectResultsMsg{view: v, results: batch, done: done}
	})
}

func runProjectActions(ctx, viewCtx context.Context, c dockerClient, verb string, targets []projectTarget) <-chan projectResult {
	do := map[string]func(context.Context, string) error{"start": c.Start, "stop": c.Stop, "restart": c.Restart}[verb]
	return runTargets(ctx, viewCtx, targets, projectWorkers, func(t projectTarget) string { return t.id },
		func(ctx context.Context, t projectTarget) error { return do(ctx, t.id) },
		"succeeded", "request may have applied, refresh container state", "")
}

func (v *projectActionView) HandleKey(key string) tea.Cmd {
	if key == "y" || key == "Y" {
		if v.stage == stagePreview && len(v.targets) > 0 {
			v.stage, v.note = stageChecking, ""
			return v.plan(true)
		}
		return nil
	}
	if key == "ctrl+r" && (v.stage == stagePreview || v.stage == stageError) {
		v.stage, v.note = stageLoading, ""
		return v.plan(false)
	}
	v.navigate(key)
	return nil
}

func (v *projectActionView) View() string {
	title := fmt.Sprintf("%s project %s · %d eligible · %d skipped", v.verb, v.project, len(v.targets), v.skipped)
	note := v.note
	switch v.stage {
	case stageLoading:
		note = "Loading current project containers…"
	case stageChecking:
		note = "Checking targets before starting…"
	case stageRunning:
		note = "Applying to existing containers, up to 4 at a time…"
	case stageCancelling:
		note = "Cancelling remaining work; waiting for in-flight requests…"
	}
	if note == "" {
		note = "Review targets. Only existing containers are affected; Compose dependencies are not ordered."
		if v.verb == "restart" {
			note = "Only running containers will restart; stopped containers are skipped."
		}
	}
	return v.frame(title, note, v.view)
}

func (v *projectActionView) Status() string {
	counts := v.counts()
	return fmt.Sprintf("%s · %d succeeded · %d failed · %d unknown · %d cancelled", v.stage, counts["succeeded"], counts["failed"], counts["unknown"], counts["cancelled"])
}

func (v *projectActionView) Footer() string {
	if v.busy() {
		return hints("esc", "cancel remaining", "↑↓/pgup/pgdn", "results")
	}
	if v.stage == stagePreview && len(v.targets) > 0 {
		return hints("y", "confirm "+v.verb, "esc", "back", "↑↓/pgup/pgdn", "targets", "ctrl+r", "refresh")
	}
	if v.stage == stageError || v.stage == stagePreview {
		return hints("esc", "back", "↑↓/pgup/pgdn", "results", "ctrl+r", "refresh preview")
	}
	return hints("esc", "back", "↑↓/pgup/pgdn", "results")
}

func (v *projectActionView) handleKey(a *App, msg tea.KeyPressMsg) tea.Cmd {
	if cancelKey(msg.String()) {
		if dismiss(v) {
			a.projectAction = nil
		}
		return nil
	}
	return v.HandleKey(msg.String())
}
