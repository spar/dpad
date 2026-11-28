package docker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"

	"github.com/spar/dpad/internal/debuglog"
)

// SystemInfo describes the daemon and the machine it runs on, from /info and
// /version. Over ssh:// that is the remote host.
type SystemInfo struct {
	Info       system.Info
	APIVersion string
	Platform   string            // e.g. "Docker Engine - Community"
	Components map[string]string // component name → version, e.g. containerd
}

// DockerDesktop reports whether the daemon runs in Docker Desktop's VM, whose
// /proc is out of reach.
func (s SystemInfo) DockerDesktop() bool {
	return strings.Contains(s.Info.OperatingSystem, "Docker Desktop") || strings.HasPrefix(s.Platform, "Docker Desktop")
}

// SystemInfo reads /info and /version. /version is best effort: older or
// unusual daemons still get their /info.
func (c *Client) SystemInfo(ctx context.Context) (SystemInfo, error) {
	res, err := c.api.Info(ctx, client.InfoOptions{})
	if err != nil {
		return SystemInfo{}, err
	}
	out := SystemInfo{Info: res.Info, Components: map[string]string{}}
	if v, err := c.api.ServerVersion(ctx, client.ServerVersionOptions{}); err == nil {
		out.APIVersion, out.Platform = v.APIVersion, v.Platform.Name
		for _, comp := range v.Components {
			out.Components[comp.Name] = comp.Version
		}
	}
	return out, nil
}

// DiskUsage is what docker system df reports for each kind of object.
type DiskUsage struct {
	Images, Containers, Volumes, BuildCache DiskUsageKind
}

// DiskUsageKind is the size of one kind of object and how much of it is unused.
type DiskUsageKind struct {
	Count, Active     int64
	Size, Reclaimable int64
}

// DiskUsage asks the daemon what its objects take on disk. It is slow on
// large hosts, as docker system df is.
func (c *Client) DiskUsage(ctx context.Context) (DiskUsage, error) {
	res, err := c.api.DiskUsage(ctx, client.DiskUsageOptions{Containers: true, Images: true, Volumes: true, BuildCache: true})
	if err != nil {
		return DiskUsage{}, err
	}
	return DiskUsage{
		Images:     DiskUsageKind{res.Images.TotalCount, res.Images.ActiveCount, res.Images.TotalSize, res.Images.Reclaimable},
		Containers: DiskUsageKind{res.Containers.TotalCount, res.Containers.ActiveCount, res.Containers.TotalSize, res.Containers.Reclaimable},
		Volumes:    DiskUsageKind{res.Volumes.TotalCount, res.Volumes.ActiveCount, res.Volumes.TotalSize, res.Volumes.Reclaimable},
		BuildCache: DiskUsageKind{res.BuildCache.TotalCount, res.BuildCache.ActiveCount, res.BuildCache.TotalSize, res.BuildCache.Reclaimable},
	}, nil
}

// HostSample is one reading of the daemon host's /proc and of the disk that
// holds Docker's data.
type HostSample struct {
	At                     time.Time
	CPUBusy, CPUTotal      uint64 // jiffies since boot
	MemTotal, MemAvailable uint64 // bytes
	SwapTotal, SwapFree    uint64
	Load                   [3]float64
	Uptime                 time.Duration
	NetRx, NetTx           uint64 // bytes since boot, physical interfaces only
	DiskSize, DiskUsed     uint64 // 0 when df failed
}

// HostUpdate carries a sample, or the error that ended the watch.
type HostUpdate struct {
	Sample HostSample
	Err    error
}

// ErrNoHostStats means the daemon's host can't be read: it is a VM (Docker
// Desktop), a tcp:// endpoint, or not this Linux machine.
var ErrNoHostStats = errors.New("host usage is only read on Linux, locally or over ssh://")

// hostInterval is how often the host is read.
const hostInterval = 2 * time.Second

const hostEnd = "@end"

