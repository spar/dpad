package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/system"

	"dpad/internal/config"
	"dpad/internal/docker"
)

func testSystemInfo() *docker.SystemInfo {
	return &docker.SystemInfo{Info: system.Info{
		Name: "homelab", OperatingSystem: "Ubuntu 24.04.1 LTS", KernelVersion: "6.8.0-49-generic", Architecture: "x86_64",
		NCPU: 8, MemTotal: 16 << 30, ServerVersion: "28.5.1", Driver: "overlay2", DockerRootDir: "/var/lib/docker",
		Warnings: []string{"No swap limit support"},
	}, APIVersion: "1.51", Components: map[string]string{}}
}

// systemApp is an app on a connected daemon, at 80x24, with /info loaded.
func systemApp(t *testing.T, fc *fakeClient) *App {
	t.Helper()
	t.Setenv("DPAD_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	if fc.systemInfo == nil {
		fc.systemInfo = testSystemInfo()
	}
	a := newApp(context.Background(), fc)
	a.daemon.connected, a.daemon.everConnected = true, true
	a.resize(80, 24)
	a.Update(containersMsg{items: []container.Summary{{ID: "a", Names: []string{"/web"}, State: container.StateRunning, Status: "Up 2 hours"}}})
	run(a, a.loadSystemInfo())
	return a
}

// run feeds cmd's messages back into the app, following batches, until
// nothing more is ready. Ticks and blocked reads are skipped.
func run(a *App, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(50 * time.Millisecond):
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			run(a, c)
		}
		return
	}
	if msg != nil {
		_, next := a.Update(msg)
		run(a, next)
	}
}

func hostSample(at time.Time, busy, total uint64) docker.HostSample {
	return docker.HostSample{At: at, CPUBusy: busy, CPUTotal: total, MemTotal: 16 << 30, MemAvailable: 6 << 30,
		Load: [3]float64{1.42, 1.37, 1.29}, Uptime: 41*24*time.Hour + 2*time.Hour, DiskSize: 200 << 30, DiskUsed: 120 << 30}
}

func TestHostPanelShowsTheHostAboveContainers(t *testing.T) {
	host := make(chan docker.HostUpdate, 2)
	now := time.Now()
	host <- docker.HostUpdate{Sample: hostSample(now.Add(-2*time.Second), 1000, 10000)}
	host <- docker.HostUpdate{Sample: hostSample(now, 1500, 11600)}
	a := systemApp(t, &fakeClient{host: host})
	for range 2 {
		run(a, nextHostUpdate(a.system.hostGen, host))
	}
	screen := ansi.Strip(a.render())
	lines := strings.Split(screen, "\n")
	if lines[2] != "" || !strings.HasPrefix(lines[3], "  System  homelab") || !strings.HasPrefix(lines[5], "  Host    Ubuntu 24.04") || lines[6] != "" {
		t.Fatalf("panel rows:\n%s", screen)
	}
	for _, want := range []string{"31% of 8 cores", "10.0G/16.0G", "Load 1.42 1.37 1.29", "41d 2h", "v28.5.1"} {
		if !strings.Contains(screen, want) {
			t.Errorf("panel lacks %q:\n%s", want, screen)
		}
	}
	// The CPU and MEM columns line up from row to row.
	col := func(line, sub string) int { return ansi.StringWidth(line[:strings.Index(line, sub)]) }
	if col(lines[3], "CPU") != col(lines[5], "Load") || col(lines[3], "MEM") != col(lines[5], "Up") {
		t.Errorf("columns don't line up:\n%s\n%s", lines[3], lines[5])
	}
	if len(lines) != 24 || !strings.Contains(lines[7], "NAME") {
		t.Fatalf("the list should start under the panel and the screen stay 24 rows:\n%s", screen)
	}
	if got := a.containers().height; got != a.bodyHeight()+1-panelRows {
		t.Errorf("containers height %d, want %d", got, a.bodyHeight()+1-panelRows)
	}

	// Other tabs keep the daemon line.
	press(a, "2")
	if line := ansi.Strip(strings.Split(a.render(), "\n")[2]); !strings.HasPrefix(line, "daemon:") {
		t.Errorf("Images tab header = %q", line)
	}
}

func TestHostPanelToggleIsSaved(t *testing.T) {
	a := systemApp(t, &fakeClient{})
	if !a.panelShown() {
		t.Fatal("panel should show on Containers")
	}
	press(a, "t")
	if a.panelShown() || a.containers().height != a.bodyHeight() {
		t.Fatal("t should hide the panel and give its rows back")
	}
	data, _ := os.ReadFile(os.Getenv("DPAD_CONFIG"))
	if !strings.Contains(string(data), "hostPanel: \"off\"") && !strings.Contains(string(data), "hostPanel: off") {
		t.Fatalf("config = %q", data)
	}
	press(a, "t")
	if !a.panelShown() {
		t.Fatal("t should show it again")
	}
	if data, _ := os.ReadFile(os.Getenv("DPAD_CONFIG")); strings.Contains(string(data), "hostPanel") {
		t.Fatalf("showing it should drop the setting: %q", data)
	}
}

