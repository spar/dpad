package docker

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
)

type infoEngine struct {
	engine
	host    string
	info    system.Info
	version client.ServerVersionResult
	verErr  error
}

func (e *infoEngine) Info(context.Context, client.InfoOptions) (client.SystemInfoResult, error) {
	return client.SystemInfoResult{Info: e.info}, nil
}

func (e *infoEngine) ServerVersion(context.Context, client.ServerVersionOptions) (client.ServerVersionResult, error) {
	return e.version, e.verErr
}

func (e *infoEngine) DaemonHost() string { return e.host }

func TestSystemInfoAddsVersionsWhenTheDaemonHasThem(t *testing.T) {
	e := &infoEngine{
		info:    system.Info{Name: "homelab", NCPU: 8, OperatingSystem: "Ubuntu 24.04.1 LTS"},
		version: client.ServerVersionResult{APIVersion: "1.51", Platform: client.PlatformInfo{Name: "Docker Engine - Community"}, Components: []system.ComponentVersion{{Name: "containerd", Version: "1.7.27"}}},
	}
	got, err := (&Client{api: e}).SystemInfo(context.Background())
	if err != nil || got.Info.Name != "homelab" || got.APIVersion != "1.51" || got.Components["containerd"] != "1.7.27" || got.DockerDesktop() {
		t.Fatalf("got %+v, %v", got, err)
	}
	e.verErr = errors.New("no /version")
	if got, err := (&Client{api: e}).SystemInfo(context.Background()); err != nil || got.Info.NCPU != 8 {
		t.Fatalf("a /version failure should keep /info: %+v, %v", got, err)
	}
	desktop := SystemInfo{Info: system.Info{OperatingSystem: "Docker Desktop"}}
	if !desktop.DockerDesktop() {
		t.Fatal("Docker Desktop not recognized")
	}
}

var hostBlock = strings.Split(`@cpu
cpu  100 20 30 800 50 5 5 10 7 0
@mem
MemTotal:       16384000 kB
MemAvailable:    6144000 kB
SwapTotal:       2048000 kB
SwapFree:        1800000 kB
@load
1.42 1.37 1.29 2/900 12345
@uptime
3549600.12 2800000.00
@net
    lo: 5000 10 0 0 0 0 0 0 5000 10 0 0 0 0 0 0
  eth0: 1000 10 0 0 0 0 0 0 400 10 0 0 0 0 0 0
vethab12: 99999 10 0 0 0 0 0 0 99999 10 0 0 0 0 0 0
docker0: 88888 10 0 0 0 0 0 0 88888 10 0 0 0 0 0 0
 wlan0: 24 1 0 0 0 0 0 0 600 1 0 0 0 0 0 0
@disk
/dev/sda1 228000000 142000000 86000000 63% /`, "\n")

func TestParseHostSample(t *testing.T) {
	s, err := ParseHostSample(hostBlock)
	if err != nil {
		t.Fatal(err)
	}
	// Total skips guest time (already in user); busy also skips idle and iowait.
	if s.CPUTotal != 1020 || s.CPUBusy != 170 {
		t.Errorf("cpu busy/total = %d/%d", s.CPUBusy, s.CPUTotal)
	}
	if s.MemTotal != 16384000*1024 || s.MemAvailable != 6144000*1024 || s.SwapTotal != 2048000*1024 || s.SwapFree != 1800000*1024 {
		t.Errorf("memory = %+v", s)
	}
	if s.Load != [3]float64{1.42, 1.37, 1.29} || s.Uptime.Truncate(time.Hour) != 986*time.Hour {
		t.Errorf("load %v uptime %v", s.Load, s.Uptime)
	}
	if s.NetRx != 1024 || s.NetTx != 1000 {
		t.Errorf("net rx/tx = %d/%d, want physical interfaces only", s.NetRx, s.NetTx)
	}
	if s.DiskSize != 228000000*1024 || s.DiskUsed != 142000000*1024 {
		t.Errorf("disk = %d/%d", s.DiskUsed, s.DiskSize)
	}
	if _, err := ParseHostSample([]string{"@cpu", "@mem"}); err == nil {
		t.Error("a host without /proc should be an error")
	}
}

func TestWatchHostSkipsDaemonsItCannotRead(t *testing.T) {
	for _, host := range []string{"tcp://10.0.4.12:2375", "unix:///var/run/docker.sock"} {
		c := &Client{api: &infoEngine{host: host}}
		if _, err := c.WatchHost(context.Background(), "not-this-machine", "/"); !errors.Is(err, ErrNoHostStats) {
			t.Errorf("%s: err = %v, want ErrNoHostStats", host, err)
		}
	}
}

func TestWatchHostRunsTheScriptOverSSH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	var gotArgs []string
	old := hostCommand
	t.Cleanup(func() { hostCommand = old })
	hostCommand = func(ctx context.Context, target *SSHTarget, script string) *exec.Cmd {
		gotArgs = []string{target.User, target.Host, script}
		return exec.CommandContext(ctx, "sh", "-c", "printf '%s\\n' \"$0\" @end; printf '@cpu\\n'; exit 3", strings.Join(hostBlock, "\n"))
	}
	c := &Client{api: &infoEngine{}, tunnel: &sshTunnel{target: SSHTarget{User: "spar", Host: "homelab"}}}
	ch, err := c.WatchHost(context.Background(), "homelab", "/var/lib/docker")
	if err != nil {
		t.Fatal(err)
	}
	if u := <-ch; u.Err != nil || u.Sample.MemTotal == 0 || u.Sample.At.IsZero() {
		t.Fatalf("first update = %+v", u)
	}
	if u := <-ch; u.Err == nil {
		t.Fatal("the script ending should end the watch with an error")
	}
	if _, open := <-ch; open {
		t.Fatal("channel should close after the error")
	}
	if gotArgs[0] != "spar" || gotArgs[1] != "homelab" || !strings.Contains(gotArgs[2], "df -Pk '/var/lib/docker'") {
		t.Errorf("ssh target/script = %q", gotArgs)
	}
}

// The script itself, against this machine's /proc.
func TestHostScriptReadsLinuxProc(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs /proc")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", hostScript("/"))
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	ch := make(chan HostUpdate, 1)
	go func() { _ = readHostSamples(ctx, out, ch) }()
	select {
	case u := <-ch:
		s := u.Sample
		if s.CPUTotal == 0 || s.MemTotal == 0 || s.Uptime == 0 || s.DiskSize == 0 {
			t.Fatalf("sample = %+v", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no sample")
	}
}
