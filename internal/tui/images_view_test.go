package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"

	"dpad/internal/docker"
)

func TestImageSelectionStaysOnTagAcrossRefreshAndRemoval(t *testing.T) {
	v := NewImagesView(context.Background(), nil)
	v.SetSize(160, 12)
	v.Update(imagesMsg{items: []image.Summary{{ID: "id", RepoTags: []string{"web:a", "web:b", "web:c"}}}})
	v.HandleKey("j")
	if r, ok := v.SelectedImage(); !ok || r.Reference != "web:b" || !r.UntagOnly {
		t.Fatalf("wrong selected tag: %+v", r)
	}
	v.Update(imagesMsg{items: []image.Summary{{ID: "id", RepoTags: []string{"web:c", "web:b", "web:a"}}}})
	if r, _ := v.SelectedImage(); r.Reference != "web:b" {
		t.Fatal("tag reorder changed selection")
	}
	v.Update(imagesMsg{items: []image.Summary{{ID: "id", RepoTags: []string{"web:c", "web:a"}}}})
	if r, _ := v.SelectedImage(); r.Reference != "web:a" {
		t.Fatalf("removed tag did not select adjacent row: %+v", r)
	}
	v.Update(containersMsg{items: []container.Summary{{ImageID: "id", State: container.StateRunning}, {ImageID: "id", State: container.StateExited}, {ImageID: "other", Image: "web:a"}}})
	if !strings.Contains(ansi.Strip(v.View()), "1 running / 2 total") {
		t.Fatal("usage did not include stopped containers")
	}
	v.Update(containersMsg{err: errors.New("offline")})
	if !strings.Contains(v.View(), "unknown") {
		t.Fatal("failed usage query was shown as unused")
	}
	v.SetFilter("other")
	v.Update(imagesMsg{items: []image.Summary{
		{ID: "id", RepoTags: []string{"web:a"}},
		{ID: "other", RepoTags: []string{"other:c"}},
	}})
	if r, ok := v.SelectedImage(); !ok || r.Reference != "other:c" {
		t.Fatalf("refresh selected a filtered-out image: %+v", r)
	}
	v.Update(imagesMsg{})
	if _, ok := v.SelectedImage(); ok {
		t.Fatal("empty images selected a removal target")
	}
}

func TestImageDetailsLoadRefreshAndCancel(t *testing.T) {
	f := &fakeClient{imageInfo: docker.ImageInfo{Image: image.InspectResponse{ID: "id", RepoTags: []string{"web:a", "web:b"}}}}
	v, load := NewImageDetailsView(context.Background(), f, docker.ImageRemoval{ID: "id", Reference: "web:a"}, 80, 8)
	defer v.Close()
	v.Update(load())
	if v.loading || !v.removal.UntagOnly || !strings.Contains(v.View(), "web:a") {
		t.Fatal("image details did not load")
	}
	v.Update(key("G"))
	if v.vp.YOffset() == 0 {
		t.Fatal("image details did not scroll")
	}
	v.SetSize(40, 4)
	if v.vp.Width() != 40 || v.vp.Height() != 4 {
		t.Fatal("image details did not resize")
	}
	refresh := v.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	v.Update(imageDetailsMsg{v: v, gen: v.gen - 1, err: errors.New("stale")})
	v.Update(imageDetailsMsg{v: &ImageDetailsView{}, gen: v.gen, err: errors.New("other")})
	if !v.loading || v.err != nil {
		t.Fatal("stale details replaced current request")
	}
	f.imageErr = errors.New("image disappeared")
	v.Update(refresh())
	if !strings.Contains(v.Status(), "inspect failed") {
		t.Fatal("inspect error hidden")
	}
	f.imageErr = nil
	v.Update(v.load()())
	if v.err != nil {
		t.Fatal("image details could not retry")
	}
	v.Close()
	if !errors.Is(v.ctx.Err(), context.Canceled) {
		t.Fatal("closing details did not cancel context")
	}
}

