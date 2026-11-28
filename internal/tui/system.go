package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/moby/moby/api/types/container"

	"dpad/internal/config"
	"dpad/internal/docker"
	"dpad/internal/render"
)

// systemState holds what dpad knows about the daemon's host: /info, and the
// host's own CPU, memory and disk read from its /proc.
type systemState struct {
	info    *docker.SystemInfo
	infoErr error
	infoGen uint64

	watching bool
	hostErr  error // why host usage isn't shown; ErrNoHostStats is final
	hostGen  uint64
	samples  [2]docker.HostSample // previous and latest
	count    int

	hidden bool // the hostPanel: off setting
}

type (
	systemInfoMsg struct {
		gen  uint64
		info docker.SystemInfo
		err  error
	}
	systemInfoTickMsg struct{ gen uint64 }
	hostStartedMsg    struct {
		gen uint64
		ch  <-chan docker.HostUpdate
		err error
	}
	hostUpdateMsg struct {
		gen    uint64
		ch     <-chan docker.HostUpdate
		update docker.HostUpdate
		closed bool
	}
	hostRetryMsg struct{ gen uint64 }
)

// systemInfoInterval is how often /info is read again: counts and versions
// change rarely, and over ssh each read is a round trip.
const systemInfoInterval = 30 * time.Second

const hostRetryDelay = 15 * time.Second

func (a *App) loadSystemInfo() tea.Cmd {
	if a.client == nil || a.ctx.Err() != nil {
		return nil
	}
	a.system.infoGen++
	gen := a.system.infoGen
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
		defer cancel()
		info, err := a.client.SystemInfo(ctx)
		return systemInfoMsg{gen: gen, info: info, err: err}
	}
}

// updateSystem handles the messages that keep systemState current.
func (a *App) updateSystem(msg tea.Msg) tea.Cmd {
	s := &a.system
	switch msg := msg.(type) {
	case systemInfoMsg:
		if msg.gen != s.infoGen {
			return nil
		}
		s.infoErr = msg.err
		if msg.err == nil {
			s.info = &msg.info
		}
		gen := s.infoGen
		tick := tea.Tick(systemInfoInterval, func(time.Time) tea.Msg { return systemInfoTickMsg{gen} })
		return tea.Batch(tick, a.watchHost())
	case systemInfoTickMsg:
		if msg.gen != s.infoGen {
			return nil
		}
		return a.loadSystemInfo()
	case hostStartedMsg:
		if msg.gen != s.hostGen {
			return nil
		}
		if msg.err != nil {
			return a.hostFailed(msg.err)
		}
		return nextHostUpdate(msg.gen, msg.ch)
	case hostUpdateMsg:
		if msg.gen != s.hostGen {
			return nil
		}
		if msg.closed || msg.update.Err != nil {
			err := msg.update.Err
			if err == nil {
				err = errors.New("host watch ended")
			}
			return a.hostFailed(err)
		}
		s.hostErr = nil
		s.samples[0], s.samples[1] = s.samples[1], msg.update.Sample
		s.count++
		return nextHostUpdate(msg.gen, msg.ch)
	case hostRetryMsg:
		if msg.gen != s.hostGen {
			return nil
		}
		return a.watchHost()
	}
	return nil
}

func (a *App) hostFailed(err error) tea.Cmd {
	s := &a.system
	s.watching, s.hostErr, s.count = false, err, 0
	if errors.Is(err, docker.ErrNoHostStats) {
		return nil
	}
	gen := s.hostGen
	return tea.Tick(hostRetryDelay, func(time.Time) tea.Msg { return hostRetryMsg{gen} })
}

func nextHostUpdate(gen uint64, ch <-chan docker.HostUpdate) tea.Cmd {
	return func() tea.Msg {
		u, ok := <-ch
		return hostUpdateMsg{gen: gen, ch: ch, update: u, closed: !ok}
	}
}

