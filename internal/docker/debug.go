package docker

import (
	"context"
	"net/http"
	"time"

	"github.com/moby/moby/client"

	"dpad/internal/debuglog"
)

// logRequests records each Engine API request in the debug log with its
// status and how long the daemon took to answer. Streams (stats, logs,
// events) are logged when their headers arrive. Query strings are left
// out: filters can hold container names.
func logRequests() []client.Opt {
	return []client.Opt{
		client.WithHTTPRequestHook(func(req *http.Request) error {
			// The client sends this request but its transport clones it, so
			// the start time travels in the context the clone inherits.
			*req = *req.WithContext(context.WithValue(req.Context(), requestStart{}, time.Now()))
			return nil
		}),
		client.WithHTTPResponseHook(func(resp *http.Response) {
			args := []any{"method", resp.Request.Method, "path", resp.Request.URL.Path, "status", resp.StatusCode}
			if at, ok := resp.Request.Context().Value(requestStart{}).(time.Time); ok {
				args = append(args, "ms", time.Since(at).Milliseconds())
			}
			debuglog.Info("api", args...)
		}),
	}
}

type requestStart struct{}
