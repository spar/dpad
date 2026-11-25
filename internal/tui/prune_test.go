package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"dpad/internal/docker"
)

func finishPrune(t *testing.T, v *pruneView, cmd tea.Cmd) {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if _, ok := msg.(refreshMsg); ok {
			return
		}
		cmd = v.Update(msg)
	}
}

func TestPruneOpensForTheActiveTabAndRemovesOnlyAfterConfirmation(t *testing.T) {
	f := &fakeClient{pruneTargets: map[docker.PruneKind][]docker.PruneTarget{
		docker.PruneImages: {{ID: "img1", Name: "<none>", Size: 2048}, {ID: "img2", Name: "<none>", Size: 1024}},
	}, pruneOneErr: map[string]error{"img2": errors.New("image has dependent child images")}}
	a := newTestApp()
	a.client = f
	press(a, "2")
	_, cmd := a.Update(key("X"))
	if a.prune == nil || a.prune.kind != docker.PruneImages {
		t.Fatal("X on Images should open an image prune preview")
	}
	a.Update(cmd())
	if a.prune.stage != "preview" || len(a.prune.targets) != 2 {
		t.Fatalf("stage %s targets %v", a.prune.stage, a.prune.targets)
	}
	if view := ansi.Strip(a.render()); !strings.Contains(view, "prune images · 2 to remove · 3.07kB") || !strings.Contains(view, "y remove 2") {
		t.Fatalf("preview missing count, size or confirm key:\n%s", view)
	}
	if slices.ContainsFunc(f.actions, func(s string) bool { return strings.HasPrefix(s, "prune images") }) {
		t.Fatal("preview removed something")
	}
	_, check := a.Update(key("y"))
	finishPrune(t, a.prune, a.prune.Update(check()))
	want := []string{"prune targets images all=false", "prune targets images all=false", "prune images img1", "prune images img2"}
	if !slices.Equal(f.actions, want) {
		t.Fatalf("actions = %v", f.actions)
	}
	if a.prune.stage != "done" || a.prune.items[0].status != "removed" || a.prune.items[1].status != "failed" {
		t.Fatalf("stage %s rows %+v", a.prune.stage, a.prune.items)
	}
	press(a, "esc")
	if a.prune != nil {
		t.Fatal("esc should close the prune view")
	}
}

func TestPruneAsksAgainWhenTargetsChange(t *testing.T) {
	f := &fakeClient{pruneTargets: map[docker.PruneKind][]docker.PruneTarget{docker.PruneContainers: {{ID: "a", Size: -1}}}}
	v, load := newPruneView(t.Context(), f, docker.PruneContainers, 120, 20)
	defer v.Close()
	v.Update(load())
	if strings.Contains(v.View(), "SIZE") {
		t.Fatal("containers have no size to show")
	}
	f.pruneTargets[docker.PruneContainers] = []docker.PruneTarget{{ID: "a", Size: -1}, {ID: "b", Size: -1}}
	if v.Update(v.HandleKey("y")()) != nil || v.stage != "preview" || !strings.Contains(v.note, "changed") {
		t.Fatalf("changed targets should return to preview, stage %s", v.stage)
	}
	finishPrune(t, v, v.Update(v.HandleKey("y")()))
	// Containers are removed by several workers, so either may finish first.
	got := slices.Sorted(slices.Values(f.actions[len(f.actions)-2:]))
	if !slices.Equal(got, []string{"prune containers a", "prune containers b"}) {
		t.Fatal(f.actions)
	}
}

func TestPruneTabCyclesKindsAndATogglesAll(t *testing.T) {
	f := &fakeClient{}
	v, load := newPruneView(t.Context(), f, docker.PruneImages, 120, 20)
	defer v.Close()
	v.Update(load())
	v.Update(v.HandleKey("a")())
	if !v.all || !strings.Contains(ansi.Strip(v.Footer()), "exclude tagged unused images") {
		t.Fatal("a should include tagged images")
	}
	v.Update(v.HandleKey("tab")())
	if v.kind != docker.PruneVolumes || v.all {
		t.Fatalf("tab should move to volumes and reset all, got %s all=%t", v.kind, v.all)
	}
	v.Update(v.HandleKey("tab")())
	if v.HandleKey("a") != nil || strings.Contains(ansi.Strip(v.Footer()), "include") {
		t.Fatal("networks have no all toggle")
	}
	want := []string{"prune targets images all=false", "prune targets images all=true", "prune targets volumes all=false", "prune targets networks all=false"}
	if !slices.Equal(f.actions, want) {
		t.Fatal(f.actions)
	}
}

func TestPruneBuildCacheMarksWhatTheDaemonRemoved(t *testing.T) {
	f := &fakeClient{
		pruneTargets:      map[docker.PruneKind][]docker.PruneTarget{docker.PruneBuildCache: {{ID: "r1", Size: 10}, {ID: "r2", Size: 5}}},
		buildCacheDeleted: []string{"r1", "later"},
	}
	v, load := newPruneView(t.Context(), f, docker.PruneBuildCache, 120, 20)
	defer v.Close()
	v.Update(load())
	finishPrune(t, v, v.Update(v.HandleKey("y")()))
	if v.items[0].status != "removed" || v.items[1].status != "kept" {
		t.Fatalf("rows %+v", v.items)
	}
	if !strings.Contains(v.note, "Reclaimed 1.02kB") || !strings.Contains(v.note, "1 records created after the preview") {
		t.Fatalf("note %q", v.note)
	}
}

func TestPruneIsNotOfferedOnEvents(t *testing.T) {
	a := newTestApp()
	a.client = &fakeClient{}
	press(a, "5")
	a.Update(key("X"))
	if a.prune != nil {
		t.Fatal("X opened prune on Events")
	}
}
