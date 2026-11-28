package docker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/moby/moby/client"

	"github.com/spar/dpad/internal/debuglog"
)

func TestDebugLogRecordsRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("OK")) }))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "dp.log")
	f, err := debuglog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c, err := client.New(append(logRequests(), client.WithHost("tcp://"+srv.Listener.Addr().String()), client.WithAPIVersion("1.47"))...)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Ping(context.Background(), client.PingOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`msg=api method=(GET|HEAD) path=/_ping status=200 ms=\d+`).Match(data) {
		t.Fatalf("log lacks the ping request:\n%s", data)
	}
}
