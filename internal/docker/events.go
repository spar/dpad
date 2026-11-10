package docker

import (
	"context"
	"time"

	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

// Backoff for resubscribing to daemon events after the stream breaks.
const (
	resubscribeDelay    = time.Second
	maxResubscribeDelay = 5 * time.Second
)

// followEvents subscribes to the given container events and then calls
// resync, so nothing that happened before the subscription is missed. It
// returns resync's error, or a loop that calls handle for each event until
// ctx is done. If the event stream breaks (for example when the daemon
// restarts), the loop resubscribes with backoff and calls resync again.
func (c *Client) followEvents(ctx context.Context, actions []events.Action, resync func() error, handle func(events.Message)) (loop func(), err error) {
	names := make([]string, len(actions))
	for i, a := range actions {
		names[i] = string(a)
	}
	subscribe := func() (client.EventsResult, context.CancelFunc, error) {
		ectx, cancel := context.WithCancel(ctx)
		ev := c.api.Events(ectx, client.EventsListOptions{
			Filters: make(client.Filters).Add("type", string(events.ContainerEventType)).Add("event", names...),
		})
		if err := resync(); err != nil {
			cancel()
			return ev, cancel, err
		}
		return ev, cancel, nil
	}
	ev, cancel, err := subscribe()
	if err != nil {
		return nil, err
	}
	return func() {
		defer func() { cancel() }()
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-ev.Messages:
				handle(m)
			case <-ev.Err:
				cancel()
				for delay := resubscribeDelay; ; delay = min(2*delay, maxResubscribeDelay) {
					select {
					case <-ctx.Done():
						return
					case <-time.After(delay):
					}
					if ev, cancel, err = subscribe(); err == nil {
						break
					}
				}
			}
		}
	}, nil
}
