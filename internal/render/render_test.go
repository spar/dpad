package render

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
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

func TestImageSummary(t *testing.T) {
	items := []image.Summary{{Size: 1_000_000, RepoTags: []string{"a:1"}}, {Size: 500_000}}
	if got, want := ImageSummary(items), "2 images · 1.5MB total · 1 dangling"; got != want {
		t.Errorf("ImageSummary = %q, want %q", got, want)
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
		Columns: []Column{{Header: "NAME"}, {Header: "SIZE", Align: Right}},
		Rows:    [][]string{{Bold.Render("a"), "1MB"}, {"longer-name", "100MB"}},
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

func TestTableWindow(t *testing.T) {
	tbl := Table{Columns: []Column{{Header: "N"}}}
	for _, r := range []string{"r0", "r1", "r2", "r3", "r4"} {
		tbl.Rows = append(tbl.Rows, []string{r})
	}
	for _, tt := range []struct {
		selected, height int
		want             string
	}{
		{-1, 3, "N|r0|r1"},
		{1, 3, "N|r0|r1"},
		{4, 3, "N|r3|r4"},
		{2, 10, "N|r0|r1|r2|r3|r4"},
	} {
		got := strings.ReplaceAll(ansi.Strip(tbl.Window(tt.selected, tt.height)), "\n", "|")
		if got != tt.want {
			t.Errorf("Window(%d, %d) = %q, want %q", tt.selected, tt.height, got, tt.want)
		}
	}
}

func BenchmarkTableWindow(b *testing.B) {
	tbl := Table{Columns: ContainerColumns, Width: 160}
	for i := range 200 {
		tbl.Rows = append(tbl.Rows, []string{
			Dim.Render("0123456789ab"), Bold.Render(fmt.Sprintf("service-%d", i)), "nginx:1.27-alpine",
			fg(Green).Render("running"), fg(Green).Render("Up 3 hours"), HealthBadge("healthy"), "8080→80/tcp",
		})
	}
	for b.Loop() {
		_ = tbl.Window(100, 40)
	}
}

func TestTableTrimsStyledEmptyCells(t *testing.T) {
	tbl := Table{
		Columns: []Column{{Header: "NAME"}, {Header: "PORTS"}},
		Rows:    [][]string{{"web", fg(Cyan).Render("")}},
	}
	if got := ansi.Strip(strings.Split(tbl.String(), "\n")[1]); got != "web" {
		t.Errorf("row = %q, want trailing padding trimmed", got)
	}
}
