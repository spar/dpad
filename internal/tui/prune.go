package tui

import (
	"context"
	"fmt"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/spar/dpad/internal/docker"
	"github.com/spar/dpad/internal/render"
)

type prunePlanMsg struct {
	view     *pruneView
	kind     docker.PruneKind
	all      bool
	targets  []docker.PruneTarget
	checking bool
	err      error
}
type pruneResultsMsg struct {
	view    *pruneView
	results []projectResult
	done    bool
}

// pruneView previews what a prune removes, rechecks it on confirmation and
// removes exactly those resources, reporting each result.
type pruneView struct {
	operation[docker.PruneTarget]
	kind docker.PruneKind
	all  bool
}

func newPruneView(ctx context.Context, c dockerClient, kind docker.PruneKind, width, height int) (*pruneView, tea.Cmd) {
	v := &pruneView{operation: newOperation(ctx, c, pruneTargetID), kind: kind}
	v.rows = func(r opRow[docker.PruneTarget]) []row {
		size := ""
		if r.target.Size >= 0 {
			size = render.Size(r.target.Size)
		}
		status := r.status
		switch status {
		case "removed":
			status = render.Accent.Render(status)
		case "failed", "unknown":
			status = render.Error.Render(status)
		}
		cells := []string{r.target.Name, r.target.Detail, size, status}
		if len(v.columns) == 3 {
			cells = slices.Delete(cells, 2, 3)
		}
		return []row{{key: r.target.ID, cells: cells}}
	}
	v.empty = "nothing to prune"
	v.SetSize(width, height)
	return v, v.plan(false)
}

func pruneTargetID(t docker.PruneTarget) string { return t.ID }

// allLabel names what the "a" toggle adds, for kinds that have one.
func (v *pruneView) allLabel() string {
	switch v.kind {
	case docker.PruneImages:
		return "tagged unused images"
	case docker.PruneVolumes:
		return "unused named volumes"
	}
	return ""
}

func (v *pruneView) plan(checking bool) tea.Cmd {
	kind, all := v.kind, v.all
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(v.ctx, 30*time.Second)
		defer cancel()
		targets, err := v.client.PruneTargets(ctx, kind, all)
		return prunePlanMsg{v, kind, all, targets, checking, err}
	}
}

func (v *pruneView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case prunePlanMsg:
		if msg.view != v || v.ctx.Err() != nil || msg.kind != v.kind || msg.all != v.all {
			return nil
		}
		if msg.err != nil {
			v.stage, v.note = stageError, msg.err.Error()
			return nil
		}
		same := slices.EqualFunc(v.targets, msg.targets, func(a, b docker.PruneTarget) bool {
			return a.ID == b.ID && slices.Equal(a.Tags, b.Tags)
		})
		v.columns = []render.Column{{Header: "NAME", Max: 40}, {Header: "DETAILS", Max: 60}, {Header: "RESULT"}}
		if slices.ContainsFunc(msg.targets, func(t docker.PruneTarget) bool { return t.Size >= 0 }) {
			v.columns = slices.Insert(v.columns, 2, render.Column{Header: "SIZE", Align: render.Right})
		}
		v.showTargets(msg.targets)
		if !msg.checking || !same {
			v.stage = stagePreview
			if msg.checking {
				v.note = "What would be removed changed. Review this updated preview and confirm again."
			}
			return nil
		}
		v.start(func(ctx context.Context) <-chan projectResult {
			return runPrune(ctx, v.ctx, v.client, v.kind, slices.Clone(v.targets))
		})
		return v.wait()
	case pruneResultsMsg:
		if msg.view != v || v.ctx.Err() != nil {
			return nil
		}
		v.applyResults(msg.results, msg.done)
		if msg.done {
			return func() tea.Msg { return refreshMsg{} }
		}
		return v.wait()
	}
	return nil
}

func (v *pruneView) wait() tea.Cmd {
	return v.waitResults(func(batch []projectResult, done bool) tea.Msg {
		return pruneResultsMsg{view: v, results: batch, done: done}
	})
}

const pruneUnknownHint = "it may have been removed, refresh to check"

// runPrune removes targets one at a time for images, so children go before
// their parents, and four at a time otherwise. Build cache is pruned in one
// request; an empty-ID result carries a note for the view.
func runPrune(ctx, viewCtx context.Context, c dockerClient, kind docker.PruneKind, targets []docker.PruneTarget) <-chan projectResult {
	if kind != docker.PruneBuildCache {
		workers := projectWorkers
		if kind == docker.PruneImages {
			workers = 1
		}
		return runTargets(ctx, viewCtx, targets, workers, pruneTargetID,
			func(ctx context.Context, t docker.PruneTarget) error { return c.PruneOne(ctx, kind, t) },
			"removed", pruneUnknownHint, "Finished. Return to the list to see what remains, or press tab to prune something else.")
	}
	out := make(chan projectResult, 64)
	send := func(r projectResult) {
		select {
		case out <- r:
		case <-viewCtx.Done():
		}
	}
	go func() {
		defer close(out)
		for _, t := range targets {
			send(projectResult{t.ID, "running", ""})
		}
		pruneCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		deleted, space, err := c.PruneBuildCache(pruneCtx)
		cancel()
		if err != nil {
			for _, t := range targets {
				send(opFailure(t.ID, err, pruneUnknownHint))
			}
			return
		}
		extra := 0
		for _, id := range deleted {
			if !slices.ContainsFunc(targets, func(t docker.PruneTarget) bool { return t.ID == id }) {
				extra++
			}
		}
		for _, t := range targets {
			if slices.Contains(deleted, t.ID) {
				send(projectResult{t.ID, "removed", ""})
			} else {
				send(projectResult{t.ID, "kept", "The daemon kept this record; it may have become used."})
			}
		}
		note := fmt.Sprintf("Finished. Reclaimed %s.", render.Size(int64(space)))
		if extra > 0 {
			note += fmt.Sprintf(" %d records created after the preview were also removed.", extra)
		}
		send(projectResult{"", "", note})
	}()
	return out
}

