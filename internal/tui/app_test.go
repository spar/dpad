package tui

import (
	"context"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"

	"dpad/internal/sortx"
)

func newTestApp() *App {
	a := &App{
		ctx:    context.Background(),
		tabs:   []tab{NewContainersView(context.Background(), nil), NewImagesView(context.Background(), nil), NewStatsView(context.Background(), nil, sortx.CPU, false), NewProjectsView(), NewEventsView(context.Background(), nil), NewVolumesView(context.Background(), nil), NewNetworksView(context.Background(), nil)},
		filter: textinput.New(),
	}
	a.tabs[2].(*StatsView).SetGrouped(true)
	a.resize(120, 30)
	return a
}

// key builds the key press message for a key name or a single character.
func key(k string) tea.KeyPressMsg {
	switch k {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

func press(a *App, keys ...string) {
	for _, k := range keys {
		a.Update(key(k))
	}
}

func TestAppSwitchTabs(t *testing.T) {
	a := newTestApp()
	for _, tt := range []struct {
		key  string
		want int
	}{{"3", 2}, {"1", 0}, {"tab", 1}, {"4", 3}, {"0", 3}, {"tab", 4}, {"tab", 5}, {"tab", 6}, {"tab", 0}} {
		press(a, tt.key)
		if a.active != tt.want {
			t.Errorf("after %q active = %d, want %d", tt.key, a.active, tt.want)
		}
	}
}

func TestAppFilterIsPerTab(t *testing.T) {
	a := newTestApp()
	press(a, "/", "w", "e", "enter")
	if got := a.tabs[0].Filter(); got != "we" {
		t.Fatalf("containers filter = %q, want we", got)
	}
	press(a, "2")
	if a.filter.Value() != "" || a.tabs[1].Filter() != "" {
		t.Errorf("images tab should start unfiltered, input shows %q", a.filter.Value())
	}
	press(a, "1")
	if a.filter.Value() != "we" {
		t.Errorf("switching back should restore the filter, input shows %q", a.filter.Value())
	}
	press(a, "esc")
	if a.tabs[0].Filter() != "" {
		t.Errorf("esc should clear the filter, got %q", a.tabs[0].Filter())
	}
}

func TestAppLogSearchConsumesKeysAndPreservesTabFilter(t *testing.T) {
	a := newTestApp()
	press(a, "/", "w", "e", "b", "enter")
	v := newTestLogsView(10)
	v.cancel = func() {}
	a.logs = v
	addLogLines(v, "query", "other")
	press(a, "/", "q")
	if a.logs != v || !v.searching || v.query != "q" || a.tabs[0].Filter() != "web" {
		t.Fatal("log search keys escaped to app or changed tab filter")
	}
	if !strings.Contains(ansi.Strip(a.footer()), "esc cancel") {
		t.Fatal("search prompt is missing editing hints")
	}
	press(a, "esc")
	if a.logs != v || v.searching || v.query != "" {
		t.Fatal("escape should cancel search without leaving logs")
	}
	press(a, "esc")
	if a.logs != nil || a.tabs[0].Filter() != "web" {
		t.Fatal("escape should return to the filtered list")
	}
}

func TestFooterHidesContainerActionsOnImages(t *testing.T) {
	a := newTestApp()
	a.Update(containersMsg{items: []container.Summary{{ID: "1", Names: []string{"/web"}}}})
	if f := ansi.Strip(a.footer()); !strings.Contains(f, "logs") {
		t.Errorf("containers footer should offer logs: %q", f)
	}
	press(a, "2")
	if f := ansi.Strip(a.footer()); strings.Contains(f, "logs") || strings.Contains(f, "S/x/R") {
		t.Errorf("images footer offers container actions: %q", f)
	}
}

func TestAppStopAsksFirst(t *testing.T) {
	f := &fakeClient{containers: []container.Summary{
		{ID: "id-db", Names: []string{"/db"}, State: "running"},
		{ID: "id-web", Names: []string{"/web"}, State: "running"},
	}}
	a := newTestApp()
	a.ctx, a.client = context.Background(), f
	a.Update(containersMsg{items: f.containers})

	press(a, "j", "x")
	if a.confirm == nil || a.confirm.name != "web" {
		t.Fatalf("x should ask to stop web, confirm = %+v", a.confirm)
	}
	press(a, "n")
	if a.confirm != nil || len(f.actions) != 0 {
		t.Fatalf("n should cancel, actions = %v", f.actions)
	}

	a.Update(key("x"))
	_, cmd := a.Update(key("y"))
	if msg, ok := cmd().(actionDoneMsg); !ok || msg.err != nil {
		t.Fatalf("y should run the stop, got %#v", msg)
	}
	if want := []string{"stop id-web"}; !slices.Equal(f.actions, want) {
		t.Errorf("actions = %v, want %v", f.actions, want)
	}
}

func TestAppPauseKillRemoveAskFirst(t *testing.T) {
	f := &fakeClient{containers: []container.Summary{{ID: "id-web", Names: []string{"/web"}, State: "running"}}}
	a := newTestApp()
	a.ctx, a.client = context.Background(), f
	a.Update(containersMsg{items: f.containers})
	for _, k := range []string{"P", "K", "D"} {
		a.Update(key(k))
		if a.confirm == nil {
			t.Fatalf("%s ran without asking", k)
		}
		_, cmd := a.Update(key("y"))
		if msg, ok := cmd().(actionDoneMsg); !ok || msg.err != nil {
			t.Fatalf("%s: y should run the action, got %#v", k, msg)
		}
	}
	if want := []string{"pause id-web", "kill id-web", "remove id-web"}; !slices.Equal(f.actions, want) {
		t.Errorf("actions = %v, want %v", f.actions, want)
	}
	a.Update(key("D"))
	if f := ansi.Strip(a.footer()); !strings.Contains(f, "killed first if running") {
		t.Errorf("remove prompt doesn't warn about running containers: %q", f)
	}
}

func TestTabRuleMarksTheActiveTab(t *testing.T) {
	a := newTestApp()
	a.switchTab(2)
	screen := strings.Split(ansi.Strip(a.render()), "\n")
	if len(screen) != 30 {
		t.Fatalf("screen has %d lines, want 30", len(screen))
	}
	bar, rule := []rune(screen[0]), []rune(screen[1])
	if len(rule) != 120 {
		t.Fatalf("rule is %d wide", len(rule))
	}
	start := len([]rune(screen[0][:strings.Index(screen[0], " 3 Stats ")]))
	end := start + len(" 3 Stats ")
	for i, r := range rule {
		heavy := i >= start && i < end
		if (r == '━') != heavy {
			t.Fatalf("rule %q doesn't underline only %q", string(rule), string(bar[start:end]))
		}
	}
}
