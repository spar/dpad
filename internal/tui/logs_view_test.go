package tui

import (
	"fmt"
	"testing"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

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

func TestLogsPageDownGlidesToTarget(t *testing.T) {
	v := &LogsView{vp: viewport.New(viewport.WithWidth(40), viewport.WithHeight(10))}
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprint(i)
	}
	v.vp.SetContentLines(lines)

	cmd := v.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	v.Update(tea.KeyPressMsg{Code: tea.KeyPgDown}) // a second press extends the target
	if v.target != 20 {
		t.Fatalf("target = %d, want 20", v.target)
	}
	frames := 0
	for cmd != nil {
		frames++
		if frames > 20 {
			t.Fatal("animation did not settle")
		}
		cmd = v.Update(scrollFrameMsg{v, v.gen})
	}
	if y := v.vp.YOffset(); y != 20 {
		t.Errorf("YOffset = %d, want 20", y)
	}
	if frames < 3 {
		t.Errorf("settled in %d frames, want a visible glide", frames)
	}
}
