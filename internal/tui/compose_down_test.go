package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/volume"
)

func downClient() *fakeClient {
	label := func(project string) map[string]string { return map[string]string{composeProjectLabel: project} }
	return &fakeClient{
		containers: []container.Summary{actionContainer("web", "wiki", container.StateExited), actionContainer("other", "shop", container.StateRunning)},
		networks:   []network.Summary{{Network: network.Network{ID: "net1", Name: "wiki_default", Labels: label("wiki")}}, {Network: network.Network{ID: "net2", Name: "bridge"}}},
		volumes:    []volume.Volume{{Name: "wiki_db", Labels: label("wiki")}, {Name: "shop_data", Labels: label("shop")}},
	}
}

func runDown(t *testing.T, v *composeDownView) {
	t.Helper()
	check := v.HandleKey("y")
	if check == nil {
		t.Fatal("y did not confirm")
	}
	next := v.Update(check())
	for next != nil {
		msg := next()
		if _, ok := msg.(refreshMsg); ok {
			return
		}
		next = v.Update(msg)
	}
}

func TestComposeDownPreviewsOnlyTheProjectsResources(t *testing.T) {
	c := downClient()
	v, load := newComposeDown(t.Context(), c, "wiki", 100, 20)
	defer v.Close()
	v.Update(load())
	view := ansi.Strip(v.View())
	for _, want := range []string{"1 container · 1 network · volumes kept", "wiki_default", "wiki_db", "keep", "remove"} {
		if !strings.Contains(view, want) {
			t.Fatalf("preview lacks %q:\n%s", want, view)
		}
	}
	for _, other := range []string{"shop_data", "bridge", "/other"} {
		if strings.Contains(view, other) {
			t.Fatalf("preview lists %q from outside the project", other)
		}
	}
	v.HandleKey("v")
	if view := ansi.Strip(v.View()); !strings.Contains(view, "1 volume will be deleted") || !strings.Contains(view, "--volumes") || !strings.Contains(ansi.Strip(v.Footer()), "keep volumes") {
		t.Fatalf("v should switch to deleting volumes:\n%s", view)
	}
}

func TestComposeDownReportsWhatIsGoneAfterwards(t *testing.T) {
	for _, volumes := range []bool{false, true} {
		c := downClient()
		v, load := newComposeDown(t.Context(), c, "wiki", 100, 20)
		var got []string
		v.down = func(_ context.Context, env []string, name string, withVolumes bool) (string, error) {
			got = append(got, name)
			c.containers, c.networks = c.containers[1:], c.networks[1:]
			if withVolumes {
				c.volumes = c.volumes[1:]
			}
			return "", nil
		}
		v.Update(load())
		if volumes {
			v.HandleKey("v")
		}
		runDown(t, v)
		counts := v.counts()
		wantKept := 1
		if volumes {
			wantKept = 0
		}
		if len(got) != 1 || got[0] != "wiki" || v.stage != stageDone || counts["removed"] != 3-wantKept || counts["kept"] != wantKept || counts["failed"] != 0 {
			t.Fatalf("volumes=%v: calls %v stage %s counts %v", volumes, got, v.stage, counts)
		}
		v.Close()
	}
}

func TestComposeDownFailureKeepsWhatRemains(t *testing.T) {
	c := downClient()
	v, load := newComposeDown(t.Context(), c, "wiki", 100, 20)
	defer v.Close()
	v.down = func(context.Context, []string, string, bool) (string, error) {
		return "Container wiki-web-1 Stopping\nError response from daemon: conflict", errors.New("exit status 1")
	}
	v.Update(load())
	runDown(t, v)
	if v.counts()["failed"] != 2 || !strings.Contains(v.note, "conflict") || !strings.Contains(v.note, "exit status 1") {
		t.Fatalf("counts %v note %q", v.counts(), v.note)
	}
}

func TestComposeDownAsksAgainWhenTheProjectChanged(t *testing.T) {
	c := downClient()
	v, load := newComposeDown(t.Context(), c, "wiki", 100, 20)
	defer v.Close()
	called := false
	v.down = func(context.Context, []string, string, bool) (string, error) { called = true; return "", nil }
	v.Update(load())
	c.containers = append(c.containers, actionContainer("new", "wiki", container.StateRunning))
	if v.Update(v.HandleKey("y")()) != nil || v.stage != stagePreview || called || !strings.Contains(v.note, "changed") {
		t.Fatalf("stale preview ran down: stage %s note %q", v.stage, v.note)
	}
}

