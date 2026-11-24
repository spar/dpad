package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"dpad/internal/docker"
)

func testEvent(i int) docker.EventUpdate {
	return docker.EventUpdate{Event: docker.ContainerEvent{At: time.Unix(int64(i), 0), ID: fmt.Sprintf("container-%d", i), Name: fmt.Sprintf("web-%d", i), Project: "shop", Service: "api", Image: "alpine", Action: "start"}}
}
func newTestEventsView(t *testing.T) *EventsView {
	t.Helper()
	v := NewEventsView(t.Context(), nil)
	v.SetSize(180, 20)
	v.Update(eventsStartedMsg{ch: make(chan docker.EventUpdate)})
	return v
}
func eventBatch(v *EventsView, updates ...docker.EventUpdate) eventsBatchMsg {
	return eventsBatchMsg{ch: v.ch, updates: updates}
}

func TestEventsRingEvictionAndSelection(t *testing.T) {
	v := newTestEventsView(t)
	batch := make([]docker.EventUpdate, eventCapacity)
	for i := range batch {
		batch[i] = testEvent(i)
	}
	v.Update(eventBatch(v, batch...))
	if id, _, ok := v.Selected(); !ok || id != "container-999" {
		t.Fatal("newest event not selected", id)
	}
	v.HandleKey("down")
	id, _, _ := v.Selected()
	for i := eventCapacity; i < eventCapacity+100; i++ {
		v.Update(eventBatch(v, testEvent(i)))
	}
	if got, _, _ := v.Selected(); got != id {
		t.Fatal("arrivals moved historical selection", got, id)
	}
	if v.count != eventCapacity || len(v.items) != eventCapacity || v.items[0].event.ID != "container-1099" || v.items[eventCapacity-1].event.ID != "container-100" {
		t.Fatal("incorrect bounded retention")
	}
	v.HandleKey("r")
	if got, _, _ := v.Selected(); got != id {
		t.Fatal("reverse changed selection")
	}
	v.HandleKey("g")
	if got, _, _ := v.Selected(); got != "container-1099" || v.reverse {
		t.Fatal("g did not return to newest")
	}
	v.Update(eventBatch(v, testEvent(1100)))
	if got, _, _ := v.Selected(); got != "container-1100" {
		t.Fatal("top did not follow arrivals")
	}
	// Once a selection ages out, the first visible row becomes selected.
	v.HandleKey("G")
	for i := 1101; i < 2101; i++ {
		batch[(i-1101)%eventCapacity] = testEvent(i)
	}
	v.Update(eventBatch(v, batch...))
	if got, _, _ := v.Selected(); got != "container-2100" {
		t.Fatal("evicted cursor did not fall back", got)
	}
}

func TestEventsFilterAndRecovery(t *testing.T) {
	v := newTestEventsView(t)
	first := testEvent(1)
	first.Event.Action = "health_status: unhealthy"
	first.Event.ExitCode = "137"
	v.Update(eventBatch(v, first, testEvent(2)))
	for _, filter := range []string{"SHOP", "api", "web-1", "container-1", "unhealthy", "alpine", "137"} {
		v.SetFilter(filter)
		rows, _ := v.visible()
		if len(rows) == 0 {
			t.Fatal("no match for", filter)
		}
	}
	v.SetFilter("unhealthy")
	v.cur.key = "1"
	v.Update(eventBatch(v, docker.EventUpdate{Err: errors.New("offline")}))
	v.Update(eventBatch(v, docker.EventUpdate{Subscribed: true}, docker.EventUpdate{Err: errors.New("offline again")}))
	if v.count != 3 {
		t.Fatal("repeated retry flooded gap markers", v.count)
	}
	v.Update(eventBatch(v, testEvent(3)))
	if v.streamErr != nil || v.filter != "unhealthy" {
		t.Fatal("recovery lost filter or error persisted")
	}
	if got, _, _ := v.Selected(); got != "container-1" {
		t.Fatal("recovery moved selection")
	}
	v.SetFilter("stream gap")
	if _, _, ok := v.Selected(); ok {
		t.Fatal("gap offered a container action")
	}
	if !strings.Contains(ansi.Strip(v.View()), "events may be missing") {
		t.Fatal(v.View())
	}
	before := v.count
	v.Update(eventsBatchMsg{ch: make(chan docker.EventUpdate), updates: []docker.EventUpdate{testEvent(4)}})
	if v.count != before {
		t.Fatal("foreign channel updated timeline")
	}
}

func TestEventBatchBoundAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch := make(chan docker.EventUpdate, maxEventBatch+2)
	for i := range cap(ch) {
		ch <- testEvent(i)
	}
	start := time.Now()
	msg := waitEvents(ctx, ch)().(eventsBatchMsg)
	if len(msg.updates) != maxEventBatch || len(ch) != 2 || time.Since(start) < eventBatchInterval {
		t.Fatal("unbounded or unthrottled event batch")
	}
	cancel()
	if waitEvents(ctx, make(chan docker.EventUpdate))() != nil {
		t.Fatal("read ignored cancellation")
	}
}

func TestEventsInitSharesOneWatcher(t *testing.T) {
	v := NewEventsView(t.Context(), &fakeClient{})
	if v.Init() == nil || v.Init() != nil || v.Focus() != nil {
		t.Fatal("watcher started more than once")
	}
}

func TestEventsOpenDetailsAndReturn(t *testing.T) {
	a := newTestApp()
	f := &fakeClient{}
	a.client = f
	press(a, "5")
	v := a.tabs[4].(*EventsView)
	v.Update(eventBatch(v, testEvent(1), testEvent(2)))
	v.SetFilter("web-1")
	a.filter.SetValue(v.Filter())
	for _, k := range []string{"e", "S", "x", "R"} {
		if a.handleKey(key(k)) != nil || a.confirm != nil || a.shell != nil {
			t.Fatal("historical event offered lifecycle action", k)
		}
	}
	for _, k := range []string{"enter", "d"} {
		cmd := a.handleKey(key(k))
		if cmd == nil || a.details == nil || a.details.id != "container-1" {
			t.Fatal("event did not open its container details")
		}
		a.Update(cmd())
		press(a, "esc")
		if a.details != nil || a.active != 4 || v.Filter() != "web-1" {
			t.Fatal("details return lost Events state")
		}
	}
	if strings.Contains(ansi.Strip(a.footer()), "S/x/R") {
		t.Fatal("Events advertises lifecycle actions")
	}
}

func BenchmarkEventsUpdateAndView(b *testing.B) {
	v := NewEventsView(b.Context(), nil)
	v.SetSize(160, 35)
	v.Update(eventsStartedMsg{ch: make(chan docker.EventUpdate)})
	initial := make([]docker.EventUpdate, eventCapacity)
	for i := range initial {
		initial[i] = testEvent(i)
	}
	v.Update(eventBatch(v, initial...))
	batch := make([]docker.EventUpdate, 100)
	for i := range batch {
		batch[i] = testEvent(i)
	}
	b.ReportAllocs()
	for b.Loop() {
		v.Update(eventBatch(v, batch...))
		v.View()
	}
}

func TestEventsNarrowLayout(t *testing.T) {
	v := newTestEventsView(t)
	e := testEvent(1)
	e.Event.Name = strings.Repeat("container", 10)
	e.Event.Project = strings.Repeat("project", 10)
	e.Event.Action = "health_status: unhealthy"
	v.Update(eventBatch(v, e))
	v.SetSize(80, 12)
	out := ansi.Strip(v.View())
	if !strings.Contains(out, "health_status: unhealthy") || strings.Contains(out, "PROJECT / SERVICE") {
		t.Fatal(out)
	}
	v.SetSize(180, 20)
	if !strings.Contains(ansi.Strip(v.View()), "PROJECT / SERVICE") {
		t.Fatal("resize did not restore project column")
	}
}

func TestContainerRefreshCoalescesDuringEventStorm(t *testing.T) {
	v := NewContainersView(t.Context(), &fakeClient{})
	first := v.load()
	if v.Update(eventsBatchMsg{updates: []docker.EventUpdate{testEvent(1)}}) == nil {
		t.Fatal("event refresh not scheduled")
	}
	for range 99 {
		if v.Update(eventsBatchMsg{updates: []docker.EventUpdate{testEvent(1)}}) != nil {
			t.Fatal("overlapping load scheduled")
		}
	}
	if v.Update(containersEventRefreshMsg{generation: v.eventGeneration}) != nil {
		t.Fatal("scheduled event refresh overlapped the running request")
	}
	followup := v.Update(first())
	if followup == nil {
		t.Fatal("pending change lost")
	}
	if v.Update(followup()) != nil || v.loading || v.pending {
		t.Fatal("refresh loop did not settle")
	}
}
