package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"
)

func actionContainer(id, project string, state container.ContainerState) container.Summary {
	return container.Summary{ID: id, Names: []string{"/" + id}, State: state, Labels: map[string]string{composeProjectLabel: project, composeServiceLabel: "web"}}
}

func TestProjectActionEligibility(t *testing.T) {
	states := []container.ContainerState{container.StateCreated, container.StateRunning, container.StatePaused, container.StateRestarting, container.StateExited, container.StateDead, container.StateRemoving}
	items := []container.Summary{actionContainer("other", "other", container.StateRunning), actionContainer("standalone", "", container.StateRunning)}
	for i, state := range states {
		items = append(items, actionContainer(fmt.Sprint(i), "demo", state))
	}
	for _, tt := range []struct {
		verb string
		ids  []string
	}{{"start", []string{"0", "2", "4"}}, {"stop", []string{"1", "2", "3"}}, {"restart", []string{"1"}}} {
		targets, skipped := projectTargets(items, "demo", tt.verb)
		ids := []string{}
		for _, target := range targets {
			ids = append(ids, target.id)
		}
		if !slices.Equal(ids, tt.ids) || skipped != len(states)-len(tt.ids) {
			t.Fatalf("%s: targets=%v skipped=%d", tt.verb, targets, skipped)
		}
	}
}

func finishProjectAction(t *testing.T, v *projectActionView, cmd func() any) {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if _, ok := msg.(refreshMsg); ok {
			return
		}
		next := v.Update(msg)
		if next == nil {
			cmd = nil
		} else {
			cmd = func() any { return next() }
		}
	}
}

func TestProjectPreviewRechecksTargetsAndRequiresNewConfirmation(t *testing.T) {
	for _, change := range []string{"id", "state", "service", "name"} {
		t.Run(change, func(t *testing.T) {
			c := &fakeClient{containers: []container.Summary{actionContainer("a", "demo", container.StateRunning)}}
			v, load := newProjectAction(t.Context(), c, "demo", "stop", 120, 20)
			defer v.Close()
			v.Update(load())
			if v.stage != "preview" || len(c.actions) != 0 {
				t.Fatal("preview performed writes")
			}
			switch change {
			case "id":
				c.containers[0].ID = "replacement"
			case "state":
				c.containers[0].State = container.StatePaused
			case "service":
				c.containers[0].Labels[composeServiceLabel] = "db"
			case "name":
				c.containers[0].Names = []string{"/renamed"}
			}
			check := v.HandleKey("y")
			if check == nil || v.HandleKey("y") != nil {
				t.Fatal("duplicate confirmation")
			}
			if v.Update(check()) != nil || v.stage != "preview" || len(c.actions) != 0 || !strings.Contains(v.note, "changed") {
				t.Fatal("stale plan performed actions")
			}
			next := v.Update(v.HandleKey("y")())
			finishProjectAction(t, v, func() any { return next() })
			if v.stage != "done" || len(c.actions) != 1 || c.actions[0] != "stop "+c.containers[0].ID {
				t.Fatal(v.stage, c.actions)
			}
		})
	}
}

type boundedProjectClient struct {
	*fakeClient
	entered         chan string
	release         chan struct{}
	active, maximum atomic.Int32
	fail            string
}

