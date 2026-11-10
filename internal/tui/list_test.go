package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
)

func TestContainersNavigateAndSelect(t *testing.T) {
	v := NewContainersView(nil, nil)
	v.Update(containersMsg{items: []container.Summary{
		{ID: "1", Names: []string{"/web"}, State: "running"},
		{ID: "2", Names: []string{"/db"}, State: "running"},
		{ID: "3", Names: []string{"/old"}, State: "exited"},
	}})
	v.View(120, 10, "")
	if _, name, _ := v.Selected(); name != "db" {
		t.Errorf("initial selection = %q, want db (first by status, then name)", name)
	}
	v.HandleKey("j")
	v.HandleKey("j")
	if _, name, _ := v.Selected(); name != "old" {
		t.Errorf("after j j selection = %q, want old", name)
	}
	v.HandleKey("s") // name → created → status cycles; status → name
	if v.key != "name" {
		t.Errorf("s cycled to %q, want name", v.key)
	}
	// The cursor follows the container, not the row number.
	if _, name, _ := v.Selected(); name != "old" {
		t.Errorf("after re-sort selection = %q, want old", name)
	}
	out := ansi.Strip(v.View(120, 10, "we"))
	if !strings.Contains(out, "web") || strings.Contains(out, "old") {
		t.Errorf("filter \"we\" shows:\n%s", out)
	}
}

func TestImagesAreNotSelectable(t *testing.T) {
	v := NewImagesView(nil, nil)
	v.Update(imagesMsg{items: []image.Summary{{ID: "sha256:abc", RepoTags: []string{"a:1", "a:2"}}}})
	out := ansi.Strip(v.View(120, 10, ""))
	if strings.Count(out, "\n") != 2 {
		t.Errorf("want a header and one row per tag:\n%s", out)
	}
	if _, _, ok := v.Selected(); ok {
		t.Error("image rows should not select a container")
	}
}
