// Package debuglog writes an opt-in diagnostic log to a local file, for
// tracking down slow or failing connections. It is off unless --debug-log
// or $DPAD_DEBUG_LOG names a file, and it never sends anything anywhere.
package debuglog

import (
	"io"
	"log/slog"
	"os"
)

// logger discards everything until Open: the default slog logger would
// write to stderr, over the TUI.
var logger = slog.New(slog.DiscardHandler)

var on bool

// Open appends the log to path, readable only by this user. Closing what it
// returns closes the file and turns the log off again.
func Open(path string) (io.Closer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	logger, on = slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})), true
	return file{f}, nil
}

type file struct{ *os.File }

func (f file) Close() error {
	logger, on = slog.New(slog.DiscardHandler), false
	return f.File.Close()
}

// On reports whether a log file is open, so callers can skip work that
// only feeds the log.
func On() bool { return on }

// Info records something that happened, such as a request or a reconnect.
func Info(msg string, args ...any) { logger.Info(msg, args...) }

// Warn records something that went wrong.
func Warn(msg string, args ...any) { logger.Warn(msg, args...) }
