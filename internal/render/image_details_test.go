package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"dockernext/internal/docker"
)

func TestImageDetailsMetadataAndUsage(t *testing.T) {
	info := docker.ImageInfo{Image: image.InspectResponse{ID: "sha256:image", Created: "2026-11-12T00:00:00Z", Os: "linux", Architecture: "arm64", Variant: "v8", Size: 1024,
		RepoTags: []string{"web:z", "web:a"}, RepoDigests: []string{"web@sha256:digest"},
		Config: &dockerspec.DockerOCIImageConfig{ImageConfig: ocispec.ImageConfig{Entrypoint: []string{"/start"}, Cmd: []string{"serve"}, User: "1000", WorkingDir: "/app", Labels: map[string]string{"z": "last", "a": "first"}, Env: []string{"SECRET=must-not-appear"}}},
	}, Containers: []container.Summary{{ID: "z-id", Names: []string{"/z-stopped"}, State: container.StateExited}, {ID: "a-id", Names: []string{"/a-running"}, State: container.StateRunning}}}
	out := ansi.Strip(ImageDetails(info))
	for _, want := range []string{"sha256:image", "linux", "arm64", "v8", "web:a", "web@sha256:digest", "/start", "serve", "1000", "/app", "a=first", "1 running / 2 total", "a-running", "z-stopped", "exited"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if strings.Contains(out, "must-not-appear") {
		t.Fatal("environment variables leaked")
	}
	if strings.Index(out, "web:a") > strings.Index(out, "web:z") || strings.Index(out, "a=first") > strings.Index(out, "z=last") || strings.Index(out, "a-running") > strings.Index(out, "z-stopped") {
		t.Fatal("details output is not deterministically sorted")
	}
	if info.Image.RepoTags[0] != "web:z" {
		t.Fatal("render mutated source tags")
	}
	if out := ansi.Strip(ImageDetails(docker.ImageInfo{})); !strings.Contains(out, "unused") || !strings.Contains(out, "none") {
		t.Fatal("empty metadata was not handled")
	}
}
