package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
)

const testTS = "2026-11-10T12:00:00.000000000Z "

// frame wraps p in the daemon's multiplexed stream format.
func frame(b *bytes.Buffer, stream stdcopy.StdType, p string) {
	hdr := [8]byte{byte(stream)}
	binary.BigEndian.PutUint32(hdr[4:], uint32(len(p)))
	b.Write(hdr[:])
	b.WriteString(p)
}

// collect runs readLogs to completion, failing the test if it hangs.
func collect(t *testing.T, raw []byte, tty bool) []LogLine {
	t.Helper()
	out := make(chan LogLine, 64)
	done := make(chan error, 1)
	go func() {
		done <- readLogs(context.Background(), bytes.NewReader(raw), tty, "web", out)
		close(out)
	}()
	var lines []LogLine
	timeout := time.After(5 * time.Second)
	for {
		select {
		case l, ok := <-out:
			if !ok {
				if err := <-done; err != nil {
					t.Fatalf("readLogs: %v", err)
				}
				return lines
			}
			lines = append(lines, l)
		case <-timeout:
			t.Fatalf("readLogs hung after %d lines", len(lines))
		}
	}
}

func TestReadLogsLongLine(t *testing.T) {
	long := strings.Repeat("x", 2*maxLogLine+10)
	raw := []byte(testTS + long + "\n" + testTS + "after\n")
	for _, tty := range []bool{true, false} {
		in := raw
		if !tty {
			var b bytes.Buffer
			frame(&b, stdcopy.Stdout, string(raw))
			in = b.Bytes()
		}
		lines := collect(t, in, tty)
		if len(lines) != 4 || lines[3].Text != "after" {
			t.Fatalf("tty=%v: got %d lines, want 3 chunks + \"after\"", tty, len(lines))
		}
		var got strings.Builder
		got.WriteString(testTS[:len(testTS)-1] + " ") // first chunk's timestamp is parsed off
		for _, l := range lines[:3] {
			got.WriteString(l.Text)
		}
		if got.Len() != len(testTS)+len(long) {
			t.Errorf("tty=%v: long line came back as %d bytes, want %d", tty, got.Len(), len(testTS)+len(long))
		}
	}
}

func TestReadLogsStderr(t *testing.T) {
	var b bytes.Buffer
	frame(&b, stdcopy.Stdout, testTS+"out\n")
	frame(&b, stdcopy.Stderr, testTS+"err\n")
	lines := collect(t, b.Bytes(), false)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	for _, l := range lines {
		if l.Stderr != (l.Text == "err") || l.Time.IsZero() || l.Container != "web" {
			t.Errorf("bad line %+v", l)
		}
	}
}
