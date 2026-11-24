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

	"dpad/internal/docker"
)

func TestPorts(t *testing.T) {
	ports := []container.PortSummary{
		{IP: netip.MustParseAddr("0.0.0.0"), PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
		{IP: netip.MustParseAddr("::"), PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
		{PrivatePort: 443, Type: "tcp"},
		{PrivatePort: 53, PublicPort: 5353, Type: "udp"},
		{IP: netip.MustParseAddr("127.0.0.1"), PrivatePort: 5432, PublicPort: 5432, Type: "tcp"},
		{IP: netip.MustParseAddr("::1"), PrivatePort: 6379, PublicPort: 6379, Type: "tcp"},
	}
	if got, want := Ports(ports), "5353→53/udp, 8080→80/tcp, 443/tcp, 127.0.0.1:5432→5432/tcp, [::1]:6379→6379/tcp"; got != want {
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

func TestStatsRatesDistinguishUnknownIdleAndTotals(t *testing.T) {
	s := docker.Stats{Name: "web", NetRx: 1000, NetTx: 2000, BlockRead: 3000, BlockWrite: 4000}
	if row := StatsRateRow(s, docker.Rates{}); row[6] != "— / —" || row[7] != "— / —" {
		t.Fatal("unavailable throughput was shown as zero")
	}
	if row := StatsRateRow(s, docker.Rates{NetValid: true, BlockValid: true}); row[6] != "0B/s / 0B/s" || row[7] != "0B/s / 0B/s" {
		t.Fatalf("idle throughput was not measured zero: %v", row)
	}
	if row := StatsRateRow(s, docker.Rates{NetRx: 1500, NetTx: 2500, NetValid: true}); !strings.Contains(row[6], "kB/s") || row[7] != "— / —" {
		t.Fatalf("rate units/availability incorrect: %v", row)
	}
	if row := StatsRow(s); strings.Contains(row[6], "/s") || row[6] == "— / —" {
		t.Fatal("snapshot/totals rendering changed")
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

func TestWidthMatchesAnsi(t *testing.T) {
	for _, s := range []string{
		"", "plain", Bold.Render("bold"), fg(Cyan).Render(""), Dim.Render("0123456789ab") + "  x",
		"8080→80/tcp", Bar(42, 10), HealthBadge("healthy"), "\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\",
		"tab\there", "日本語", "e\u0301",
	} {
		if got, want := width(s), ansi.StringWidth(s); got != want {
			t.Errorf("width(%q) = %d, want %d", s, got, want)
		}
	}
}

func TestPreparedTableMatchesFreshAcrossViewports(t *testing.T) {
	t1 := Table{Columns: []Column{{Header: "NAME", Max: 12}, {Header: "CPU", Align: Right}}, Rows: [][]string{{Bold.Render("alpha"), Bar(40, 10)}, {"long-name-off-screen", Bar(90, 10)}, {"東京", "1"}, {"♥️ combine\u0301", "2"}}, Width: 40}
	prepared := t1.Prepare()
	for _, w := range []int{20, 40, 80} {
		t1.Width = w
		prepared.Width = w
		for _, selected := range []int{-1, 0, 2, 3} {
			for _, height := range []int{2, 4, 10} {
				if got, want := prepared.Window(selected, height), t1.Window(selected, height); got != want {
					t.Fatalf("width=%d selection=%d height=%d\n%s\n%s", w, selected, height, got, want)
				}
			}
		}
	}
	for _, s := range []string{"▾ project ▲ · — →", Bar(100, 10), "♥️", "▸\u0301", "→\u200d🙂", "✗ unhealthy"} {
		if got, want := width(s), ansi.StringWidth(s); got != want {
			t.Fatalf("width(%q)=%d want %d", s, got, want)
		}
	}
	t1.Columns[0].Header = "changed"
	if strings.Contains(ansi.Strip(prepared.String()), "changed") {
		t.Fatal("prepared table retained mutable column headers")
	}
}

func TestPrintableASCIITruncationMatchesANSI(t *testing.T) {
	for _, s := range []string{"", "short", strings.Repeat("f", 64), "a b!c~d", "\x1b[31mred text\x1b[0m", "hello\tworld", "你好世界", "🙂🙂🙂", "e\u0301long", "♥️ test"} {
		for _, limit := range []int{0, 1, 2, 5, 40, 80} {
			if got, want := truncate(s, limit), ansi.Truncate(s, limit, "…"); got != want {
				t.Fatalf("truncate(%q,%d)=%q want %q", s, limit, got, want)
			}
		}
	}
}

func TestPreparedViewportStart(t *testing.T) {
	tbl := (Table{Columns: []Column{{Header: "NAME"}}, Rows: [][]string{{"one"}, {"two"}, {"three"}, {"four"}}}).Prepare()
	if got := ansi.Strip(tbl.WindowFrom(2, 3, 1)); got != "NAME\ntwo\nthree" {
		t.Fatal(got)
	}
	if got := ansi.Strip(tbl.WindowFrom(-1, 3, -10)); got != "NAME\none\ntwo" {
		t.Fatal(got)
	}
	if got := ansi.Strip(tbl.WindowFrom(-1, 3, 99)); got != "NAME" {
		t.Fatal(got)
	}
}

func TestStatsSnapshotTablePlainDropsBars(t *testing.T) {
	items := []docker.Stats{{Name: "web", CPUPercent: 42, MemUsage: 1 << 20, MemLimit: 4 << 20, MemPercent: 25, PIDs: 3}}
	if got := ansi.Strip(StatsSnapshotTable(items, false).String()); !strings.Contains(got, "░") {
		t.Fatalf("terminal output lost its bars:\n%s", got)
	}
	got := ansi.Strip(StatsSnapshotTable(items, true).String())
	if strings.ContainsAny(got, "█░") {
		t.Fatalf("plain output kept bars:\n%s", got)
	}
}
