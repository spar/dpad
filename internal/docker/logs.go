package docker

import (
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"fmt"
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

// readLogs splits a raw log stream into lines on out, in stream order, until
// the stream ends or ctx is cancelled. Non-TTY streams are multiplexed:
// frames of an 8-byte header (stream type, 3 zero bytes, big-endian payload
// length) and a payload. Reading them in one pass keeps stdout and stderr
// lines in the order the daemon sent them.
func readLogs(ctx context.Context, r io.Reader, tty bool, name string, out chan<- LogLine) error {
	var stdout, stderr lineSplitter
	emit := func(isErr bool) func([]byte) error {
		return func(line []byte) error {
			ts, text := splitTimestamp(string(line))
			select {
			case out <- LogLine{Container: name, Stderr: isErr, Time: ts, Text: text}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	emitOut, emitErr := emit(false), emit(true)
	fail := func(err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}

	buf := make([]byte, 32*1024)
	if tty {
		for {
			n, err := r.Read(buf)
			if err := stdout.write(buf[:n], emitOut); err != nil {
				return err
			}
			if err == io.EOF {
				return stdout.flush(emitOut)
			}
			if err != nil {
				return fail(err)
			}
		}
	}

	var hdr [8]byte
	for {
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			if err == io.EOF {
				return cmp.Or(stdout.flush(emitOut), stderr.flush(emitErr))
			}
			return fail(err)
		}
		size := int(binary.BigEndian.Uint32(hdr[4:]))
		if size > len(buf) {
			buf = make([]byte, size)
		}
		if _, err := io.ReadFull(r, buf[:size]); err != nil {
			return fail(err)
		}
		var err error
		switch stdcopy.StdType(hdr[0]) {
		case stdcopy.Stdin, stdcopy.Stdout:
			err = stdout.write(buf[:size], emitOut)
		case stdcopy.Stderr:
			err = stderr.write(buf[:size], emitErr)
		case stdcopy.Systemerr:
			return fmt.Errorf("daemon: %s", buf[:size])
		default:
			return fmt.Errorf("unrecognized log stream type %d", hdr[0])
		}
		if err != nil {
			return err
		}
	}
}

// lineSplitter turns a byte stream into lines, holding a partial last line
// until it completes. A line longer than maxLogLine is emitted in chunks.
type lineSplitter struct{ buf []byte }

func (s *lineSplitter) write(p []byte, emit func([]byte) error) error {
	s.buf = append(s.buf, p...)
	rest := s.buf
	for {
		n := bytes.IndexByte(rest, '\n')
		if n < 0 || n > maxLogLine {
			if len(rest) < maxLogLine {
				break
			}
			n = maxLogLine
			if err := emit(rest[:n]); err != nil {
				return err
			}
			rest = rest[n:]
			continue
		}
		if err := emit(rest[:n]); err != nil {
			return err
		}
		rest = rest[n+1:]
	}
	s.buf = append(s.buf[:0], rest...)
	return nil
}

// flush emits a final line that had no trailing newline.
func (s *lineSplitter) flush(emit func([]byte) error) error {
	if len(s.buf) == 0 {
		return nil
	}
	defer func() { s.buf = s.buf[:0] }()
	return emit(s.buf)
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
