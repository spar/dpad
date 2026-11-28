package render

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/volume"

	"github.com/spar/dpad/internal/docker"
)

func TestContainerDetails(t *testing.T) {
	port := network.MustParsePort("80/tcp")
	c := container.InspectResponse{
		ID: "web-id", Name: "/web", RestartCount: 3,
		State: &container.State{Status: "exited", ExitCode: 137, OOMKilled: true, Error: "out of memory", StartedAt: "2026-11-12T00:00:00Z", FinishedAt: "0001-01-01T00:00:00Z",
			Health: &container.Health{Status: container.Unhealthy, FailingStreak: 2, Log: []*container.HealthcheckResult{
				nil, {Start: time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC), ExitCode: 1, Output: "connection refused\nretry failed\n"},
			}},
		},
		Config: &container.Config{Image: "nginx:alpine", Entrypoint: []string{"/entrypoint.sh"}, Cmd: []string{"sh", "-c", "echo hello world"},
			WorkingDir: "/app", User: "1000", Env: []string{"SECRET=do-not-display"}},
		HostConfig: &container.HostConfig{RestartPolicy: container.RestartPolicy{Name: "on-failure", MaximumRetryCount: 5}},
		NetworkSettings: &container.NetworkSettings{
			Ports: network.PortMap{port: {{HostIP: netip.MustParseAddr("::"), HostPort: "8080"}}},
			Networks: map[string]*network.EndpointSettings{
				"z-network": nil,
				"a-network": {IPAddress: netip.MustParseAddr("172.18.0.2"), GlobalIPv6Address: netip.MustParseAddr("fd00::2")},
			},
		},
		Mounts: []container.MountPoint{
			{Type: mount.TypeBind, Source: "/host/config", Destination: "/etc/config", RW: false},
			{Type: mount.TypeVolume, Name: "data", Source: "/var/lib/docker/volumes/data", Destination: "/data", RW: true},
		},
	}
	got := ansi.Strip(ContainerDetails(c))
	for _, want := range []string{
		"Name:  web", "web-id", "exited", "Exit code:  137", "OOM killed:  true", "out of memory", "Restarts:  3",
		"Started:  2026-11-12T00:00:00Z", "Finished:  —",
		"nginx:alpine", `"/entrypoint.sh"`, `"sh" "-c" "echo hello world"`, "/app", "1000", "on-failure (max retries: 5)",
		"unhealthy", "Failing streak:  2", "2026-11-12T00:00:00Z", "exit 1", "connection refused\n  retry failed",
		"[::]:8080 → 80/tcp", "a-network  172.18.0.2  fd00::2", "z-network",
		"/host/config → /etc/config (ro)", "data → /data (rw)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("details missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "SECRET") || strings.Contains(got, "do-not-display") {
		t.Error("details exposed environment variables")
	}
	if strings.Index(got, "a-network") > strings.Index(got, "z-network") {
		t.Error("networks are not sorted")
	}
}

func TestContainerDetailsIncomplete(t *testing.T) {
	got := ansi.Strip(ContainerDetails(container.InspectResponse{}))
	for _, want := range []string{"no health check", "Ports\nnone", "Networks\nnone", "Mounts\nnone"} {
		if !strings.Contains(got, want) {
			t.Errorf("empty details missing %q: %s", want, got)
		}
	}
	c := container.InspectResponse{Config: &container.Config{
		ExposedPorts: network.PortSet{network.MustParsePort("443/tcp"): {}},
	}}
	if got := ContainerDetails(c); !strings.Contains(got, "443/tcp (not published)") {
		t.Errorf("created container's exposed port missing: %s", got)
	}
}

func TestEmptyResourceDetailsFormatting(t *testing.T) {
	for _, tc := range []struct {
		name, output, want string
	}{
		{"container", ContainerDetails(container.InspectResponse{}), "Name:  —\nID:  —\nCreated:  —"},
		{"image", ImageDetails(docker.ImageInfo{}), "ID:  —\nCreated:  —\nOS:  —\nArchitecture:  —"},
		{"volume", VolumeDetails(volume.Volume{}, nil, true), "Name:  —\nDriver:  —\nScope:  —"},
		{"network", NetworkDetails(network.Inspect{}), "Name:  —\nID:  —\nDriver:  —\nScope:  —"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := ansi.Strip(tc.output)
			if !strings.HasPrefix(out, tc.want) {
				t.Fatalf("missing fields changed formatting:\n%s", out)
			}
			if tc.name != "container" && !strings.Contains(out, "\n\nLabels\nnone\n\n") {
				t.Fatalf("empty labels changed section spacing:\n%s", out)
			}
			if strings.HasSuffix(out, "\n") {
				t.Fatal("details have a trailing newline")
			}
		})
	}
}
