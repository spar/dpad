package render

import (
	"net/netip"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"
)

func TestPorts(t *testing.T) {
	ports := []container.PortSummary{
		{IP: netip.MustParseAddr("0.0.0.0"), PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
		{IP: netip.MustParseAddr("::"), PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
		{PrivatePort: 443, Type: "tcp"},
		{PrivatePort: 53, PublicPort: 5353, Type: "udp"},
	}
	if got, want := Ports(ports), "5353→53/udp, 8080→80/tcp, 443/tcp"; got != want {
		t.Errorf("Ports = %q, want %q", got, want)
	}
}

func TestExitCode(t *testing.T) {
	for status, want := range map[string]int{
		"Exited (137) 2 hours ago": 137,
		"Exited (0) 1 second ago":  0,
		"Up 3 minutes":             0,
	} {
		if got := ExitCode(status); got != want {
			t.Errorf("ExitCode(%q) = %d, want %d", status, got, want)
		}
	}
}

func TestSplitRepoTag(t *testing.T) {
	for in, want := range map[string][2]string{
		"nginx:latest":            {"nginx", "latest"},
		"registry:5000/app:v1.2":  {"registry:5000/app", "v1.2"},
		"registry:5000/app":       {"registry:5000/app", "<none>"},
		"ghcr.io/org/tool:1-slim": {"ghcr.io/org/tool", "1-slim"},
	} {
		repo, tag := SplitRepoTag(in)
		if repo != want[0] || tag != want[1] {
			t.Errorf("SplitRepoTag(%q) = %q, %q; want %q, %q", in, repo, tag, want[0], want[1])
		}
	}
}

func TestDetectLevel(t *testing.T) {
	tests := []struct {
		line  string
		want  Level
		token string
	}{
		{`{"level":"error","msg":"boom"}`, LevelError, "error"},
		{`time=now level=warn msg=slow`, LevelWarn, "warn"},
		{`2026/11/10 [error] 12#12: connect() failed`, LevelError, "error"},
		{`INFO server started`, LevelInfo, "INFO"},
		{`[DEBUG] cache miss`, LevelDebug, "DEBUG"},
		{`no errors here`, LevelNone, ""},
		{`GET /information 200`, LevelNone, ""},
	}
	for _, tt := range tests {
		lvl, s, e := DetectLevel(tt.line)
		token := ""
		if s >= 0 {
			token = tt.line[s:e]
		}
		if lvl != tt.want || token != tt.token {
			t.Errorf("DetectLevel(%q) = %v %q, want %v %q", tt.line, lvl, token, tt.want, tt.token)
		}
	}
}

func TestHighlightKeepsText(t *testing.T) {
	line := "ERROR failed to connect to db (ünïcode)"
	out := highlight(line, regexp.MustCompile("db|ü"))
	if got := ansi.Strip(out); got != line {
		t.Errorf("highlight changed text: %q", got)
	}
}

func TestTableAlignment(t *testing.T) {
	tbl := Table{
		Headers:  []string{"NAME", "SIZE"},
		Rows:     [][]string{{Bold.Render("a"), "1MB"}, {"longer-name", "100MB"}},
		Align:    []Align{Left, Right},
		Selected: -1,
	}
	lines := strings.Split(ansi.Strip(tbl.String()), "\n")
	want := []string{
		"NAME           SIZE",
		"a               1MB",
		"longer-name   100MB",
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}
