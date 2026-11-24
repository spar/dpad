package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"dpad/internal/docker"
	"dpad/internal/render"
	"dpad/internal/sortx"
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
type pruneRow struct {
	target         docker.PruneTarget
	status, detail string
}

// pruneView previews what a prune removes, rechecks it on confirmation and
// removes exactly those resources, reporting each result.
type pruneView struct {
	list[pruneRow]
	ctx             context.Context
	cancel          context.CancelFunc
	operationCancel context.CancelFunc
	client          dockerClient
	kind            docker.PruneKind
	all             bool
	stage, note     string
	targets         []docker.PruneTarget
	results         <-chan projectResult
}

func newPruneView(ctx context.Context, c dockerClient, kind docker.PruneKind, width, height int) (*pruneView, tea.Cmd) {
	ctx, cancel := context.WithCancel(ctx)
	v := &pruneView{ctx: ctx, cancel: cancel, client: c, kind: kind, stage: "loading"}
	v.list = list[pruneRow]{
		sort: func([]pruneRow, sortx.Key, bool) {},
		rows: func(r pruneRow) []row {
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
		},
		empty: "nothing to prune", loaded: true,
	}
	v.SetSize(width, height)
	return v, v.plan(false)
}

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
			v.stage, v.note = "error", msg.err.Error()
			return nil
		}
		same := slices.EqualFunc(v.targets, msg.targets, func(a, b docker.PruneTarget) bool {
			return a.ID == b.ID && slices.Equal(a.Tags, b.Tags)
		})
		v.targets = msg.targets
		v.columns = []render.Column{{Header: "NAME", Max: 40}, {Header: "DETAILS", Max: 60}, {Header: "RESULT"}}
		if slices.ContainsFunc(msg.targets, func(t docker.PruneTarget) bool { return t.Size >= 0 }) {
			v.columns = slices.Insert(v.columns, 2, render.Column{Header: "SIZE", Align: render.Right})
		}
		rows := make([]pruneRow, len(msg.targets))
		for i, t := range msg.targets {
			rows[i] = pruneRow{target: t, status: "pending"}
		}
		v.setItems(rows, nil)
		if !msg.checking || !same {
			v.stage = "preview"
			if msg.checking {
				v.note = "What would be removed changed. Review this updated preview and confirm again."
			}
			return nil
		}
		v.stage = "running"
		ctx, cancel := context.WithCancel(v.ctx)
		v.operationCancel = cancel
		v.results = runPrune(ctx, v.ctx, v.client, v.kind, slices.Clone(v.targets))
		return v.waitResults()
	case pruneResultsMsg:
		if msg.view != v || v.ctx.Err() != nil {
			return nil
		}
		for _, result := range msg.results {
			if result.id == "" {
				v.note = result.detail
				continue
			}
			for i := range v.items {
				if v.items[i].target.ID == result.id {
					v.items[i].status, v.items[i].detail = result.status, result.detail
					break
				}
			}
		}
		v.rowCache = nil
		if msg.done {
			v.stage = "done"
			v.operationCancel()
			return func() tea.Msg { return refreshMsg{} }
		}
		return v.waitResults()
	}
	return nil
}

func (v *pruneView) waitResults() tea.Cmd {
	return waitActionBatch(v.ctx, v.results, func(batch []projectResult, done bool) tea.Msg {
		return pruneResultsMsg{view: v, results: batch, done: done}
	})
}

// runPrune removes targets one at a time for images, so children go before
// their parents, and four at a time otherwise. Build cache is pruned in one
// request; an empty-ID result carries a note for the view.
func runPrune(ctx, viewCtx context.Context, c dockerClient, kind docker.PruneKind, targets []docker.PruneTarget) <-chan projectResult {
	out := make(chan projectResult, 64)
	send := func(r projectResult) {
		select {
		case out <- r:
		case <-viewCtx.Done():
		}
	}
	failure := func(id string, err error) projectResult {
		result := projectResult{id: id, status: "failed", detail: err.Error()}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			result.status = "unknown"
			result.detail += "; it may have been removed, refresh to check"
		}
		return result
	}
	if kind == docker.PruneBuildCache {
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
					send(failure(t.ID, err))
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
	workers := projectWorkers
	if kind == docker.PruneImages {
		workers = 1
	}
	go func() {
		defer close(out)
		jobs := make(chan docker.PruneTarget)
		var wg sync.WaitGroup
		for range min(workers, len(targets)) {
			wg.Go(func() {
				for t := range jobs {
					if ctx.Err() != nil {
						send(projectResult{t.ID, "cancelled", "Not started"})
						continue
					}
					send(projectResult{t.ID, "running", ""})
					removeCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
					err := c.PruneOne(removeCtx, kind, t)
					cancel()
					if err != nil {
						send(failure(t.ID, err))
					} else {
						send(projectResult{t.ID, "removed", ""})
					}
				}
			})
		}
		for _, t := range targets {
			select {
			case jobs <- t:
			case <-viewCtx.Done():
				close(jobs)
				wg.Wait()
				return
			}
		}
		close(jobs)
		wg.Wait()
		send(projectResult{"", "", "Finished. Return to the list to see what remains, or press tab to prune something else."})
	}()
	return out
}

func (v *pruneView) HandleKey(key string) tea.Cmd {
	idle := v.stage == "preview" || v.stage == "error" || v.stage == "done"
	switch key {
	case "y", "Y":
		if v.stage == "preview" && len(v.targets) > 0 {
			v.stage, v.note = "checking", ""
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
	_, keys := v.visible()
	v.cur.navKey(keys, key, v.page)
	return nil
}

func (v *pruneView) reload() tea.Cmd {
	v.stage, v.note = "loading", ""
	v.targets = nil
	v.setItems(nil, nil)
	return v.plan(false)
}

func (v *pruneView) busy() bool { return v.stage == "running" || v.stage == "cancelling" }

func (v *pruneView) SetSize(width, height int) {
	v.list.SetSize(width, height)
	v.page = max(height-4, 1)
}

func (v *pruneView) Close() {
	v.cancel()
	if v.operationCancel != nil {
		v.operationCancel()
	}
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
	case "loading":
		note = "Looking for unused " + string(v.kind) + "…"
	case "checking":
		note = "Checking the list again before removing…"
	case "running":
		note = "Removing…"
	case "cancelling":
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
	_, keys := v.visible()
	if len(keys) > 0 {
		selected := keys[v.cur.index(keys)]
		for _, r := range v.items {
			if r.target.ID == selected && r.detail != "" {
				note = r.detail
				break
			}
		}
	}
	note = strings.NewReplacer("\r", " ", "\n", " ").Replace(note)
	lines := []string{ansi.Truncate(render.Bold.Render(title), v.width, ""), ansi.Truncate(render.Dim.Render(note), v.width, "")}
	if v.height > 2 {
		body := render.Dim.Render("loading…")
		if v.stage != "loading" && v.stage != "error" {
			body = v.view(max(v.height-2, 1))
		}
		lines = append(lines, strings.Split(body, "\n")...)
	}
	return strings.Join(lines[:min(len(lines), max(v.height, 1))], "\n")
}

func (v *pruneView) Status() string {
	counts := map[string]int{}
	for _, r := range v.items {
		counts[r.status]++
	}
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
	if v.stage == "preview" && len(v.targets) > 0 {
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
