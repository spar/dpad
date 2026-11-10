package docker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/events"
)

// waitFor polls cond until it holds, failing the test after a few seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); !cond(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// tracker consumes WatchStats updates and records which containers are shown.
type tracker struct {
	ch   <-chan StatsUpdate
	seen map[string]bool
}

func (tr *tracker) drain() {
	for {
		select {
		case u := <-tr.ch:
			tr.seen[u.Stats.ID] = !u.Gone
		default:
			return
		}
	}
}

func (tr *tracker) waitShown(t *testing.T, id string, want bool) {
	t.Helper()
	waitFor(t, id+" shown="+map[bool]string{true: "true", false: "false"}[want], func() bool {
		tr.drain()
		return tr.seen[id] == want
	})
}

func watch(t *testing.T, f *fakeEngine) *tracker {
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := (&Client{api: f}).WatchStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		for range ch {
		}
	})
	return &tracker{ch: ch, seen: map[string]bool{}}
}

func TestWatchStatsRestart(t *testing.T) {
	f := newFakeEngine("web")
	hold := make(chan struct{})
	f.hold["web"] = hold
	tr := watch(t, f)
	release := sync.OnceFunc(func() { close(hold) })
	t.Cleanup(release) // runs before watch's cleanup, which waits for the stream
	tr.waitShown(t, "web", true)

	// Restart: the old stream is still shutting down when start arrives.
	f.send(events.ActionDie, "web")
	f.send(events.ActionStart, "web")
	waitFor(t, "a new stats stream", func() bool { return f.calls("web") == 2 })
	release()

	// The old stream's end must not hide the restarted container.
	time.Sleep(50 * time.Millisecond)
	tr.drain()
	if !tr.seen["web"] {
		t.Error("restarted container disappeared from stats")
	}
}

func TestWatchStatsDie(t *testing.T) {
	f := newFakeEngine("web", "db")
	tr := watch(t, f)
	tr.waitShown(t, "db", true)
	f.send(events.ActionDie, "db")
	tr.waitShown(t, "db", false)
	tr.waitShown(t, "web", true)
}

func TestWatchStatsResubscribes(t *testing.T) {
	f := newFakeEngine("web")
	tr := watch(t, f)
	tr.waitShown(t, "web", true)

	// While the event stream is down, web stops and db starts.
	f.setContainers("db")
	f.fail(errors.New("daemon restarted"))
	waitFor(t, "a new event subscription", func() bool { return f.subscriptions() == 2 })
	tr.waitShown(t, "db", true)
	tr.waitShown(t, "web", false)

	// Events on the new subscription are followed.
	f.send(events.ActionStart, "api")
	tr.waitShown(t, "api", true)
}
