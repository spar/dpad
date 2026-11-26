package docker

import (
	"bytes"
	"strings"
	"testing"

	"github.com/moby/moby/api/pkg/stdcopy"
)

// FuzzReadLogs feeds arbitrary log output through both stream formats. The
// multiplexed form is framed here so a fuzzed length can't ask for gigabytes.
func FuzzReadLogs(f *testing.F) {
	f.Add([]byte(testTS+"hello\n"+testTS+"world"), false)
	f.Add([]byte("no timestamp\r\nsecond\n\n"), true)
	f.Add([]byte(testTS), false)
	f.Fuzz(func(t *testing.T, data []byte, tty bool) {
		raw := data
		if !tty {
			var b bytes.Buffer
			half := len(data) / 2
			frame(&b, stdcopy.Stdout, string(data[:half]))
			frame(&b, stdcopy.Stderr, string(data[half:]))
			raw = b.Bytes()
		}
		for _, l := range collect(t, raw, tty) {
			if strings.Contains(l.Text, "\n") || len(l.Text) > maxLogLine {
				t.Fatalf("bad line %q", l.Text)
			}
		}
	})
}
