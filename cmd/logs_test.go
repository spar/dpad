package cmd

import (
	"slices"
	"testing"
	"time"

	"dpad/internal/docker"
)

func at(sec int, text string) docker.LogLine {
	return docker.LogLine{Time: time.Unix(int64(sec), 0), Text: text}
}

func TestMergeLogsOrdersByTime(t *testing.T) {
	a, b := make(chan docker.LogLine, 8), make(chan docker.LogLine, 8)
	for _, l := range []docker.LogLine{at(1, "a1"), at(3, "a3"), at(5, "a5")} {
		a <- l
	}
	for _, l := range []docker.LogLine{at(2, "b2"), at(3, "b3"), at(4, "b4")} {
		b <- l
	}
	close(a)
	close(b)
	var got []string
	mergeLogs([]<-chan docker.LogLine{a, b}, func(l docker.LogLine) { got = append(got, l.Text) })
	if want := []string{"a1", "b2", "a3", "b3", "b4", "a5"}; !slices.Equal(got, want) {
		t.Errorf("merged = %v, want %v", got, want)
	}
}

func TestMergeLogsStreams(t *testing.T) {
	a, b := make(chan docker.LogLine), make(chan docker.LogLine)
	emitted := make(chan string, 8)
	done := make(chan struct{})
	go func() {
		mergeLogs([]<-chan docker.LogLine{a, b}, func(l docker.LogLine) { emitted <- l.Text })
		close(done)
	}()
	a <- at(1, "a1")
	b <- at(2, "b2")
	// a1 is known to be first as soon as both sources have a line, long
	// before either source ends.
	select {
	case got := <-emitted:
		if got != "a1" {
			t.Errorf("first line = %q, want a1", got)
		}
	case <-time.After(time.Second):
		t.Fatal("merge waited for the sources to end instead of streaming")
	}
	close(a)
	close(b)
	<-done
}