func TestHostPanelStepsAsideWhenItDoesNotFit(t *testing.T) {
	a := systemApp(t, &fakeClient{})
	a.resize(79, 24)
	if a.panelShown() {
		t.Error("panel at 79 columns")
	}
	a.resize(80, 19)
	if a.panelShown() {
		t.Error("panel at 19 rows")
	}
	a.resize(80, 24)
	a.daemon.connected = false
	if a.panelShown() {
		t.Error("panel while disconnected hides the connection error")
	}
}

func TestHostPanelOnDockerDesktopShowsContainersOnly(t *testing.T) {
	info := testSystemInfo()
	info.Info.Name, info.Info.OperatingSystem = "docker-desktop", "Docker Desktop"
	a := systemApp(t, &fakeClient{systemInfo: info, host: make(chan docker.HostUpdate)})
	if a.system.watching {
		t.Fatal("Docker Desktop's VM has no /proc to read")
	}
	screen := ansi.Strip(a.render())
	if !strings.Contains(screen, "Docker Desktop VM · 8 cores · 16.0G · host usage n/a") || !strings.Contains(screen, "Desktop VM") {
		t.Fatalf("panel:\n%s", screen)
	}
}

func TestClickRowsUnderTheHostPanel(t *testing.T) {
	fc := &fakeClient{}
	a := systemApp(t, fc)
	a.Update(containersMsg{items: []container.Summary{
		{ID: "a", Names: []string{"/a"}, State: container.StateRunning},
		{ID: "b", Names: []string{"/b"}, State: container.StateRunning},
	}})
	a.render()
	// Rows: tab bar, rule, 5 panel rows, the list header, then a and b.
	a.Update(tea.MouseClickMsg{X: 2, Y: 9, Button: tea.MouseLeft})
	if id, _, _ := a.containers().Selected(); id != "b" {
		t.Fatalf("clicked row selected %q", id)
	}
}

func TestSystemScreen(t *testing.T) {
	fc := &fakeClient{diskUsage: docker.DiskUsage{Images: docker.DiskUsageKind{Count: 41, Active: 32, Size: 18_200_000_000, Reclaimable: 6_100_000_000}}}
	a := systemApp(t, fc)
	_, cmd := a.Update(key("I"))
	run(a, cmd)
	if a.systemView == nil {
		t.Fatal("I should open the System screen")
	}
	screen := ansi.Strip(a.render())
	for _, want := range []string{"HOST", "ENGINE", "Ubuntu 24.04.1 LTS", "28.5.1  API 1.51", "overlay2", "DISK USAGE", "18.2GB", "6.1GB"} {
		if !strings.Contains(screen, want) {
			t.Errorf("System screen lacks %q:\n%s", want, screen)
		}
	}
	if a.panelShown() {
		t.Error("the panel should step aside for the screen")
	}
	press(a, "esc")
	if a.systemView != nil || !a.panelShown() {
		t.Fatal("esc should close the screen and bring the panel back")
	}
}

func TestHostUsageRates(t *testing.T) {
	var s systemState
	now := time.Now()
	s.samples = [2]docker.HostSample{hostSample(now.Add(-2*time.Second), 1000, 10000), hostSample(now, 1500, 11000)}
	s.samples[0].NetRx, s.samples[1].NetRx = 1000, 5000
	s.count = 2
	u := s.usage()
	if !u.ok || u.cpu != 50 || u.rx != 2000 {
		t.Fatalf("usage = %+v", u)
	}
	s.count = 1
	if s.usage().ok {
		t.Fatal("one sample gives no CPU rate")
	}
}

func TestShortOSAndUptime(t *testing.T) {
	for in, want := range map[string]string{
		"Ubuntu 24.04.1 LTS":             "Ubuntu 24.04",
		"Debian GNU/Linux 12 (bookworm)": "Debian GNU/Linux 12",
		"Alpine Linux v3.20":             "Alpine Linux v3.20",
	} {
		if got := shortOS(in, 12); got != want {
			t.Errorf("shortOS(%q) = %q, want %q", in, got, want)
		}
	}
	for d, want := range map[time.Duration]string{41*24*time.Hour + 2*time.Hour: "41d 2h", 3*time.Hour + 12*time.Minute: "3h 12m", 7 * time.Minute: "7m"} {
		if got := uptime(d); got != want {
			t.Errorf("uptime(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestHostPanelSetting(t *testing.T) {
	a := systemApp(t, &fakeClient{})
	a.configure(config.Config{HostPanel: "off"}, nil)
	if !a.system.hidden || a.flash != "" {
		t.Fatalf("hostPanel: off → hidden %v, flash %q", a.system.hidden, a.flash)
	}
	b := systemApp(t, &fakeClient{})
	b.configure(config.Config{HostPanel: "maybe"}, nil)
	if b.system.hidden || !strings.Contains(b.flash, `hostPanel: "maybe" is not on or off`) {
		t.Fatalf("hidden %v, flash %q", b.system.hidden, b.flash)
	}
}
