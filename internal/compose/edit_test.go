package compose

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
)

func TestCheckUsesACandidateBesideTheOriginalAndLeavesItAlone(t *testing.T) {
	dir, _, log := composeFixture(t)
	p := Prepare(t.Context(), os.Environ(), Discover([]container.Summary{labeledProject("demo", dir)}, ""), nil)[0]
	file := filepath.Join(dir, "compose.yaml")
	if _, err := Check(t.Context(), os.Environ(), p, file, []byte("edited")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(file)
	if string(b) != "fixture" {
		t.Fatal("check changed the original file")
	}
	last := calls(t, log)
	args := last[len(last)-1].Args
	candidate := args[slices.Index(args, "--file")+1]
	if filepath.Dir(candidate) != dir || candidate == file || !slices.Contains(args, filepath.Join(dir, "override.yaml")) {
		t.Fatal("candidate did not stand in for the edited file only", args)
	}
	if _, err := os.Stat(candidate); !os.IsNotExist(err) {
		t.Fatal("candidate file left behind")
	}
	if _, err := Check(t.Context(), os.Environ(), p, filepath.Join(dir, "other.yaml"), nil); err == nil {
		t.Fatal("a file outside the project was accepted")
	}
}

func TestReplaceKeepsPermissionsFollowsSymlinksAndRefusesConcurrentChanges(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.yaml")
	link := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(real, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	p := Project{Directory: dir, Files: []string{link}, CLI: []string{"docker", "compose"}}
	if err := Replace(t.Context(), nil, p, link, []byte("someone else's"), []byte("new")); err == nil || !strings.Contains(err.Error(), "changed on disk") {
		t.Fatal("overwrote a file that changed during the edit", err)
	}
	if err := Replace(t.Context(), nil, p, link, []byte("old"), []byte("new")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(real)
	info, _ := os.Stat(real)
	if string(b) != "new" || info.Mode().Perm() != 0o640 {
		t.Fatalf("content %q mode %v", b, info.Mode())
	}
	if l, err := os.Lstat(link); err != nil || l.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink replaced by a regular file")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatal("temporary file left behind", entries)
	}
}

func TestChangesNamesImageAndServiceChanges(t *testing.T) {
	before := Project{Services: []string{"db", "old", "web"}, ServiceImages: map[string]string{"db": "postgres:16", "old": "old:1", "web": "nginx:1.25"}}
	after := Project{Services: []string{"cache", "db", "web"}, ServiceImages: map[string]string{"cache": "redis:7", "db": "postgres:16", "web": "nginx:1.27"}}
	got := Changes(before, after)
	want := []string{"cache: added (redis:7)", "web: nginx:1.25 → nginx:1.27", "old: removed"}
	if !slices.Equal(got, want) {
		t.Fatal(got)
	}
	same := Changes(before, before)
	after = before
	after.ConfigHash[0] = 1
	if !strings.Contains(same[0], "same") || !strings.Contains(Changes(before, after)[0], "settings change") {
		t.Fatal(same, Changes(before, after))
	}
}

func TestCheckWithInstalledCompose(t *testing.T) {
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("Docker CLI unavailable")
	}
	if err := exec.Command(docker, "compose", "version", "--short").Run(); err != nil {
		t.Skip("Compose plugin unavailable")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(file, []byte("services:\n  web:\n    image: nginx:1.25\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_HOST", "unix:///nonexistent-compose-edit-test.sock")
	t.Setenv("DOCKER_CONTEXT", "")
	item := labeledProject("edit-test", dir)
	item.Labels[projectLabel+".config_files"] = file
	delete(item.Labels, projectLabel+".environment_file")
	p := Prepare(t.Context(), os.Environ(), Discover([]container.Summary{item}, ""), nil)[0]
	if p.Issue != "" {
		t.Fatal(p.Issue)
	}
	if _, err := Check(t.Context(), os.Environ(), p, file, []byte("services:\n  web:\n    image: [\n")); err == nil {
		t.Fatal("invalid YAML accepted")
	}
	edited, err := Check(t.Context(), os.Environ(), p, file, []byte("services:\n  web:\n    image: nginx:1.27\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := Changes(p, edited); !slices.Equal(got, []string{"web: nginx:1.25 → nginx:1.27"}) {
		t.Fatal(got)
	}
}

func TestRemoteEditReadsChecksAndReplacesOverSSH(t *testing.T) {
	dir, _, log := composeFixture(t)
	t.Setenv("DOCKER_HOST", "ssh://me@server/run/user/1000/docker.sock")
	real := filepath.Join(dir, "real.yaml")
	file := filepath.Join(dir, "compose.yaml")
	if err := os.Rename(file, real); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(real, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, file); err != nil {
		t.Skip("symlinks unavailable")
	}
	projects := Discover([]container.Summary{labeledProject("demo", dir)}, "")
	projects[0].Location = "ssh"
	p := Prepare(t.Context(), os.Environ(), projects, nil)[0]
	if !p.Remote || p.Issue != "" {
		t.Fatal(p)
	}
	original, err := ReadSource(t.Context(), os.Environ(), p, file)
	if err != nil || string(original) != "fixture" {
		t.Fatalf("read %q, %v", original, err)
	}
	if _, err := Check(t.Context(), os.Environ(), p, file, []byte("edited")); err != nil {
		t.Fatal(err)
	}
	last := calls(t, log)
	args := last[len(last)-1].Args
	if candidate := args[slices.Index(args, "--file")+1]; filepath.Dir(candidate) != dir || candidate == file {
		t.Fatal("remote check did not use a copy beside the file", args)
	}
	if err := Replace(t.Context(), os.Environ(), p, file, []byte("stale"), []byte("new")); err == nil || !strings.Contains(err.Error(), "changed on disk") {
		t.Fatal("remote save overwrote a concurrent change", err)
	}
	if err := Replace(t.Context(), os.Environ(), p, file, original, []byte("new\n$(not run)\n")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(real)
	info, _ := os.Stat(real)
	if string(b) != "new\n$(not run)\n" || info.Mode().Perm() != 0o640 {
		t.Fatalf("remote save wrote %q with mode %v", b, info.Mode())
	}
	if l, err := os.Lstat(file); err != nil || l.Mode()&os.ModeSymlink == 0 {
		t.Fatal("remote save replaced the symlink")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".dpad-") {
			t.Fatal("temporary file left on the SSH host", e.Name())
		}
	}
}