func (v *pruneView) HandleKey(key string) tea.Cmd {
	idle := v.stage == stagePreview || v.stage == stageError || v.stage == stageDone
	switch key {
	case "y", "Y":
		if v.stage == stagePreview && len(v.targets) > 0 {
			v.stage, v.note = stageChecking, ""
			return v.plan(true)
		}
		return nil
	case "tab", "shift+tab":
		if !idle {
			return nil
		}
		i := slices.Index(docker.PruneKinds, v.kind)
		step := 1
		if key == "shift+tab" {
			step = len(docker.PruneKinds) - 1
		}
		v.kind, v.all = docker.PruneKinds[(i+step)%len(docker.PruneKinds)], false
		return v.reload()
	case "a":
		if idle && v.allLabel() != "" {
			v.all = !v.all
			return v.reload()
		}
		return nil
	case "ctrl+r":
		if idle {
			return v.reload()
		}
		return nil
	}
	v.navigate(key)
	return nil
}

func (v *pruneView) reload() tea.Cmd {
	v.stage, v.note = stageLoading, ""
	v.showTargets(nil)
	return v.plan(false)
}

func (v *pruneView) View() string {
	var size int64
	known := false
	for _, t := range v.targets {
		if t.Size >= 0 {
			size += t.Size
			known = true
		}
	}
	title := fmt.Sprintf("prune %s · %d to remove", v.kind, len(v.targets))
	if known {
		title += " · " + render.Size(size)
	}
	note := v.note
	switch v.stage {
	case stageLoading:
		note = "Looking for unused " + string(v.kind) + "…"
	case stageChecking:
		note = "Checking the list again before removing…"
	case stageRunning:
		note = "Removing…"
	case stageCancelling:
		note = "Cancelling remaining work; waiting for in-flight requests…"
	}
	if note == "" {
		note = map[docker.PruneKind]string{
			docker.PruneContainers: "Stopped containers (exited, created or dead). Their anonymous volumes are kept.",
			docker.PruneImages:     "Untagged images no container uses.",
			docker.PruneVolumes:    "Anonymous volumes no container mounts.",
			docker.PruneNetworks:   "User-defined networks no container uses.",
			docker.PruneBuildCache: "Build cache records not in use by a running build.",
		}[v.kind]
		if v.all {
			note = map[docker.PruneKind]string{
				docker.PruneImages:  "All images no container uses, tagged or not.",
				docker.PruneVolumes: "All volumes no container mounts, named and anonymous. Their data is deleted.",
			}[v.kind]
		}
	}
	return v.frame(title, note, func(height int) string {
		if v.stage == stageLoading || v.stage == stageError {
			return render.Dim.Render("loading…")
		}
		return v.view(height)
	})
}

func (v *pruneView) Status() string {
	counts := v.counts()
	status := fmt.Sprintf("%s · %d removed · %d failed · %d unknown · %d cancelled", v.stage, counts["removed"], counts["failed"], counts["unknown"], counts["cancelled"])
	if counts["kept"] > 0 {
		status += fmt.Sprintf(" · %d kept", counts["kept"])
	}
	return status
}

func (v *pruneView) Footer() string {
	if v.busy() {
		return hints("esc", "cancel remaining", "↑↓/pgup/pgdn", "results")
	}
	keys := []string{}
	if v.stage == stagePreview && len(v.targets) > 0 {
		keys = append(keys, "y", "remove "+fmt.Sprint(len(v.targets)))
	}
	keys = append(keys, "esc", "back", "tab", "next kind")
	if l := v.allLabel(); l != "" {
		verb := "include "
		if v.all {
			verb = "exclude "
		}
		keys = append(keys, "a", verb+l)
	}
	return hints(append(keys, "↑↓/pgup/pgdn", "move", "ctrl+r", "refresh")...)
}

// pruneKindFor picks the prune preview that matches the active tab.
func pruneKindFor(t tab) docker.PruneKind {
	switch t.(type) {
	case *ImagesView:
		return docker.PruneImages
	case *VolumesView:
		return docker.PruneVolumes
	case *NetworksView:
		return docker.PruneNetworks
	}
	return docker.PruneContainers
}

func (v *pruneView) handleKey(a *App, msg tea.KeyPressMsg) tea.Cmd {
	if cancelKey(msg.String()) {
		if dismiss(v) {
			a.prune = nil
		}
		return nil
	}
	return v.HandleKey(msg.String())
}