func TestImageRemovalConfirmationAndDetailsReturn(t *testing.T) {
	f := &fakeClient{imageInfo: docker.ImageInfo{Image: image.InspectResponse{ID: "id", RepoTags: []string{"web:a", "web:b"}}}}
	a := newTestApp()
	a.ctx, a.client = context.Background(), f
	a.Update(imagesMsg{items: []image.Summary{{ID: "id", RepoTags: []string{"web:a", "web:b"}}}})
	press(a, "2", "/", "w", "e", "b", "enter", "j")
	press(a, "x")
	if a.confirm == nil || !strings.Contains(ansi.Strip(a.footer()), "tag web:b only") {
		t.Fatal("multi-tag confirmation did not distinguish untagging")
	}
	press(a, "n")
	if len(f.actions) != 0 {
		t.Fatal("cancelling confirmation called daemon")
	}
	_, load := a.Update(key("d"))
	a.Update(load())
	if a.imageDetails == nil || a.details != nil {
		t.Fatal("image details used container view")
	}
	press(a, "e", "S", "R", "enter")
	if a.shell != nil || a.logs != nil || a.confirm != nil {
		t.Fatal("image details offered container actions")
	}
	press(a, "x")
	_, remove := a.Update(key("y"))
	msg := remove().(actionDoneMsg)
	_, refresh := a.Update(msg)
	if msg.err != nil || a.imageDetails != nil || refresh == nil || a.tabs[1].Filter() != "web" {
		t.Fatal("removal did not close details, refresh or preserve filter")
	}
	if f.actions[len(f.actions)-1] != "image remove web:b" {
		t.Fatalf("wrong removal target: %v", f.actions)
	}
	if r, _ := a.tabs[1].(*ImagesView).SelectedImage(); r.Reference != "web:b" {
		t.Fatal("unrefreshed list selection changed unexpectedly")
	}
	a.setFilter("")
	for _, tt := range []struct {
		tags []string
		want string
	}{{[]string{"web:a"}, "last tag"}, {nil, "untagged image"}} {
		a.Update(imagesMsg{items: []image.Summary{{ID: "id", RepoTags: tt.tags}}})
		press(a, "x")
		if !strings.Contains(a.footer(), tt.want) {
			t.Fatalf("missing %q in confirmation", tt.want)
		}
		press(a, "esc")
	}
}

func TestImageRemovalFailureKeepsDetailsAndRefreshes(t *testing.T) {
	f := &fakeClient{imageInfo: docker.ImageInfo{Image: image.InspectResponse{ID: "id"}}}
	a := newTestApp()
	a.ctx, a.client = context.Background(), f
	a.Update(imagesMsg{items: []image.Summary{{ID: "id"}}})
	press(a, "2")
	_, load := a.Update(key("enter"))
	a.Update(load())
	f.imageErr = errors.New("image is in use")
	press(a, "x")
	_, remove := a.Update(key("y"))
	_, refresh := a.Update(remove())
	if a.imageDetails == nil || !a.flashErr || !strings.Contains(a.flash, "in use") || refresh == nil {
		t.Fatal("remove failure hid details/error or skipped refresh")
	}
	press(a, "esc")
	if a.imageDetails != nil || a.active != 1 {
		t.Fatal("escape did not return to Images")
	}
}

func TestImagesReloadWhenTheTabOpens(t *testing.T) {
	c := &fakeClient{images: []image.Summary{{ID: "a", RepoTags: []string{"web:1"}}}}
	v := NewImagesView(t.Context(), c)
	v.Update(v.Init()())
	c.images = append(c.images, image.Summary{ID: "b", RepoTags: []string{"api:1"}})
	cmd := v.Focus()
	if cmd == nil {
		t.Fatal("opening Images did not reload")
	}
	if v.Focus() != nil {
		t.Fatal("a second load started while one was running")
	}
	if v.Update(refreshMsg{}); !v.pending {
		t.Fatal("a refresh during a load was dropped")
	}
	if again := v.Update(cmd()); again == nil || len(v.items) != 2 {
		t.Fatalf("items = %d, pending reload = %v", len(v.items), again != nil)
	}
}
