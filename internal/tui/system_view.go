package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/spar/dpad/internal/docker"
	"github.com/spar/dpad/internal/render"
)

// systemView is the I screen: everything known about the daemon and its host.
type systemView struct {
	app           *App
	width, height int
	top           int
	disk          *docker.DiskUsage
	diskErr       error
	diskGen       uint64
	loadedAt      time.Time
}

type systemDiskMsg struct {
	gen  uint64
	disk docker.DiskUsage
	err  error
}

func (a *App) openSystemView() tea.Cmd {
	a.systemView = &systemView{app: a}
	a.systemView.SetSize(a.width, a.bodyHeight())
	return tea.Batch(a.systemView.loadDisk(), a.loadSystemInfo(), a.watchHost())
}

func (v *systemView) loadDisk() tea.Cmd {
	v.diskGen++
	gen, a := v.diskGen, v.app
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
		defer cancel()
		disk, err := a.client.DiskUsage(ctx)
		return systemDiskMsg{gen: gen, disk: disk, err: err}
	}
}

func (v *systemView) SetSize(width, height int) { v.width, v.height = width, height }

func (v *systemView) Update(msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(systemDiskMsg); ok && msg.gen == v.diskGen {
		v.diskErr = msg.err
		if msg.err == nil {
			v.disk, v.loadedAt = &msg.disk, time.Now()
		}
	}
	return nil
}

func (v *systemView) handleKey(a *App, msg tea.KeyPressMsg) tea.Cmd {
	switch key := msg.String(); key {
	case "esc", "q", "I":
		a.systemView = nil
		a.layout()
	case "r", "ctrl+r":
		return tea.Batch(v.loadDisk(), a.loadSystemInfo())
	case "up", "k":
		v.top = max(v.top-1, 0)
	case "down", "j":
		v.top++
	case "g", "home":
		v.top = 0
	}
	return nil
}

func (v *systemView) Status() string {
	s := "System · /info every 30s · host every 2s"
	switch {
	case v.diskErr != nil:
		s += " · " + render.Error.Render("disk usage: "+oneLine(connectionError(v.diskErr)))
	case v.disk == nil:
		s += " · reading disk usage…"
	default:
		s += " · disk usage read " + v.loadedAt.Format("15:04:05")
	}
	return s
}

func (v *systemView) Footer() string {
	return hints("esc", "back", "r", "refresh disk usage", "↑↓", "scroll", "?", "help")
}

// The System screen's columns.
const (
	sysColWidth = 38
	sysKeyWidth = 12
)

