package docker

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

type retryStatsEngine struct {
	*fakeEngine
	failFirst bool
	eof       bool
}

func (f *retryStatsEngine) ContainerStats(ctx context.Context, id string, opts client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
	f.mu.Lock()
	fail := f.failFirst
	f.failFirst = false
	f.mu.Unlock()
	if fail {
		if f.eof {
			return client.ContainerStatsResult{Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return client.ContainerStatsResult{}, errors.New("connection lost")
	}
	return f.fakeEngine.ContainerStats(ctx, id, opts)
}

func TestStatsRecoversRequestAndIdleFailures(t *testing.T) {
	for _, mode := range []string{"request failure", "EOF", "stalled body"} {
		t.Run(mode, func(t *testing.T) {
			failFirst := mode != "stalled body"
			f := &retryStatsEngine{fakeEngine: newFakeEngine("web"), failFirst: failFirst, eof: mode == "EOF"}
			ctx, cancel := context.WithCancel(t.Context())
			ch, err := (&Client{api: f}).watchStats(ctx, 40*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				cancel()
				for range ch {
				}
			}()
			next := func() StatsUpdate {
				t.Helper()
				select {
				case u := <-ch:
					return u
				case <-time.After(3 * time.Second):
					t.Fatal("no recovery update")
					return StatsUpdate{}
				}
			}
			var first StatsUpdate
			if !failFirst {
				first = next()
				if first.Unavailable {
					t.Fatal(first)
				}
			}
			failed := next()
			if !failed.Unavailable || failed.Gone || failed.Err == nil {
				t.Fatalf("want unavailable row, got %+v", failed)
			}
			recovered := next()
			if recovered.Unavailable || recovered.Gone || recovered.Generation <= failed.Generation || recovered.Generation <= first.Generation {
				t.Fatalf("recovery did not get fresh generation: %+v", recovered)
			}
			f.mu.Lock()
			maxReaders := f.maxInflight
			f.mu.Unlock()
			if maxReaders != 1 {
				t.Fatalf("%d concurrent readers for one container", maxReaders)
			}
			cancel()
			for range ch {
			}
			calls := f.calls("web")
			time.Sleep(60 * time.Millisecond)
			if f.calls("web") != calls {
				t.Fatal("retried after cancellation")
			}
		})
	}
}

func TestStatsStopDuringBackoff(t *testing.T) {
	f := &retryStatsEngine{fakeEngine: newFakeEngine("web"), failFirst: true}
	ctx, cancel := context.WithCancel(t.Context())
	ch, err := (&Client{api: f}).WatchStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		for range ch {
		}
	}()
	select {
	case failed := <-ch:
		if !failed.Unavailable {
			t.Fatal(failed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("missing failure")
	}
	f.send(events.ActionDie, "web")
	select {
	case gone := <-ch:
		if !gone.Gone {
			t.Fatal(gone)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("backoff prevented stop")
	}
	time.Sleep(resubscribeDelay + 50*time.Millisecond)
	if f.calls("web") != 0 {
		t.Fatal("stopped container retried")
	}
}

func TestConnectionLabelBoundToClient(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("DOCKER_CONFIG", cfg)
	t.Setenv("DOCKER_CONTEXT", "remote")
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	writeContext(t, cfg, "remote", "tcp://localhost:2375")
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.ConnectionLabel() != "remote @ tcp://localhost:2375" {
		t.Fatal(c.ConnectionLabel())
	}
	t.Setenv("DOCKER_HOST", "tcp://localhost:1234")
	if c.ConnectionLabel() != "remote @ tcp://localhost:2375" {
		t.Fatal("bound label changed")
	}
	override, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer override.Close()
	if override.ConnectionLabel() != "tcp://localhost:1234" {
		t.Fatal(override.ConnectionLabel())
	}
	if got := connectionLabel("remote", "https://user:password@host:1234/path?secret=token#private"); got != "remote @ https://host:1234/path" {
		t.Fatal(got)
	}
}
