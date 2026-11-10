package sortx

import (
	"slices"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"

	"dockernext/internal/docker"
)

func statNames(items []docker.Stats) []string {
	var out []string
	for _, s := range items {
		out = append(out, s.Name)
	}
	return out
}

func TestStats(t *testing.T) {
	items := []docker.Stats{
		{Name: "b", CPUPercent: 10, MemUsage: 300, NetRx: 1, PIDs: 3},
		{Name: "a", CPUPercent: 50, MemUsage: 100, NetRx: 9, PIDs: 3},
		{Name: "c", CPUPercent: 10, MemUsage: 200, NetTx: 5, PIDs: 1},
	}
	tests := []struct {
		key     Key
		reverse bool
		want    []string
	}{
		{"cpu", false, []string{"a", "b", "c"}}, // tie on 10% broken by name
		{"cpu", true, []string{"c", "b", "a"}},
		{"mem", false, []string{"b", "c", "a"}},
		{"net", false, []string{"a", "c", "b"}},
		{"pids", false, []string{"a", "b", "c"}},
		{"name", false, []string{"a", "b", "c"}},
		{"name", true, []string{"c", "b", "a"}},
	}
	for _, tt := range tests {
		Stats(items, tt.key, tt.reverse)
		if got := statNames(items); !slices.Equal(got, tt.want) {
			t.Errorf("Stats(%s, reverse=%v) = %v, want %v", tt.key, tt.reverse, got, tt.want)
		}
	}
}

func TestContainersByStatus(t *testing.T) {
	items := []container.Summary{
		{Names: []string{"/old"}, State: "exited"},
		{Names: []string{"/web"}, State: "running"},
		{Names: []string{"/db"}, State: "paused"},
		{Names: []string{"/api"}, State: "running"},
	}
	Containers(items, "status", false)
	var got []string
	for _, c := range items {
		got = append(got, docker.ContainerName(c.Names))
	}
	if want := []string{"api", "web", "db", "old"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestImagesDanglingLast(t *testing.T) {
	items := []image.Summary{
		{ID: "1", Size: 5},
		{ID: "2", RepoTags: []string{"zeta:1"}, Size: 5},
		{ID: "3", RepoTags: []string{"alpha:1"}, Size: 5},
	}
	Images(items, "size", false)
	var got []string
	for _, img := range items {
		got = append(got, img.ID)
	}
	if want := []string{"3", "2", "1"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// A tag that sorts after any letter still comes before untagged images.
	items = []image.Summary{{ID: "1"}, {ID: "2", RepoTags: []string{"\uffff\uffff:1"}}}
	Images(items, Name, false)
	if items[0].ID != "2" {
		t.Errorf("untagged image sorted before a tagged one")
	}
}

func TestParseAndNext(t *testing.T) {
	if k, err := Parse("cpu", StatsKeys); k != CPU || err != nil {
		t.Errorf("Parse(cpu) = %q, %v", k, err)
	}
	if _, err := Parse("size", StatsKeys); err == nil {
		t.Error("Parse should reject a key the list doesn't support")
	}
	if Next(Name, StatsKeys) != CPU || Next(CPU, StatsKeys) != Mem {
		t.Error("Next should cycle through keys")
	}
}