func (c *boundedProjectClient) Stop(ctx context.Context, id string) error {
	active := c.active.Add(1)
	defer c.active.Add(-1)
	for old := c.maximum.Load(); active > old; old = c.maximum.Load() {
		if c.maximum.CompareAndSwap(old, active) {
			break
		}
	}
	c.entered <- id
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.release:
	}
	if id == c.fail {
		return errors.New("one container failed")
	}
	return nil
}
func waitProjectEntry(t *testing.T, c *boundedProjectClient) string {
	t.Helper()
	select {
	case id := <-c.entered:
		return id
	case <-time.After(3 * time.Second):
		t.Fatal("workers stalled")
		return ""
	}
}
func TestProjectActionBoundedConcurrencyAndPartialFailure(t *testing.T) {
	c := &boundedProjectClient{fakeClient: &fakeClient{}, entered: make(chan string, 20), release: make(chan struct{}), fail: "5"}
	targets := []projectTarget{}
	for i := range 20 {
		targets = append(targets, projectTarget{id: fmt.Sprint(i)})
	}
	results := runProjectActions(t.Context(), t.Context(), c, "stop", targets)
	for range projectWorkers {
		waitProjectEntry(t, c)
	}
	if c.active.Load() != projectWorkers {
		t.Fatal("workers not parallel", c.active.Load())
	}
	close(c.release)
	completed, failed := 0, 0
	for r := range results {
		if r.status == "succeeded" {
			completed++
		}
		if r.status == "failed" {
			failed++
		}
	}
	if completed != 19 || failed != 1 || c.maximum.Load() > projectWorkers {
		t.Fatal(completed, failed, c.maximum.Load())
	}
}
func TestProjectActionCancellationSkipsPendingAndReportsUncertainRequests(t *testing.T) {
	c := &boundedProjectClient{fakeClient: &fakeClient{}, entered: make(chan string, 20), release: make(chan struct{})}
	targets := make([]projectTarget, 20)
	for i := range targets {
		targets[i].id = fmt.Sprint(i)
	}
	ctx, cancel := context.WithCancel(t.Context())
	results := runProjectActions(ctx, t.Context(), c, "stop", targets)
	for range projectWorkers {
		waitProjectEntry(t, c)
	}
	cancel()
	unknown, cancelled := 0, 0
	for r := range results {
		switch r.status {
		case "unknown":
			unknown++
		case "cancelled":
			cancelled++
		}
	}
	if unknown != projectWorkers || cancelled != len(targets)-projectWorkers || len(c.entered) != 0 {
		t.Fatal(unknown, cancelled, len(c.entered))
	}
}

func TestAppProjectActionsPreserveSelectionAndIndividualActions(t *testing.T) {
	a := newTestApp()
	c := &fakeClient{containers: []container.Summary{actionContainer("a", "demo", container.StateRunning)}}
	a.client = c
	a.active = 3
	v := a.tabs[3].(*ProjectsView)
	v.Update(containersMsg{items: c.containers})
	a.setFilter("demo")
	cmd := a.handleKey(key("x"))
	if a.projectAction == nil || a.confirm != nil {
		t.Fatal("project header did not open preview")
	}
	pane := a.projectAction
	pane.Update(cmd())
	if !strings.Contains(ansi.Strip(a.render()), "1 eligible") || !strings.Contains(a.footer(), "confirm stop") {
		t.Fatal("missing preview")
	}
	press(a, "esc")
	if a.projectAction != nil || a.tabs[3].Filter() != "demo" || v.selectedProject() == nil || len(c.actions) != 0 {
		t.Fatal("cancel changed underlying view")
	}
	v.HandleKey("j")
	a.handleKey(key("R"))
	if a.confirm == nil || a.confirm.id != "a" || a.projectAction != nil {
		t.Fatal("child row lost single-container action")
	}
	a.confirm = nil
	v.HandleKey("k")
	cmd = a.handleKey(key("x"))
	pane = a.projectAction
	pane.Update(cmd())
	check := pane.HandleKey("y")
	press(a, "esc")
	if pane.Update(check()) != nil || len(c.actions) != 0 {
		t.Fatal("late check on closed pane started work")
	}
}

func TestProjectActionResultsSizeAndErrors(t *testing.T) {
	v, _ := newProjectAction(t.Context(), &fakeClient{}, "demo", "stop", 50, 6)
	defer v.Close()
	targets := []projectTarget{}
	for i := range 30 {
		targets = append(targets, projectTarget{id: fmt.Sprint(i), name: fmt.Sprintf("container-%d", i)})
	}
	v.targets = targets
	v.showTargets(targets)
	v.stage = "preview"
	for _, size := range [][2]int{{50, 6}, {20, 2}, {10, 1}} {
		v.SetSize(size[0], size[1])
		text := ansi.Strip(v.View())
		if len(strings.Split(text, "\n")) > size[1] {
			t.Fatal("overflow height", text)
		}
		for line := range strings.SplitSeq(text, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("overflow width", line)
			}
		}
	}
	v.SetSize(120, 10)
	v.stage = "running"
	v.operationCancel = func() {}
	v.Update(projectResultsMsg{view: v, results: []projectResult{{id: "0", status: "failed", detail: "first line\nsecond line"}}, done: true})
	if !strings.Contains(v.View(), "first line second line") || !strings.Contains(v.Status(), "1 failed") {
		t.Fatal(v.View(), v.Status())
	}
}