func (v *systemView) View() string {
	a := v.app
	info := a.system.info
	if info == nil {
		if a.system.infoErr != nil {
			return render.Error.Render("system info unavailable: " + oneLine(connectionError(a.system.infoErr)))
		}
		return render.Dim.Render("reading system info…")
	}
	in := info.Info
	host := a.system.usage()
	ctrs := a.containers().usageTotals()
	indent := strings.Repeat(" ", panelPad)
	kv := func(k, val string) string { return pad(render.Dim.Render(k), sysKeyWidth) + val }
	head := func(s string) string { return render.Header.Render(s) }
	twoCols := v.width >= panelPad+2*sysColWidth

	hostRows := []string{
		head("HOST"),
		kv("name", render.Bold.Render(in.Name)),
		kv("os", in.OperatingSystem),
		kv("kernel", in.KernelVersion),
		kv("hardware", fmt.Sprintf("%s · %d cpus · %s", in.Architecture, in.NCPU, render.CompactSize(uint64(max(in.MemTotal, 0))))),
	}
	if host.ok {
		l := host.latest.Load
		hostRows = append(hostRows, kv("uptime", uptime(host.latest.Uptime)), kv("load", fmt.Sprintf("%.2f %.2f %.2f", l[0], l[1], l[2])))
	} else {
		hostRows = append(hostRows, kv("uptime", render.Dim.Render(v.hostNote())), "")
	}
	engineRows := []string{
		head("ENGINE"),
		kv("docker", in.ServerVersion+render.Dim.Render(apiNote(info.APIVersion))),
		kv("runtime", runtimeNote(in.DefaultRuntime, info.Components)),
		kv("containerd", orDash(info.Components["containerd"])),
		kv("storage", orDash(in.Driver)),
		kv("cgroup", cgroupNote(in.CgroupVersion, in.CgroupDriver)),
		kv("logging", orDash(in.LoggingDriver)),
	}
	var lines []string
	if twoCols {
		for i := range max(len(hostRows), len(engineRows)) {
			l, r := "", ""
			if i < len(hostRows) {
				l = hostRows[i]
			}
			if i < len(engineRows) {
				r = engineRows[i]
			}
			lines = append(lines, indent+pad(l, sysColWidth)+r)
		}
	} else {
		for _, l := range append(append(hostRows, ""), engineRows...) {
			lines = append(lines, indent+l)
		}
	}

	lines = append(lines, "", indent+head("USAGE"))
	gauge := func(k string, p float64, tail string) string {
		return indent + kv(k, render.Gauge(p, 16)+" "+panelPercent(p)+"  "+tail)
	}
	memTotal := uint64(max(in.MemTotal, 0))
	ctrCPU := ctrs.cpu / float64(max(in.NCPU, 1))
	if host.ok {
		l := host.latest
		used := l.MemTotal - min(l.MemAvailable, l.MemTotal)
		ctrTail := func(s string) string {
			if !ctrs.ok {
				return ""
			}
			return render.Dim.Render("containers ") + s
		}
		lines = append(lines,
			gauge("cpu", host.cpu, pad(fmt.Sprintf("of %d cores", in.NCPU), 13)+ctrTail(fmt.Sprintf("%.0f%%", ctrCPU))),
			gauge("memory", percent(used, l.MemTotal), pad(render.CompactSize(used)+render.Dim.Render("/"+render.CompactSize(l.MemTotal)), 13)+ctrTail(render.CompactSize(ctrs.mem))))
		if l.DiskSize > 0 {
			lines = append(lines, gauge("disk", percent(l.DiskUsed, l.DiskSize), pad(render.CompactSize(l.DiskUsed)+render.Dim.Render("/"+render.CompactSize(l.DiskSize)), 13)+render.Dim.Render(in.DockerRootDir)))
		}
	} else if ctrs.ok {
		lines = append(lines,
			gauge("containers", ctrCPU, render.Dim.Render(fmt.Sprintf("CPU of %d cores", in.NCPU))),
			gauge("", percent(ctrs.mem, memTotal), render.CompactSize(ctrs.mem)+render.Dim.Render("/"+render.CompactSize(memTotal)+" memory")),
			indent+render.Dim.Render(v.hostNote()))
	} else {
		lines = append(lines, indent+render.Dim.Render(v.hostNote()+"; container usage shows when the CPU graph is on (o)"))
	}

	lines = append(lines, "")
	for _, l := range v.diskTable() {
		lines = append(lines, indent+l)
	}
	if len(in.Warnings) > 0 {
		lines = append(lines, "", indent+head("WARNINGS"))
		for _, w := range in.Warnings {
			lines = append(lines, indent+render.Warn.Render("! ")+strings.TrimPrefix(oneLine(w), "WARNING: "))
		}
	}

	title := render.Bold.Render("System") + render.Dim.Render(" · ") + render.Accent.Render(in.Name)
	body := append([]string{title}, lines...)
	v.top = min(v.top, max(len(body)-v.height, 0))
	if v.top > 0 {
		body = append([]string{title}, body[1+v.top:]...)
	}
	for i := range body {
		body[i] = ansi.Truncate(body[i], v.width, "…")
	}
	if len(body) > v.height {
		body = body[:v.height]
	}
	return strings.Join(body, "\n")
}

// diskTable is docker system df: one row per kind of object.
func (v *systemView) diskTable() []string {
	head := pad(render.Header.Render("DISK USAGE"), sysKeyWidth+4) + render.Dim.Render(rpad("count", 7)+rpad("in use", 8)+rpad("size", 9)+rpad("reclaimable", 13))
	if v.disk == nil {
		note := "reading…"
		if v.diskErr != nil {
			note = "unavailable: " + oneLine(connectionError(v.diskErr))
		}
		return []string{head, render.Dim.Render(note), "", "", ""}
	}
	row := func(name string, k docker.DiskUsageKind) string {
		return pad(render.Dim.Render(name), sysKeyWidth+4) + rpad(fmt.Sprint(k.Count), 7) + rpad(fmt.Sprint(k.Active), 8) +
			rpad(render.Size(k.Size), 9) + render.Dim.Render(rpad(render.Size(k.Reclaimable), 13))
	}
	d := v.disk
	return []string{head, row("images", d.Images), row("containers", d.Containers), row("volumes", d.Volumes), row("build cache", d.BuildCache)}
}

func (v *systemView) hostNote() string {
	s := &v.app.system
	switch {
	case s.hostErr == nil:
		return "reading host usage…"
	case s.info != nil && s.info.DockerDesktop():
		return "host usage n/a: Docker Desktop runs Docker in a VM"
	case errors.Is(s.hostErr, docker.ErrNoHostStats):
		return "host usage n/a: " + docker.ErrNoHostStats.Error()
	}
	return "host usage n/a: " + oneLine(connectionError(s.hostErr))
}

func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}

func orDash(s string) string {
	if s == "" {
		return render.Dim.Render("—")
	}
	return s
}

func apiNote(api string) string {
	if api == "" {
		return ""
	}
	return "  API " + api
}

func runtimeNote(name string, components map[string]string) string {
	if v := components[name]; v != "" {
		return name + " " + v
	}
	return orDash(name)
}

func cgroupNote(version, driver string) string {
	switch {
	case version == "":
		return orDash(driver)
	case driver == "":
		return "v" + version
	}
	return "v" + version + " (" + driver + ")"
}