// watchHost starts reading the host once /info is known and something shows
// it: the Containers panel or the System screen.
func (a *App) watchHost() tea.Cmd {
	s := &a.system
	if s.info == nil || s.watching || errors.Is(s.hostErr, docker.ErrNoHostStats) || a.ctx.Err() != nil {
		return nil
	}
	if !a.panelWanted() && a.systemView == nil {
		return nil
	}
	if s.info.DockerDesktop() || s.info.Info.OSType != "" && s.info.Info.OSType != "linux" {
		s.hostErr = docker.ErrNoHostStats
		return nil
	}
	s.watching = true
	s.hostGen++
	gen, name, root := s.hostGen, s.info.Info.Name, s.info.Info.DockerRootDir
	return func() tea.Msg {
		ch, err := a.client.WatchHost(a.ctx, name, root)
		return hostStartedMsg{gen: gen, ch: ch, err: err}
	}
}

// hostUsage is the host's latest reading, with CPU and network as rates
// over the last interval. ok is false until two samples arrived.
type hostUsage struct {
	ok     bool
	latest docker.HostSample
	cpu    float64
	rx, tx float64
}

func (s *systemState) usage() hostUsage {
	if s.count < 2 || s.hostErr != nil {
		return hostUsage{}
	}
	prev, cur := s.samples[0], s.samples[1]
	u := hostUsage{ok: true, latest: cur}
	if dt := cur.CPUTotal - prev.CPUTotal; cur.CPUTotal > prev.CPUTotal {
		u.cpu = float64(cur.CPUBusy-min(prev.CPUBusy, cur.CPUBusy)) / float64(dt) * 100
	}
	if secs := cur.At.Sub(prev.At).Seconds(); secs > 0 {
		u.rx = float64(cur.NetRx-min(prev.NetRx, cur.NetRx)) / secs
		u.tx = float64(cur.NetTx-min(prev.NetTx, cur.NetTx)) / secs
	}
	return u
}

// containersTotal sums the containers' latest stats; ok is false when the
// stream isn't running (the CPU graph is off) or has nothing yet.
type containersTotal struct {
	ok             bool
	cpu            float64 // percent of one core, as docker stats
	mem            uint64
	rx, tx         float64
	running, total int
}

func (v *ContainersView) usageTotals() containersTotal {
	var u containersTotal
	for _, c := range v.items {
		if c.State == container.StateRunning {
			u.running++
		}
	}
	u.total = len(v.items)
	if !v.showUsage() || len(v.usage.data) == 0 {
		return u
	}
	u.ok = true
	for id, s := range v.usage.data {
		if v.usage.health[id].err == nil {
			u.cpu += s.CPUPercent
			u.mem += s.MemUsage
			r := v.usage.rates[id]
			if r.NetValid {
				u.rx += r.NetRx
				u.tx += r.NetTx
			}
		}
	}
	return u
}

// The host panel's columns, fixed so every row lines up.
const (
	panelRows  = 5 // a blank line, System, Docker, Host, a blank line
	panelPad   = 2
	panelLabel = 8
	panelName  = 13
	panelBar   = 8
	cpuCol     = 5 + panelBar + 1 + 4 + 12 // "CPU  ■■■■■■■■  31% of 8 cores"
	memCol     = 5 + panelBar + 1 + 5 + 6  // "MEM  ■■■■■■■■  9.8G/15.6G"
	panelWidth = panelPad + panelLabel + panelName + cpuCol + 2 + memCol
	// wideWidth is where TX, RX and the other extra columns fit.
	wideWidth = panelWidth + 3 + 15 + 15 + 20
)

// panelWanted reports whether the Containers tab would show the panel now,
// before /info has arrived.
func (a *App) panelWanted() bool {
	if a.system.hidden || a.width < panelWidth || a.height < 20 || a.help || a.screen() != nil || !a.daemon.connected {
		return false
	}
	_, ok := a.tabs[a.active].(*ContainersView)
	return ok
}