func TestDOnProjectRowOpensDownAndOnContainerRowRemoves(t *testing.T) {
	a := newTestApp()
	a.client = downClient()
	a.tabs[3].Update(containersMsg{items: a.client.(*fakeClient).containers})
	press(a, "4")
	if p := a.tabs[3].(*ProjectsView).selectedProject(); p == nil {
		t.Fatal("expected a project row")
	}
	a.Update(key("D"))
	if a.composeDown == nil || a.confirm != nil {
		t.Fatal("D on a project row should open the down preview")
	}
	a.Update(key("esc"))
	press(a, "j")
	a.Update(key("D"))
	if a.composeDown != nil || a.confirm == nil || a.confirm.verb != "remove" {
		t.Fatal("D on a container row should still ask to remove the container")
	}
}

func TestProjectInfoStripShowsWhereTheComposeFileIs(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "compose.yaml")
	for _, f := range []string{file, filepath.Join(dir, "override.yaml")} {
		if err := os.WriteFile(f, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	item := func(project, files string) container.Summary {
		c := actionContainer(project+"-web", project, container.StateRunning)
		c.Labels[composeProjectLabel+".config_files"] = files
		c.Labels[composeProjectLabel+".working_dir"] = filepath.Dir(strings.Split(files, ",")[0])
		c.Labels[composeProjectLabel+".environment_file"] = filepath.Join(dir, ".env")
		return c
	}
	items := []container.Summary{item("here", file+","+filepath.Join(dir, "override.yaml")), item("gone", "/no/such/dir/compose.yml")}
	for _, tt := range []struct {
		name, ssh, selected string
		local               bool
		want, not           []string
	}{
		{"local", "", "here", true, []string{"── here ", "file  ~/compose.yaml + override.yaml", "dir   ~ · env .env"}, []string{"not found"}},
		{"missing", "", "gone", true, []string{"/no/such/dir/compose.yml  ✗ not found", "D down still works"}, nil},
		{"ssh", "homelab", "gone", false, []string{"file  /no/such/dir/compose.yml", "host  homelab (paths are on this host)"}, []string{"not found", "~"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v := NewProjectsView()
			v.sshHost, v.localFiles, v.home = tt.ssh, tt.local, dir
			v.SetSize(100, 20)
			v.Update(containersMsg{items: items})
			v.cur.key = "project:" + tt.selected
			view := ansi.Strip(v.View())
			lines := strings.Split(view, "\n")
			if len(lines) != 20 {
				t.Fatalf("view has %d lines, want 20", len(lines))
			}
			for _, want := range tt.want {
				if !strings.Contains(view, want) {
					t.Errorf("strip lacks %q:\n%s", want, view)
				}
			}
			for _, not := range tt.not {
				if strings.Contains(strings.Join(lines[len(lines)-infoLines:], "\n"), not) {
					t.Errorf("strip has %q:\n%s", not, view)
				}
			}
		})
	}
}

func TestDaemonHost(t *testing.T) {
	for host, want := range map[string]struct {
		ssh   string
		local bool
	}{
		"ssh://spar@homelab:22": {"homelab", false}, "unix:///var/run/docker.sock": {"", true},
		"npipe:////./pipe/docker_engine": {"", true}, "tcp://10.0.0.5:2376": {"", false},
	} {
		if ssh, local := daemonHost([]string{"PATH=/bin", "DOCKER_HOST=" + host}); ssh != want.ssh || local != want.local {
			t.Errorf("%s: got %q %v", host, ssh, local)
		}
	}
}

func TestComposeDownStoppedMidwayMarksWhatIsLeftCancelled(t *testing.T) {
	c := downClient()
	v, load := newComposeDown(t.Context(), c, "wiki", 100, 20)
	defer v.Close()
	started := make(chan struct{})
	v.down = func(ctx context.Context, _ []string, _ string, _ bool) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}
	v.Update(load())
	next := v.Update(v.HandleKey("y")())
	<-started
	if dismiss(v) || v.stage != stageCancelling {
		t.Fatal("esc while running should stop the command, not close the view")
	}
	for next != nil {
		msg := next()
		if _, ok := msg.(refreshMsg); ok {
			break
		}
		next = v.Update(msg)
	}
	if counts := v.counts(); counts["cancelled"] != 2 || counts["kept"] != 1 || !strings.Contains(v.note, "Stopped") {
		t.Fatalf("counts %v note %q", counts, v.note)
	}
}
