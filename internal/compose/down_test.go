package compose

import (
	"os"
	"slices"
	"testing"
)

func TestDownRunsByProjectNameAwayFromTheCurrentFolder(t *testing.T) {
	_, _, log := composeFixture(t)
	t.Setenv("DP_COMPOSE_STANDALONE", "1")
	cwd, _ := os.Getwd()
	env := append(os.Environ(), "COMPOSE_FILE=/elsewhere/compose.yaml")
	if _, err := Down(t.Context(), env, "old-wiki", true); err != nil {
		t.Fatal(err)
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
