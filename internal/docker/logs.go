package docker

import (
	"bufio"
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

	done := make(chan struct{}, 2)
	scan := func(r io.Reader, stderr bool) {
		defer func() { done <- struct{}{} }()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			ts, text := splitTimestamp(sc.Text())
			select {
			case out <- LogLine{Container: name, Stderr: stderr, Time: ts, Text: text}:
			case <-ctx.Done():
				return
			}
		}
	}
	go scan(outR, false)
	go scan(errR, true)
	<-done
	<-done
	// Unblock the copier if the scanners stopped early (ctx cancelled).
	outR.Close()
	errR.Close()
	return nil
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
