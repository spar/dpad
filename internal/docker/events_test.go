package docker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/moby/moby/api/types/events"
)

func recv(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("no signal for %s", what)
	}
}

func TestContainerChanges(t *testing.T) {
	f := newFakeEngine()
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := (&Client{api: f}).ContainerChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	recv(t, ch, "the initial load")

	// A burst of events coalesces into one pending signal.
	f.send(events.ActionCreate, "a")
	f.send(events.ActionStart, "a")
	f.send(events.ActionDie, "a")
	time.Sleep(50 * time.Millisecond) // let the last event's handler run
	recv(t, ch, "a burst of events")
	select {
	case <-ch:
		t.Error("a burst should leave at most one pending signal")
	case <-time.After(50 * time.Millisecond):
	}

	// After the stream breaks, resubscribing signals so missed changes load.
	f.fail(errors.New("daemon restarted"))
	recv(t, ch, "resubscribing")

	cancel()
	waitFor(t, "the channel to close", func() bool {
		_, ok := <-ch
		return !ok
	})
}
