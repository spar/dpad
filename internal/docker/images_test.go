package docker

import (
	"context"
	"errors"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
)

type imageEngine struct {
	engine
	info                           image.InspectResponse
	containers                     []container.Summary
	inspectErr, listErr, removeErr error
	removed                        string
	options                        client.ImageRemoveOptions
	all                            bool
}

func (f *imageEngine) ImageInspect(context.Context, string, ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	return client.ImageInspectResult{InspectResponse: f.info}, f.inspectErr
}
func (f *imageEngine) ContainerList(_ context.Context, opts client.ContainerListOptions) (client.ContainerListResult, error) {
	f.all = opts.All
	return client.ContainerListResult{Items: f.containers}, f.listErr
}
func (f *imageEngine) ImageRemove(_ context.Context, ref string, opts client.ImageRemoveOptions) (client.ImageRemoveResult, error) {
	f.removed, f.options = ref, opts
	return client.ImageRemoveResult{}, f.removeErr
}

func TestImageDetailsIncludesRunningAndStoppedByImageID(t *testing.T) {
	f := &imageEngine{info: image.InspectResponse{ID: "image-id", RepoTags: []string{"web:latest"}}, containers: []container.Summary{
		{ID: "running", ImageID: "image-id", State: container.StateRunning},
		{ID: "stopped", ImageID: "image-id", State: container.StateExited},
		{ID: "different-image", ImageID: "old-image", Image: "web:latest"},
	}}
	c := &Client{api: f}
	info, err := c.ImageDetails(context.Background(), "image-id")
	if err != nil || !f.all || len(info.Containers) != 2 || info.Image.ID != "image-id" {
		t.Fatalf("image details = %+v, %v; all=%v", info, err, f.all)
	}
	f.listErr = errors.New("usage unavailable")
	if _, err := c.ImageDetails(context.Background(), "image-id"); !errors.Is(err, f.listErr) {
		t.Fatal("usage failure was presented as unused")
	}
	f.inspectErr = errors.New("not found")
	if _, err := c.ImageDetails(context.Background(), "image-id"); !errors.Is(err, f.inspectErr) {
		t.Fatal("inspect failure was lost")
	}
}

func TestImageRemovalRechecksIdentityEffectAndUsage(t *testing.T) {
	for _, tt := range []struct {
		name    string
		info    image.InspectResponse
		request ImageRemoval
		used    bool
		wantOK  bool
	}{
		{"one of multiple tags", image.InspectResponse{ID: "id", RepoTags: []string{"web:a", "web:b"}}, ImageRemoval{"id", "web:a", true}, true, true},
		{"last tag unused", image.InspectResponse{ID: "id", RepoTags: []string{"web:a"}}, ImageRemoval{"id", "web:a", false}, false, true},
		{"untagged unused", image.InspectResponse{ID: "id"}, ImageRemoval{"id", "id", false}, false, true},
		{"last tag used by stopped container", image.InspectResponse{ID: "id", RepoTags: []string{"web:a"}}, ImageRemoval{"id", "web:a", false}, true, false},
		{"untagged used", image.InspectResponse{ID: "id"}, ImageRemoval{"id", "id", false}, true, false},
		{"tag moved", image.InspectResponse{ID: "different", RepoTags: []string{"web:a"}}, ImageRemoval{"id", "web:a", false}, false, false},
		{"tag became last", image.InspectResponse{ID: "id", RepoTags: []string{"web:a"}}, ImageRemoval{"id", "web:a", true}, false, false},
		{"last tag gained another tag", image.InspectResponse{ID: "id", RepoTags: []string{"web:a", "web:b"}}, ImageRemoval{"id", "web:a", false}, false, false},
		{"untagged gained tag", image.InspectResponse{ID: "id", RepoTags: []string{"web:a"}}, ImageRemoval{"id", "id", false}, false, false},
		{"selected tag missing", image.InspectResponse{ID: "id", RepoTags: []string{"web:b", "web:c"}}, ImageRemoval{"id", "web:a", true}, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &imageEngine{info: tt.info}
			if tt.used {
				f.containers = []container.Summary{{ImageID: "id", State: container.StateExited}}
			}
			err := (&Client{api: f}).RemoveImage(context.Background(), tt.request)
			if (err == nil) != tt.wantOK {
				t.Fatalf("RemoveImage error = %v", err)
			}
			if tt.wantOK {
				if f.removed != tt.request.Reference || f.options.Force || f.options.PruneChildren {
					t.Fatalf("unexpected remove request: %q %+v", f.removed, f.options)
				}
				if !tt.request.UntagOnly && !f.all {
					t.Fatal("deletion did not check stopped containers")
				}
			} else if f.removed != "" {
				t.Fatal("invalid removal reached daemon")
			}
		})
	}
}

func TestImageRemovalPropagatesDaemonFailures(t *testing.T) {
	for _, stage := range []string{"inspect", "list", "remove"} {
		t.Run(stage, func(t *testing.T) {
			failure := errors.New("daemon failure")
			f := &imageEngine{info: image.InspectResponse{ID: "id"}}
			switch stage {
			case "inspect":
				f.inspectErr = failure
			case "list":
				f.listErr = failure
			case "remove":
				f.removeErr = failure
			}
			if err := (&Client{api: f}).RemoveImage(context.Background(), ImageRemoval{ID: "id", Reference: "id"}); !errors.Is(err, failure) {
				t.Fatalf("failure lost: %v", err)
			}
		})
	}
}
