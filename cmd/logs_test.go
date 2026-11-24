package cmd

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"

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

func TestResolveLogTargets(t *testing.T) {
	ctr := func(id, name, project, service string, extra ...string) container.Summary {
		labels := map[string]string{}
		if project != "" {
			labels[composeProjectLabel], labels[composeServiceLabel] = project, service
		}
		for i := 0; i+1 < len(extra); i += 2 {
			labels[extra[i]] = extra[i+1]
		}
		return container.Summary{ID: id, Names: []string{"/" + name}, Labels: labels}
	}
	list := []container.Summary{
		ctr("aaa111", "shop-web-1", "shop", "web"),
		ctr("bbb222", "shop-worker-2", "shop", "worker"),
		ctr("ccc333", "shop-worker-1", "shop", "worker"),
		ctr("ddd444", "shop-worker-run-x", "shop", "worker", composeOneoffLabel, "True"),
		ctr("eee555", "blog-web-1", "blog", "web"),
		ctr("fff666", "blog-db-1", "blog", "db"),
		ctr("abc777", "redis", "", ""),
	}
	for _, tc := range []struct {
		args []string
		want []string
		err  string
	}{
		{args: []string{"redis"}, want: []string{"redis"}},
		{args: []string{"abc7"}, want: []string{"abc7"}},
		{args: []string{"worker"}, want: []string{"shop-worker-1", "shop-worker-2"}},
		{args: []string{"db", "redis", "blog-db-1"}, want: []string{"blog-db-1", "redis"}},
		{args: []string{"blog"}, want: []string{"blog-db-1", "blog-web-1"}},
		{args: []string{"shop-web"}, want: []string{"shop-web-1"}},
		{args: []string{"missing"}, want: []string{"missing"}},
		{args: []string{"web"}, err: "several Compose projects (blog, shop)"},
		{args: []string{"shop-w"}, err: "matches several containers (shop-web-1, shop-worker-1, shop-worker-2, shop-worker-run-x)"},
	} {
		got, err := resolveLogTargets(tc.args, list)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%v: err = %v, want %q", tc.args, err, tc.err)
			}
			continue
		}
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("%v = %v, %v; want %v", tc.args, got, err, tc.want)
		}
	}
}
