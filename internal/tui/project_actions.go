package tui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"

	"dpad/internal/docker"
	"dpad/internal/render"
	"dpad/internal/sortx"
)

const projectWorkers = 4

type projectTarget struct {
	id, name, service string
	state             container.ContainerState
}
type projectResult struct{ id, status, detail string }
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
type projectActionRow struct {
	target         projectTarget
	status, detail string
}

type projectActionView struct {
	list[projectActionRow]
	ctx                        context.Context
	cancel                     context.CancelFunc
	operationCancel            context.CancelFunc
	client                     dockerClient
	project, verb, stage, note string
	targets                    []projectTarget
	skipped                    int
	results                    <-chan projectResult
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
	ctx, cancel := context.WithCancel(ctx)
	v := &projectActionView{ctx: ctx, cancel: cancel, client: c, project: project, verb: verb, stage: "loading"}
	v.list = list[projectActionRow]{
		columns: []render.Column{{Header: "CONTAINER", Max: 35}, {Header: "SERVICE", Max: 30}, {Header: "STATE"}, {Header: "RESULT"}},
		sort:    func([]projectActionRow, sortx.Key, bool) {},
		rows: func(r projectActionRow) []row {
			return []row{{key: r.target.id, cells: []string{r.target.name, r.target.service, string(r.target.state), r.status}}}
		},
		empty: "no eligible containers", loaded: true,
	}
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

func (v *projectActionView) showTargets(targets []projectTarget) {
	rows := make([]projectActionRow, len(targets))
	for i, t := range targets {
		rows[i] = projectActionRow{target: t, status: "pending"}
	}
	v.setItems(rows, nil)
}

func (v *projectActionView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case projectPlanMsg:
		if msg.view != v || v.ctx.Err() != nil {
			return nil
		}
		if msg.err != nil {
			v.stage = "error"
			v.note = msg.err.Error()
			return nil
		}
		same := slices.Equal(v.targets, msg.targets)
		v.targets, v.skipped = msg.targets, msg.skipped
		v.showTargets(msg.targets)
		if !msg.checking || !same {
			v.stage = "preview"
			if msg.checking {
				v.note = "Targets changed. Review this updated preview and confirm again."
			}
			return nil
		}
		v.stage = "running"
		ctx, cancel := context.WithCancel(v.ctx)
		v.operationCancel = cancel
		v.results = runProjectActions(ctx, v.ctx, v.client, v.verb, slices.Clone(v.targets))
		return v.waitResults()
	case projectResultsMsg:
		if msg.view != v || v.ctx.Err() != nil {
			return nil
		}
		for _, result := range msg.results {
			for i := range v.items {
				if v.items[i].target.id == result.id {
					v.items[i].status, v.items[i].detail = result.status, result.detail
					break
				}
			}
		}
		v.rowCache = nil
		if msg.done {
			v.stage = "done"
			v.note = "Finished. Return to Projects to view current state or run another action."
			v.operationCancel()
			return func() tea.Msg { return refreshMsg{} }
		}
		return v.waitResults()
	}
	return nil
}

func (v *projectActionView) waitResults() tea.Cmd {
	return waitActionBatch(v.ctx, v.results, func(batch []projectResult, done bool) tea.Msg {
		return projectResultsMsg{view: v, results: batch, done: done}
	})
}

