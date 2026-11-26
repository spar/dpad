package tui

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"dpad/internal/compose"
	"dpad/internal/docker"
	"dpad/internal/render"
)

// downTarget is a container, network or volume that carries the project's label.
type downTarget struct{ kind, id, name, state string }

type composeDownPlanMsg struct {
	view     *composeDownView
	targets  []downTarget
	checking bool
	err      error
}
type composeDownResultsMsg struct {
	view    *composeDownView
	results []projectResult
	done    bool
}

// composeDownView previews and runs `docker compose down` for one project.
type composeDownView struct {
	operation[downTarget]
	project string
	volumes bool
	env     []string
	down    func(ctx context.Context, env []string, name string, volumes bool) (string, error)
}

func newComposeDown(ctx context.Context, c dockerClient, project string, width, height int) (*composeDownView, tea.Cmd) {
	v := &composeDownView{operation: newOperation(ctx, c, func(t downTarget) string { return t.kind + ":" + t.id }), project: project, env: c.CLIEnvironment(), down: compose.Down}
	v.columns = []render.Column{{Header: "KIND"}, {Header: "NAME", Max: 40, Min: 16}, {Header: "STATE"}, {Header: "RESULT"}}
	v.rows = func(r opRow[downTarget]) []row {
		state := r.target.state
		if r.target.kind == "container" {
			state = render.StateStyle(state, 0).Render(state)
		}
		return []row{{key: v.id(r.target), cells: []string{r.target.kind, r.target.name, state, v.result(r)}}}
	}
	v.empty = "nothing left to remove: the project has no containers, networks or volumes"
	v.SetSize(width, height)
	return v, v.plan(false)
}

// result shows what down will do to a target in the preview, then what it did.
func (v *composeDownView) result(r opRow[downTarget]) string {
	switch r.status {
	case "pending":
		switch {
		case r.target.kind != "volume":
			return render.Warn.Render("remove")
		case v.volumes:
			return render.Error.Render("delete (data lost)")
		}
		return render.Dim.Render("keep")
	case "removed":
		return render.Success.Render(r.status)
	case "kept":
		return render.Dim.Render(r.status)
	case "cancelled":
		return render.Warn.Render(r.status)
	case "failed", "unknown":
		return render.Error.Render(r.status)
	}
	return r.status
}

func projectDownTargets(ctx context.Context, c dockerClient, project string) ([]downTarget, error) {
	containers, err := c.Containers(ctx, true)
	if err != nil {
		return nil, err
	}
	networks, err := c.Networks(ctx)
	if err != nil {
		return nil, err
	}
	volumes, _, err := c.Volumes(ctx)
	if err != nil {
		return nil, err
	}
	targets := []downTarget{}
	for _, item := range containers {
		if item.Labels[composeProjectLabel] == project {
			targets = append(targets, downTarget{"container", item.ID, docker.ContainerName(item.Names), string(item.State)})
		}
	}
	for _, n := range networks {
		if n.Labels[composeProjectLabel] == project {
			targets = append(targets, downTarget{"network", n.ID, n.Name, ""})
		}
	}
	for _, vol := range volumes {
		if vol.Labels[composeProjectLabel] == project {
			targets = append(targets, downTarget{"volume", vol.Name, vol.Name, ""})
		}
	}
	kinds := []string{"container", "network", "volume"}
	slices.SortStableFunc(targets, func(a, b downTarget) int {
		return cmp.Or(cmp.Compare(slices.Index(kinds, a.kind), slices.Index(kinds, b.kind)), cmp.Compare(a.name, b.name))
	})
	return targets, nil
}

func (v *composeDownView) plan(checking bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(v.ctx, 10*time.Second)
		defer cancel()
		targets, err := projectDownTargets(ctx, v.client, v.project)
		return composeDownPlanMsg{v, targets, checking, err}
	}
}

// run downs the project, then lists its resources again: what is gone was
// removed, whatever Compose printed.
func (v *composeDownView) run(ctx context.Context, targets []downTarget, volumes bool) <-chan projectResult {
	out := make(chan projectResult, len(targets)+1)
	go func() {
		defer close(out)
		output, err := v.down(ctx, v.env, v.project, volumes)
		checkCtx, cancel := context.WithTimeout(v.ctx, 10*time.Second)
		defer cancel()
		left, listErr := projectDownTargets(checkCtx, v.client, v.project)
		for _, t := range targets {
			status, detail := "removed", ""
			switch {
			case listErr != nil:
				status, detail = "unknown", "could not list resources afterwards: "+listErr.Error()
			case slices.ContainsFunc(left, func(l downTarget) bool { return l.kind == t.kind && l.id == t.id }):
				switch {
				case t.kind == "volume" && !volumes:
					status = "kept"
				case ctx.Err() != nil:
					status = "cancelled"
				default:
					status = "failed"
				}
			}
			out <- projectResult{v.id(t), status, detail}
		}
		if err != nil && ctx.Err() != nil {
			out <- projectResult{"", "", "Stopped before docker compose down finished; whatever it removed is gone."}
		} else if err != nil {
			note := strings.TrimSpace(output)
			if i := strings.LastIndexByte(note, '\n'); i >= 0 {
				note = note[i+1:]
			}
			out <- projectResult{"", "", strings.TrimPrefix(fmt.Sprintf("%s · compose down failed: %v", note, err), " · ")}
		}
	}()
	return out
}

