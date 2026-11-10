package docker

import (
	"bufio"
	"cmp"
	"context"
	"io"
	"strings"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// LogLine is a single line of container output.
type LogLine struct {
	Container string
	Stderr    bool
	Time      time.Time
	Text      string
}

// LogOptions controls which log lines are fetched.
type LogOptions struct {
	Follow bool
	Tail   string
	Since  string
}

// Logs streams log lines for one container onto out until the stream ends or
// ctx is cancelled. Lines are timestamped by the daemon so callers can merge
// several containers in order.
func (c *Client) Logs(ctx context.Context, id string, opts LogOptions, out chan<- LogLine) error {
	info, err := c.api.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return err
	}
	name := strings.TrimPrefix(info.Container.Name, "/")
	tty := info.Container.Config != nil && info.Container.Config.Tty

	rc, err := c.api.ContainerLogs(ctx, id, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     opts.Follow,
		Tail:       opts.Tail,
		Since:      opts.Since,
		Timestamps: true,
	})
	if err != nil {
		return err
	}
	defer rc.Close()

	return readLogs(ctx, rc, tty, name, out)
}

// maxLogLine caps the length of one emitted line; longer lines are split.
const maxLogLine = 1024 * 1024

// readLogs splits a raw log stream into lines on out until the stream ends or
// ctx is cancelled. It returns the first read error, if any.
func readLogs(ctx context.Context, rc io.Reader, tty bool, name string, out chan<- LogLine) error {
	// TTY containers produce a raw stream; others are multiplexed stdout/stderr.
	outR, outW := io.Pipe()
	errR, errW := io.Pipe()
	go func() {
		var err error
		if tty {
			_, err = io.Copy(outW, rc)
		} else {
			_, err = stdcopy.StdCopy(outW, errW, rc)
		}
		outW.CloseWithError(err)
		errW.CloseWithError(err)
	}()

	errs := make(chan error, 2)
	scan := func(r *io.PipeReader, stderr bool) {
		err := func() error {
			sc := bufio.NewScanner(r)
			sc.Buffer(make([]byte, 64*1024), maxLogLine)
			sc.Split(scanLogLines)
			for sc.Scan() {
				ts, text := splitTimestamp(sc.Text())
				select {
				case out <- LogLine{Container: name, Stderr: stderr, Time: ts, Text: text}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return sc.Err()
		}()
		// Closing the reader fails the copier's next write, so a scanner that
		// stops early ends the other stream too instead of leaving it blocked.
		r.CloseWithError(err)
		errs <- err
	}
	go scan(outR, false)
	go scan(errR, true)
	return cmp.Or(<-errs, <-errs)
}

// scanLogLines is bufio.ScanLines, except a line longer than maxLogLine is
// emitted in maxLogLine chunks instead of stopping the scanner.
func scanLogLines(data []byte, atEOF bool) (int, []byte, error) {
	advance, token, err := bufio.ScanLines(data, atEOF)
	if advance == 0 && token == nil && err == nil && len(data) >= maxLogLine {
		return maxLogLine, data[:maxLogLine], nil
	}
	return advance, token, err
}

// splitTimestamp separates the RFC3339Nano prefix added by Timestamps: true.
func splitTimestamp(line string) (time.Time, string) {
	ts, rest, ok := strings.Cut(line, " ")
	if !ok {
		ts = line
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, line
	}
	return t, strings.TrimSuffix(rest, "\r")
}
