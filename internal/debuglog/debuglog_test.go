package debuglog

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOpenWritesAPrivateLog(t *testing.T) {
	Info("before open")
	path := filepath.Join(t.TempDir(), "dp.log")
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !On() {
		t.Fatal("On() is false after Open")
	}
	Info("api", "path", "/containers/json", "ms", 3)
	Warn("stream ended", "err", "EOF")
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if On() {
		t.Fatal("On() is true after Close")
	}
	Info("after close")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"msg=api path=/containers/json ms=3", "level=WARN msg=\"stream ended\" err=EOF"} {
		if !strings.Contains(got, want) {
			t.Errorf("log %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "before open") || strings.Contains(got, "after close") {
		t.Error("a line logged while the log was off was written")
	}
	if fi, err := os.Stat(path); err == nil && runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("log mode %v, want 0600", fi.Mode().Perm())
	}
}