func (a *App) panelShown() bool { return a.system.info != nil && a.panelWanted() }

// headerRows is how many rows sit between the tab rule and the body.
func (a *App) headerRows() int {
	if a.panelShown() {
		return panelRows
	}
	return 1
}

// layout gives the Containers tab the rows the panel leaves it.
func (a *App) layout() {
	v := a.containers()
	if v == nil {
		return
	}
	h := a.bodyHeight() + 1 - a.headerRows()
	if a.containersHeight != h || a.containersWidth != a.width {
		a.containersHeight, a.containersWidth = h, a.width
		v.SetSize(a.width, h)
	}
}

func pad(s string, w int) string  { return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0)) }
func rpad(s string, w int) string { return strings.Repeat(" ", max(w-lipgloss.Width(s), 0)) + s }

func fit(s string, w int) string { return pad(ansi.Truncate(s, w-1, "…"), w) }

func panelPercent(p float64) string {
	return render.PercentStyle(p).UnsetBold().Render(fmt.Sprintf("%3.0f%%", p))
}

// panelRow is a label and a name, then the two columns under CPU and MEM.
func panelRow(label, name, cpu, mem string) string {
	return strings.Repeat(" ", panelPad) + pad(render.Header.Render(label), panelLabel) + fit(name, panelName) + pad(cpu, cpuCol) + "  " + mem
}

func cpuCell(p float64, cores int) string {
	return panelKey("CPU  ") + render.Gauge(p, panelBar) + " " + panelPercent(p) + render.Dim.Render(fmt.Sprintf(" of %d cores", cores))
}

func memCell(used, total uint64) string {
	p := 0.0
	if total > 0 {
		p = float64(used) / float64(total) * 100
	}
	return panelKey("MEM  ") + render.Gauge(p, panelBar) + " " + rpad(render.Bold.Render(render.CompactSize(used)), 5) + render.Dim.Render("/"+render.CompactSize(total))
}

// panelKey styles a label such as CPU or Load like the row names, so it
// stands out from the dimmed units after the numbers. Bold matters in
// themes like ink and mono, where the accent is the text color.
func panelKey(s string) string { return render.Header.Render(s) }

func keyValue(k, v string) string { return panelKey(pad(k, max(5, len(k)+1))) + v }

func wideCols(cols ...string) string {
	widths := []int{15, 15, 20}
	var b strings.Builder
	b.WriteString("   ")
	for i, c := range cols {
		b.WriteString(pad(c, widths[i]))
	}
	return b.String()
}

func rateCell(label string, rate float64) string {
	return panelKey(label+" ") + render.Rate(rate)
}