func (v *composeDownView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case composeDownPlanMsg:
		if msg.view != v || v.ctx.Err() != nil {
			return nil
		}
		if msg.err != nil {
			v.stage, v.note = stageError, msg.err.Error()
			return nil
		}
		same := slices.Equal(v.targets, msg.targets)
		v.showTargets(msg.targets)
		if !msg.checking || !same {
			v.stage = stagePreview
			if msg.checking {
				v.note = "The project changed. Review this updated preview and confirm again."
			}
			return nil
		}
		volumes := v.volumes
		v.start(func(ctx context.Context) <-chan projectResult { return v.run(ctx, slices.Clone(v.targets), volumes) })
		return v.wait()
	case composeDownResultsMsg:
		if msg.view != v || v.ctx.Err() != nil {
			return nil
		}
		v.applyResults(msg.results, msg.done)
		if msg.done {
			if v.note == "" {
				v.note = "Finished. The Compose files were not touched."
			}
			return func() tea.Msg { return refreshMsg{} }
		}
		return v.wait()
	}
	return nil
}

func (v *composeDownView) wait() tea.Cmd {
	return v.waitResults(func(batch []projectResult, done bool) tea.Msg {
		return composeDownResultsMsg{view: v, results: batch, done: done}
	})
}

func (v *composeDownView) HandleKey(key string) tea.Cmd {
	switch {
	case key == "y" || key == "Y":
		if v.stage == stagePreview && len(v.targets) > 0 {
			v.stage, v.note = stageChecking, ""
			return v.plan(true)
		}
	case key == "v" && v.stage == stagePreview:
		v.volumes = !v.volumes
		v.rowCache = nil
	case key == "ctrl+r" && (v.stage == stagePreview || v.stage == stageError):
		v.stage, v.note = stageLoading, ""
		return v.plan(false)
	default:
		v.navigate(key)
	}
	return nil
}

func (v *composeDownView) count(kind string) int {
	n := 0
	for _, t := range v.targets {
		if t.kind == kind {
			n++
		}
	}
	return n
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func (v *composeDownView) View() string {
	title := fmt.Sprintf("down project %s · %s · %s", v.project, plural(v.count("container"), "container"), plural(v.count("network"), "network"))
	switch volumes := v.count("volume"); {
	case volumes == 0:
	case v.volumes:
		title += " · " + plural(volumes, "volume") + " will be deleted"
	default:
		title += " · volumes kept"
	}
	note := v.note
	switch v.stage {
	case stageLoading:
		note = "Loading the project's containers, networks and volumes…"
	case stageChecking:
		note = "Checking the project before running down…"
	case stageRunning:
		note = "Running docker compose down…"
	case stageCancelling:
		note = "Stopping docker compose down…"
	}
	if note == "" {
		note = "Runs: docker compose -p " + v.project + " down"
		if v.volumes {
			note += " --volumes"
		}
		note += " · needs no Compose files"
	}
	return v.frame(title, note, v.view)
}

func (v *composeDownView) Status() string {
	counts := v.counts()
	parts := []string{string(v.stage)}
	for i, status := range []string{"removed", "kept", "failed", "cancelled", "unknown"} {
		if i < 3 || counts[status] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[status], status))
		}
	}
	return strings.Join(parts, " · ")
}

func (v *composeDownView) Footer() string {
	switch {
	case v.busy():
		return hints("esc", "stop", "↑↓/pgup/pgdn", "results")
	case v.stage == stagePreview && len(v.targets) > 0 && v.volumes:
		return hints("y", "down + delete volumes", "v", "keep volumes", "esc", "back", "↑↓", "rows", "ctrl+r", "refresh")
	case v.stage == stagePreview && len(v.targets) > 0:
		return hints("y", "confirm down", "v", "also delete volumes", "esc", "back", "↑↓", "rows", "ctrl+r", "refresh")
	case v.stage == stageError || v.stage == stagePreview:
		return hints("esc", "back", "↑↓/pgup/pgdn", "rows", "ctrl+r", "refresh preview")
	}
	return hints("esc", "back", "↑↓/pgup/pgdn", "results")
}

func (v *composeDownView) handleKey(a *App, msg tea.KeyPressMsg) tea.Cmd {
	if cancelKey(msg.String()) {
		if dismiss(v) {
			a.composeDown = nil
		}
		return nil
	}
	return v.HandleKey(msg.String())
}
