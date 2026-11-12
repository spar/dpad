package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/moby/moby/api/types/container"

	"dockernext/internal/docker"
)

func TestDetailsLoadRefreshAndStaleResults(t *testing.T) {
	f := &fakeClient{inspect: container.InspectResponse{Name: "/web", ID: "web-id"}}
	v, load := NewDetailsView(context.Background(), f, "web-id", "web", 80, 8)
	defer v.Close()
	if !strings.Contains(v.View(), "loading") {
		t.Fatal("expected loading state before inspect completes")
	}
	v.Update(load())
	if !strings.Contains(v.View(), "web-id") || v.loading {
		t.Fatalf("inspect result not shown: %s", v.View())
	}
	if len(f.actions) != 1 || f.actions[0] != "inspect web-id" {
		t.Fatalf("inspected wrong container: %v", f.actions)
	}
	v.Update(key("G"))
	if v.vp.YOffset() == 0 {
		t.Fatal("details did not scroll")
	}
	v.SetSize(40, 4)
	if v.vp.Width() != 40 || v.vp.Height() != 4 {
		t.Fatal("details did not resize")
	}
	refresh := v.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if refresh == nil || !v.loading {
		t.Fatal("ctrl+r did not reload inspect data")
	}
	v.Update(detailsMsg{v: v, gen: v.gen - 1, err: errors.New("stale failure")})
	v.Update(detailsMsg{v: &DetailsView{}, gen: v.gen, err: errors.New("other pane")})
	if !v.loading || v.err != nil {
		t.Fatal("stale inspect result changed the current view")
	}
	f.inspectErr = errors.New("container no longer exists")
	v.Update(refresh())
	if !strings.Contains(v.Status(), "inspect failed") {
		t.Fatalf("missing error status: %s", v.Status())
	}
	v.Update(key("g"))
	if !strings.Contains(v.View(), "container no longer exists") {
		t.Fatalf("missing inspect error: %s", v.View())
	}
	f.inspectErr = nil
	v.Update(v.load()())
	if v.err != nil || !strings.Contains(v.View(), "web-id") {
		t.Fatalf("retry did not recover: %s", v.View())
	}
	v.Close()
	if !errors.Is(v.ctx.Err(), context.Canceled) {
		t.Fatal("closing details did not cancel inspect context")
	}
}

func TestAppDetailsPreservesListAndLogsReturn(t *testing.T) {
	f := &fakeClient{containers: []container.Summary{
		{ID: "db", Names: []string{"/common-db"}}, {ID: "web", Names: []string{"/common-web"}},
	}, inspect: container.InspectResponse{ID: "web", Name: "/web"}}
	a := newTestApp()
	a.ctx, a.client = context.Background(), f
	a.Update(containersMsg{items: f.containers})
	press(a, "/", "c", "o", "m", "m", "o", "n", "enter", "j")
	_, load := a.Update(key("d"))
	if a.details == nil || a.details.id != "web" || load == nil {
		t.Fatal("d did not open selected container's details")
	}
	a.Update(load())
	if !strings.Contains(a.render(), "details:") || !strings.Contains(a.footer(), "refresh") {
		t.Fatal("app did not render details and its controls")
	}
	press(a, "enter")
	if a.logs == nil {
		t.Fatal("enter did not open logs from details")
	}
	press(a, "esc")
	if a.logs != nil || a.details == nil {
		t.Fatal("escaping logs did not return to details")
	}
	ctx := a.details.ctx
	press(a, "esc")
	if a.details != nil || ctx.Err() == nil || a.tabs[0].Filter() != "common" {
		t.Fatal("closing details did not preserve filter or cancel its context")
	}
	if id, _, _ := a.tabs[0].Selected(); id != "web" {
		t.Fatalf("selection changed: %s", id)
	}
	press(a, "d", "q")
	if a.details != nil {
		t.Fatal("q did not close details")
	}
}

func TestDetailsOnlyForContainersAndStatsSortUnchanged(t *testing.T) {
	a := newTestApp()
	press(a, "d")
	if a.details != nil {
		t.Fatal("empty container list opened details")
	}
	press(a, "2", "d")
	if a.details != nil || strings.Contains(a.footer(), "details") {
		t.Fatal("images offered container details")
	}
	press(a, "3", "i")
	if a.tabs[2].SortKey() != "io" {
		t.Fatal("details shortcut broke Stats disk I/O sorting")
	}
	a.ctx, a.client = context.Background(), &fakeClient{}
	a.Update(statsBatchMsg{{Stats: docker.Stats{ID: "web", Name: "web"}}})
	_, load := a.Update(key("d"))
	if a.details == nil || a.details.id != "web" || load == nil {
		t.Fatal("d did not open details from Stats")
	}
	a.details.Close()
}
