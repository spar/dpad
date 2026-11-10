package tui

import (
	"testing"
	"time"

	"dockernext/internal/docker"
)

func TestLogsWaitBatchesSteadyStream(t *testing.T) {
	v := &LogsView{ch: make(chan docker.LogLine), errc: make(chan error, 1)}
	go func() {
		for range 20 {
			v.ch <- docker.LogLine{Text: "x"}
			time.Sleep(time.Millisecond)
		}
	}()
	msg, ok := v.wait()().(logBatchMsg)
	if !ok || len(msg.lines) < 10 {
		t.Errorf("want one batch with most of a steady 1ms stream, got %d lines", len(msg.lines))
	}
}