// hostPanel draws the System, Docker and Host rows above the containers.
func (a *App) hostPanel() []string {
	info := a.system.info
	cores, memTotal := info.Info.NCPU, uint64(max(info.Info.MemTotal, 0))
	host := a.system.usage()
	ctrs := a.containers().usageTotals()
	wide := a.width >= wideWidth

	var system string
	switch {
	case host.ok:
		l := host.latest
		system = panelRow("System", render.Bold.Render(info.Info.Name), cpuCell(host.cpu, cores), memCell(l.MemTotal-min(l.MemAvailable, l.MemTotal), l.MemTotal))
		if wide {
			swap := panelKey("Swap ") + "off"
			if l.SwapTotal > 0 {
				swap = panelKey("Swap ") + render.CompactSize(l.SwapTotal-min(l.SwapFree, l.SwapTotal)) + render.Dim.Render("/"+render.CompactSize(l.SwapTotal))
			}
			system = pad(system, panelWidth) + wideCols(rateCell("TX", host.tx), rateCell("RX", host.rx), swap)
		}
	default:
		note := "reading host usage…"
		if a.system.hostErr != nil {
			note = "host usage n/a"
			if !errors.Is(a.system.hostErr, docker.ErrNoHostStats) {
				note += ": " + oneLine(connectionError(a.system.hostErr))
			}
		}
		what := shortOS(info.Info.OperatingSystem, 40)
		if info.DockerDesktop() {
			what = "Docker Desktop VM"
		}
		system = strings.Repeat(" ", panelPad) + pad(render.Header.Render("System"), panelLabel) + fit(render.Bold.Render(info.Info.Name), panelName) +
			render.Dim.Render(fmt.Sprintf("%s · %d cores · %s · %s", what, cores, render.CompactSize(memTotal), note))
	}

	docker := "v" + info.Info.ServerVersion
	var dockerRow string
	if ctrs.ok {
		dockerRow = panelRow("Docker", docker, cpuCell(ctrs.cpu/float64(max(cores, 1)), cores), memCell(ctrs.mem, memTotal))
		if wide {
			running := render.StateStyle("running", 0).Render(fmt.Sprint(ctrs.running)) + render.Dim.Render(fmt.Sprintf("/%d running", ctrs.total))
			dockerRow = pad(dockerRow, panelWidth) + wideCols(rateCell("TX", ctrs.tx), rateCell("RX", ctrs.rx), running)
		}
	} else {
		dockerRow = strings.Repeat(" ", panelPad) + pad(render.Header.Render("Docker"), panelLabel) + fit(docker, panelName) +
			render.Dim.Render(fmt.Sprintf("%d/%d running · container usage needs the CPU graph (o)", ctrs.running, ctrs.total))
	}

	osName := shortOS(info.Info.OperatingSystem, panelName-1)
	if info.DockerDesktop() {
		osName = "Desktop VM"
	}
	var hostRow string
	if host.ok {
		l := host.latest
		hostRow = panelRow("Host", osName, keyValue("Load", fmt.Sprintf("%.2f %.2f %.2f", l.Load[0], l.Load[1], l.Load[2])), keyValue("Up", uptime(l.Uptime)))
		if wide {
			hostRow = pad(hostRow, panelWidth) + "   " + pad(keyValue("Kernel", info.Info.KernelVersion), 30) + keyValue("Arch", info.Info.Architecture)
		}
	} else {
		hostRow = panelRow("Host", osName, keyValue("Kern", info.Info.KernelVersion), keyValue("Arch", info.Info.Architecture))
	}

	lines := []string{"", system, dockerRow, hostRow, ""}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, a.width, "…")
	}
	return lines
}

// shortOS trims an /info OperatingSystem such as "Ubuntu 24.04.1 LTS" to fit
// w cells: "LTS" and codenames first, then the patch version.
func shortOS(s string, w int) string {
	s = strings.TrimSpace(strings.TrimSuffix(s, " LTS"))
	if i := strings.Index(s, " ("); i > 0 {
		s = s[:i]
	}
	if word := s[strings.LastIndex(s, " ")+1:]; len(s) > w && strings.Count(word, ".") > 1 {
		s = s[:strings.LastIndex(s, ".")]
	}
	return s
}

// uptime formats a duration as its two largest units, such as "41d 2h".
func uptime(d time.Duration) string {
	days, hours, mins := int(d.Hours())/24, int(d.Hours())%24, int(d.Minutes())%60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

func (a *App) toggleHostPanel() tea.Cmd {
	a.system.hidden = !a.system.hidden
	value := ""
	if a.system.hidden {
		value = "off"
	}
	if err := config.SaveHostPanel(value); err != nil {
		a.flash, a.flashErr = "host panel: could not save: "+oneLine(err.Error()), true
	}
	if a.settings != nil {
		a.settings.rowCache = nil
	}
	if !a.system.hidden && a.width < panelWidth {
		a.flash = fmt.Sprintf("the host panel needs %d columns", panelWidth)
	}
	a.layout()
	return a.watchHost()
}

func (a *App) hostPanelValue() string {
	if a.system.hidden {
		return "off " + render.Dim.Render("· Containers tab; enter or t shows it")
	}
	return "on " + render.Dim.Render("· Containers tab; enter or t hides it")
}
