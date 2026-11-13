package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"
)

func composeContainer(id, project, service, state string) container.Summary {
	return container.Summary{ID: id, Names: []string{"/" + id}, State: container.ContainerState(state), Labels: map[string]string{
		composeProjectLabel: project, composeServiceLabel: service,
	}}
}

func TestProjectsGroupLabelsAndSummarizeState(t *testing.T) {
	v := NewProjectsView()
	v.SetSize(140, 20)
	web := composeContainer("shop-web-1", "shop", "web", "running")
	web.Health = &container.HealthSummary{Status: "unhealthy"}
	v.Update(containersMsg{items: []container.Summary{
		composeContainer("shop-web-2", "shop", "web", "running"), web,
		composeContainer("shop-db-1", "shop", "db", "exited"),
		composeContainer("other-web-1", "other", "web", "running"),
		composeContainer("standalone", "", "", "running"),
	}})
	rows, _ := v.visible()
	if len(rows) != 6 || len(v.items) != 2 || strings.Contains(v.View(), "standalone") {
		t.Fatalf("unexpected project grouping: %s", ansi.Strip(v.View()))
	}
	if rows[0].key != "project:other" || rows[2].key != "project:shop" || rows[3].id != "shop-db-1" || rows[4].id != "shop-web-1" || rows[5].id != "shop-web-2" {
		t.Fatalf("projects/services are not in stable label order: %+v", rows)
	}
	if rows[2].cells[2] != "partial" || rows[2].cells[4] != "2/3" || !strings.Contains(ansi.Strip(rows[2].cells[3]), "1 unhealthy") {
		t.Fatalf("incorrect project summary: %v", rows[2].cells)
	}
	if !strings.Contains(v.Status(), "2 projects · 4 containers · 3 running") {
		t.Fatalf("incorrect totals: %s", v.Status())
	}
	if _, _, ok := v.Selected(); ok {
		t.Fatal("project header must not select a container for lifecycle actions")
	}
}

func TestProjectsFilterCollapseAndRefreshPreserveSelection(t *testing.T) {
	v := NewProjectsView()
	items := []container.Summary{
		composeContainer("a-web-1", "a", "web", "running"), composeContainer("a-db-1", "a", "db", "exited"),
		composeContainer("b-worker-1", "b", "worker", "paused"),
	}
	v.Update(containersMsg{items: items})
	v.SetFilter("db")
	rows, _ := v.visible()
	if len(rows) != 3 || rows[0].key != "project:a" {
		t.Fatalf("service filter should keep full matching project: %+v", rows)
	}
	v.HandleKey("space")
	rows, _ = v.visible()
	if len(rows) != 1 || v.selectedProject().name != "a" {
		t.Fatal("collapse did not retain project selection")
	}
	v.Update(containersMsg{items: items})
	rows, _ = v.visible()
	if len(rows) != 1 {
		t.Fatal("refresh lost collapsed state")
	}
	v.HandleKey("space")
	v.HandleKey("j")
	v.HandleKey("r")
	v.SetFilter("")
	v.Update(containersMsg{items: append(items, composeContainer("a-web-2", "a", "web", "running"))})
	if id, _, ok := v.Selected(); !ok || id != "a-db-1" {
		t.Fatalf("refresh/reverse lost selected container: %q", id)
	}
	v.SetFilter("b-worker-1")
	rows, _ = v.visible()
	if len(rows) != 2 || rows[0].cells[2] != "inactive" || rows[0].cells[4] != "0/1" {
		t.Fatal("paused containers must not count as running")
	}
	v.Update(containersMsg{err: errors.New("offline")})
	if !strings.Contains(v.View(), "offline") {
		t.Fatal("daemon errors are not shown")
	}
	v.Update(containersMsg{})
	if len(v.collapsed) != 0 || v.selectedProject() != nil {
		t.Fatal("removed projects retained stale selection")
	}
}

func TestAppProjectLogsAndContainerActions(t *testing.T) {
	a := newTestApp()
	f := &fakeClient{}
	a.ctx, a.client = context.Background(), f
	a.Update(containersMsg{items: []container.Summary{
		composeContainer("web", "shop", "web", "running"), composeContainer("db", "shop", "db", "running"),
	}})
	press(a, "4", "x", "d")
	if a.confirm != nil || a.details != nil {
		t.Fatal("project header offered single-container actions")
	}
	if a.projectAction == nil {
		t.Fatal("project header did not offer project controls")
	}
	press(a, "esc")
	if !strings.Contains(ansi.Strip(a.footer()), "project logs") {
		t.Fatal("project log action is not discoverable")
	}
	_, cmd := a.Update(key("enter"))
	if a.logs == nil || a.logs.name != "project shop" || a.logs.prefixWidth == 0 || cmd == nil {
		t.Fatal("enter did not open project logs")
	}
	press(a, "/", "E", "enter", "esc")
	if a.logs != nil || a.active != 3 {
		t.Fatal("logs did not return to Projects tab")
	}
	press(a, "j", "x")
	if a.confirm == nil || a.confirm.id != "db" {
		t.Fatal("container row did not offer its lifecycle actions")
	}
	press(a, "n", "d")
	if a.details == nil || a.details.id != "db" {
		t.Fatal("container row did not open details")
	}
	press(a, "esc")
}