// hostScript prints a sample every interval. It runs in one long-lived
// process, locally or over ssh, so polling costs no new connections.
func hostScript(root string) string {
	return fmt.Sprintf(`while :; do
echo @cpu; head -n 1 /proc/stat || exit 1
echo @mem; grep -E '^(MemTotal|MemAvailable|SwapTotal|SwapFree):' /proc/meminfo
echo @load; cat /proc/loadavg
echo @uptime; cat /proc/uptime
echo @net; tail -n +3 /proc/net/dev
echo @disk; df -Pk %s 2>/dev/null | tail -n 1
echo %s
sleep %d
done`, shellQuote(root), hostEnd, int(hostInterval/time.Second))
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// hostCommand builds the process that runs the script: sh here, or ssh to
// the endpoint's host. Tests replace it.
var hostCommand = func(ctx context.Context, target *SSHTarget, script string) *exec.Cmd {
	if target == nil {
		return exec.CommandContext(ctx, "sh", "-c", script)
	}
	args := append([]string{"-n", "-T"}, target.Options(true)...)
	return exec.CommandContext(ctx, "ssh", append(args, "--", target.Host, script)...)
}

// WatchHost streams samples of the daemon's host every few seconds until ctx
// ends or the reader stops. daemonName is /info's Name: a local daemon is
// only read when it names this machine, so a VM's socket isn't mistaken for
// the host. root is Docker's data directory, whose disk is reported.
func (c *Client) WatchHost(ctx context.Context, daemonName, root string) (<-chan HostUpdate, error) {
	var target *SSHTarget
	switch {
	case c.tunnel != nil:
		t := c.tunnel.target
		target = &t
	case runtime.GOOS != "linux" || !strings.HasPrefix(c.api.DaemonHost(), "unix://"):
		return nil, ErrNoHostStats
	default:
		if host, err := os.Hostname(); err != nil || host != daemonName {
			return nil, ErrNoHostStats
		}
	}
	cmd := hostCommand(ctx, target, hostScript(root))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	debuglog.Info("host watch started", "remote", target != nil)
	out := make(chan HostUpdate)
	go func() {
		defer close(out)
		err := readHostSamples(ctx, stdout, out)
		_ = cmd.Process.Kill()
		waitErr := cmd.Wait()
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = waitErr
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			err = fmt.Errorf("%w: %s", err, msg)
		}
		if err == nil {
			err = errors.New("host watch ended")
		}
		debuglog.Warn("host watch ended", "err", err)
		select {
		case out <- HostUpdate{Err: err}:
		case <-ctx.Done():
		}
	}()
	return out, nil
}

func readHostSamples(ctx context.Context, r io.Reader, out chan<- HostUpdate) error {
	sc := bufio.NewScanner(r)
	var block []string
	for sc.Scan() {
		line := sc.Text()
		if line != hostEnd {
			block = append(block, line)
			continue
		}
		s, err := ParseHostSample(block)
		block = block[:0]
		if err != nil {
			return err
		}
		s.At = time.Now()
		select {
		case out <- HostUpdate{Sample: s}:
		case <-ctx.Done():
			return nil
		}
	}
	return sc.Err()
}

// ParseHostSample reads one block of hostScript output.
func ParseHostSample(lines []string) (HostSample, error) {
	var s HostSample
	section := ""
	sawCPU := false
	for _, line := range lines {
		if strings.HasPrefix(line, "@") {
			section = line
			continue
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch section {
		case "@cpu":
			if f[0] != "cpu" || len(f) < 5 {
				return s, fmt.Errorf("unexpected /proc/stat line %q", line)
			}
			// user nice system idle iowait irq softirq steal; guest time is
			// already counted in user and nice.
			for i, v := range f[1:min(len(f), 9)] {
				n, _ := strconv.ParseUint(v, 10, 64)
				s.CPUTotal += n
				if i != 3 && i != 4 {
					s.CPUBusy += n
				}
			}
			sawCPU = true
		case "@mem":
			if len(f) < 2 {
				continue
			}
			n, _ := strconv.ParseUint(f[1], 10, 64)
			n *= 1024
			switch f[0] {
			case "MemTotal:":
				s.MemTotal = n
			case "MemAvailable:":
				s.MemAvailable = n
			case "SwapTotal:":
				s.SwapTotal = n
			case "SwapFree:":
				s.SwapFree = n
			}
		case "@load":
			for i := 0; i < 3 && i < len(f); i++ {
				s.Load[i], _ = strconv.ParseFloat(f[i], 64)
			}
		case "@uptime":
			secs, _ := strconv.ParseFloat(f[0], 64)
			s.Uptime = time.Duration(secs * float64(time.Second))
		case "@net":
			name, rest, ok := strings.Cut(line, ":")
			if !ok || virtualInterface(strings.TrimSpace(name)) {
				continue
			}
			cols := strings.Fields(rest)
			if len(cols) < 9 {
				continue
			}
			rx, _ := strconv.ParseUint(cols[0], 10, 64)
			tx, _ := strconv.ParseUint(cols[8], 10, 64)
			s.NetRx += rx
			s.NetTx += tx
		case "@disk":
			if len(f) < 4 {
				continue
			}
			size, _ := strconv.ParseUint(f[1], 10, 64)
			used, _ := strconv.ParseUint(f[2], 10, 64)
			s.DiskSize, s.DiskUsed = size*1024, used*1024
		}
	}
	if !sawCPU || s.MemTotal == 0 {
		return s, fmt.Errorf("%w: no /proc readings on the daemon's host", ErrNoHostStats)
	}
	return s, nil
}

// virtualInterface reports loopback, bridge and container-side interfaces,
// whose traffic is already counted on the physical ones.
func virtualInterface(name string) bool {
	if name == "lo" {
		return true
	}
	for _, p := range []string{"veth", "docker", "br-", "virbr", "cni", "flannel", "cali", "vxlan", "tun", "tap"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