func runProjectActions(ctx, viewCtx context.Context, c dockerClient, verb string, targets []projectTarget) <-chan projectResult {
	out := make(chan projectResult, 64)
	go func() {
		defer close(out)
		send := func(r projectResult) {
			select {
			case out <- r:
			case <-viewCtx.Done():
			}
		}
		jobs := make(chan projectTarget)
		var workers sync.WaitGroup
		for range min(projectWorkers, len(targets)) {
			workers.Go(func() {
				for t := range jobs {
					if ctx.Err() != nil {
						send(projectResult{t.id, "cancelled", "Not started"})
						continue
					}
					send(projectResult{t.id, "running", ""})
					actionCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
					var err error
					switch verb {
					case "start":
						err = c.Start(actionCtx, t.id)
					case "stop":
						err = c.Stop(actionCtx, t.id)
					case "restart":
						err = c.Restart(actionCtx, t.id)
					}
					cancel()
					result := projectResult{id: t.id, status: "succeeded"}
					if err != nil {
						result.status, result.detail = "failed", err.Error()
						if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
							result.status = "unknown"
							result.detail += "; request may have applied, refresh container state"
						}
					}
					send(result)
				}
			})
		}
		for _, t := range targets {
			select {
			case jobs <- t:
			case <-viewCtx.Done():
				close(jobs)
				workers.Wait()
				return
			}
		}
		close(jobs)
		workers.Wait()
	}()
	return out
}

func (v *projectActionView) HandleKey(key string) tea.Cmd {
	if key == "y" || key == "Y" {
		if v.stage == "preview" && len(v.targets) > 0 {
			v.stage = "checking"
			v.note = ""
			return v.plan(true)
		}
		return nil
	}
	if key == "ctrl+r" && (v.stage == "preview" || v.stage == "error") {
		v.stage = "loading"
		v.note = ""
		return v.plan(false)
	}
	_, keys := v.visible()
	v.cur.navKey(keys, key, v.page)
	return nil
}

func (v *projectActionView) busy() bool { return v.stage == "running" || v.stage == "cancelling" }

func (v *projectActionView) SetSize(width, height int) {
	v.list.SetSize(width, height)
	v.page = max(height-4, 1)
}

func (v *projectActionView) Close() {
	v.cancel()
	if v.operationCancel != nil {
		v.operationCancel()
	}
}
func (v *projectActionView) View() string {
	title := fmt.Sprintf("%s project %s · %d eligible · %d skipped", v.verb, v.project, len(v.targets), v.skipped)
	note := v.note
	switch v.stage {
	case "loading":
		note = "Loading current project containers…"
	case "checking":
		note = "Checking targets before starting…"
	case "running":
		note = "Applying to existing containers, up to 4 at a time…"
	case "cancelling":
		note = "Cancelling remaining work; waiting for in-flight requests…"
	}
	if note == "" {
		note = "Review targets. Only existing containers are affected; Compose dependencies are not ordered."
		if v.verb == "restart" {
			note = "Only running containers will restart; stopped containers are skipped."
		}
	}
	_, keys := v.visible()
	if len(keys) > 0 {
		selected := keys[v.cur.index(keys)]
		for _, r := range v.items {
			if r.target.id == selected && r.detail != "" {
				note = r.detail
				break
			}
		}
	}
	note = strings.NewReplacer("\r", " ", "\n", " ").Replace(note)
	lines := []string{ansi.Truncate(render.Bold.Render(title), v.width, ""), ansi.Truncate(render.Dim.Render(note), v.width, "")}
	if v.height > 2 {
		lines = append(lines, strings.Split(v.view(max(v.height-2, 1)), "\n")...)
	}
	return strings.Join(lines[:min(len(lines), max(v.height, 1))], "\n")
}
func (v *projectActionView) Status() string {
	counts := map[string]int{}
	for _, r := range v.items {
		counts[r.status]++
	}
	return fmt.Sprintf("%s · %d succeeded · %d failed · %d unknown · %d cancelled", v.stage, counts["succeeded"], counts["failed"], counts["unknown"], counts["cancelled"])
}
func (v *projectActionView) Footer() string {
	if v.busy() {
		return hints("esc", "cancel remaining", "↑↓/pgup/pgdn", "results")
	}
	if v.stage == "preview" && len(v.targets) > 0 {
		return hints("y", "confirm "+v.verb, "esc", "back", "↑↓/pgup/pgdn", "targets", "ctrl+r", "refresh")
	}
	if v.stage == "error" || v.stage == "preview" {
		return hints("esc", "back", "↑↓/pgup/pgdn", "results", "ctrl+r", "refresh preview")
	}
	return hints("esc", "back", "↑↓/pgup/pgdn", "results")
}
