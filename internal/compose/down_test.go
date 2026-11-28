package compose

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestDownRunsByProjectNameAwayFromTheCurrentFolder(t *testing.T) {
	_, _, log := composeFixture(t)
	t.Setenv("DP_COMPOSE_STANDALONE", "1")
	cwd, _ := os.Getwd()
	env := append(os.Environ(), "COMPOSE_FILE=/elsewhere/compose.yaml")
	out, err := Down(t.Context(), env, "old-wiki", true)
	if err != nil || !strings.Contains(out, "old-wiki") {
		t.Fatalf("Down = %q, %v", out, err)
	}
	all := calls(t, log)
	last := all[len(all)-1]
	if !slices.Equal(last.Args, DownArgs("old-wiki", true)) || last.Host != "tcp://bound-daemon:2375" {
		t.Fatalf("call = %+v", last)
	}
	if last.Directory == cwd {
		t.Fatal("down ran in the current folder, where Compose could load another project's files")
	}
	if _, err := os.Stat(last.Directory); !os.IsNotExist(err) {
		t.Fatalf("temporary folder %s was left behind", last.Directory)
	}
	if !slices.Contains(DownArgs("x", false), "down") || slices.Contains(DownArgs("x", false), "--volumes") {
		t.Fatal("volumes are kept unless asked for")
	}
}

func TestDownRunsOnTheSSHHostWithoutALocalCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses shell scripts as docker and ssh")
	}
	_, _, log := composeFixture(t)
	remote := filepath.SplitList(os.Getenv("PATH"))[0]
	local := t.TempDir()
	script := "#!/bin/sh\nPATH=" + quote(remote+string(os.PathListSeparator)+"/usr/bin:/bin") + " exec " + quote(filepath.Join(remote, "ssh")) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(local, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", local)
	t.Setenv("DOCKER_HOST", "ssh://me@server/run/user/1000/docker.sock")
	t.Setenv("COMPOSE_FILE", "/elsewhere/compose.yaml")
	out, err := Down(t.Context(), os.Environ(), "old-wiki", false)
	if err != nil || !strings.Contains(out, "old-wiki") {
		t.Fatalf("Down = %q, %v", out, err)
	}
	all := calls(t, log)
	last := all[len(all)-1]
	if !slices.Equal(last.Args, append([]string{"compose"}, DownArgs("old-wiki", false)...)) || last.Host != "unix:///run/user/1000/docker.sock" || last.Directory != "/" {
		t.Fatalf("call = %+v", last)
	}
}
