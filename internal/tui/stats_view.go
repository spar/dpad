package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"dockernext/internal/docker"
	"dockernext/internal/render"
	"dockernext/internal/sortx"
)

type (
	statsStartedMsg struct {
		ch         <-chan docker.StatsUpdate
		generation uint64
	}
	statsBatchMsg []docker.StatsUpdate
	statsErrMsg   struct {
		err        error
		generation uint64
	}
)

// StatsView shows live, sortable resource usage for running containers.
type StatsView struct {
	list[docker.Stats]
	ctx           context.Context
	client        dockerClient
	ch            <-chan docker.StatsUpdate
	data          map[string]docker.Stats
	watching      bool // WatchStats requested
	history       map[string]*statsHistory
	historyCache  statsHistoryCache
	rates         map[string]docker.Rates
	totals        bool
	health        map[string]statsHealth
	requested     bool
	watchGen      uint64
	watchErr      error
	retryDelay    time.Duration
	endpoint      string
	grouped       bool
	membership    map[string]statsMembership
	membershipErr error
	projects      map[string]*statsProject
	projectRates  map[string]docker.Rates
	expanded      map[string]bool
	groupMembers  int
}

func NewStatsView(ctx context.Context, c dockerClient, key sortx.Key, reverse bool) *StatsView {
	v := &StatsView{ctx: ctx, client: c, data: map[string]docker.Stats{}, history: map[string]*statsHistory{}, rates: map[string]docker.Rates{}, health: map[string]statsHealth{}}
	if c != nil {
		v.endpoint = c.ConnectionLabel()
	}
	v.list = list[docker.Stats]{
		columns: render.StatsColumns,
		keys:    sortx.StatsKeys,
		sortCol: map[sortx.Key]int{sortx.Name: 0, sortx.CPU: 1, sortx.Mem: 3, sortx.Net: 6, sortx.IO: 7, sortx.PIDs: 8},
		key:     key,
		reverse: reverse,
		match:   v.statsMatches,
		sort:    func(items []docker.Stats, _ sortx.Key, _ bool) { v.sortSamples(items, v.grouped) },
		rows:    v.statsRows,
		loading: "connecting…",
		empty:   "no running containers",
	}
	v.SetTotals(false)
	return v
}

func (v *StatsView) SetTotals(totals bool) {
	v.totals = totals
	v.columns = slices.Clone(render.StatsColumns)
	if !totals {
		v.columns[6].Header = "NET RX / TX"
		v.columns[7].Header = "DISK READ / WRITE"
	}
	if v.grouped {
		v.columns[0].Header = "PROJECT / CONTAINER"
		v.columns[0].Max = 35
	}
	v.rowCache = nil
}

func (v *StatsView) Title() string { return "Stats" }

func (v *StatsView) Init() tea.Cmd { return nil }

// Focus starts watching stats the first time the tab is shown: that opens one
// stream per running container, which isn't worth it if the tab never is.
func (v *StatsView) Focus() tea.Cmd {
	v.requested = true
	if v.grouped {
		v.rebuildItems()
	}
	if v.watching {
		return nil
	}
	v.watching = true
	v.watchGen++
	gen := v.watchGen
	return func() tea.Msg {
		ch, err := v.client.WatchStats(v.ctx)
		if err != nil {
			return statsErrMsg{err: err, generation: gen}
		}
		return statsStartedMsg{ch: ch, generation: gen}
	}
}

// statsFlushInterval is how long waitStats collects updates before handing
// them over. Every container reports about once a second, each at its own
// moment, so collecting for a second turns N redraws a second into one.
const statsFlushInterval = time.Second

// waitStats blocks for the next update, then collects everything else that
// arrives within statsFlushInterval so the batch costs one re-render.
func waitStats(ch <-chan docker.StatsUpdate) tea.Cmd {
	return func() tea.Msg {
		timer := time.NewTimer(statsFlushInterval)
		defer timer.Stop()
		var u docker.StatsUpdate
		select {
		case value, ok := <-ch:
			if !ok {
				return statsClosedMsg{ch}
			}
			u = value
		case <-timer.C:
			return statsTickMsg{ch}
		}
		batch := statsBatchMsg{u}
		timer.Reset(statsFlushInterval)
		for {
			select {
			case u, ok := <-ch:
				if !ok {
					return statsSourceBatchMsg{ch: ch, batch: batch}
				}
				batch = append(batch, u)
			case <-timer.C:
				return statsSourceBatchMsg{ch: ch, batch: batch}
			}
		}
	}
}

func (v *StatsView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case containersMsg:
		v.updateMembership(msg)
		return nil
	case statsRetryMsg:
		if msg.generation == v.watchGen {
			return v.retryNow()
		}
		return nil
	case refreshMsg:
		return v.retryNow()
	case statsClosedMsg:
		if msg.ch != v.ch || v.ctx.Err() != nil {
			return nil
		}
		v.watchErr = fmt.Errorf("stats connection closed")
		for id, h := range v.health {
			v.markUnavailable(id, h, v.watchErr)
		}
		v.rowCache = nil
		if v.grouped {
			v.rebuildItems()
		}
		return v.retryWatch()
	case statsTickMsg:
		if msg.ch != v.ch || v.ctx.Err() != nil {
			return nil
		}
		v.expireSamples(time.Now())
		return waitStats(v.ch)
	case statsSourceBatchMsg:
		if msg.ch != v.ch {
			return nil
		}
		return v.Update(msg.batch)
	case statsStartedMsg:
		if msg.generation != v.watchGen {
			return nil
		}
		v.ch = msg.ch
		v.watchErr = nil
		v.retryDelay = time.Second
		clear(v.data)
		clear(v.history)
		clear(v.rates)
		clear(v.health)
		v.historyCache = statsHistoryCache{}
		v.loaded = true
		v.rebuildItems()
		return waitStats(v.ch)
	case statsBatchMsg:
		now := time.Now()
		for _, u := range msg {
			h := v.history[u.Stats.ID]
			health, known := v.health[u.Stats.ID]
			if known && u.Generation < health.generation {
				continue
			}
			if u.Gone {
				delete(v.data, u.Stats.ID)
				delete(v.history, u.Stats.ID)
				delete(v.rates, u.Stats.ID)
				delete(v.health, u.Stats.ID)
				if v.historyCache.history == h {
					v.historyCache = statsHistoryCache{}
				}
			} else if u.Unavailable {
				if _, exists := v.data[u.Stats.ID]; !exists {
					v.data[u.Stats.ID] = u.Stats
				}
				health.generation = u.Generation
				err := u.Err
				if err == nil {
					err = fmt.Errorf("stats stream unavailable")
				}
				v.markUnavailable(u.Stats.ID, health, err)
			} else {
				if known && u.Generation == health.generation && !u.SampledAt.IsZero() && !health.sourceAt.IsZero() && !u.SampledAt.After(health.sourceAt) {
					delete(v.rates, u.Stats.ID)
					continue
				}
				rates := u.Rates
				if health.err != nil {
					rates = docker.Rates{}
				}
				v.health[u.Stats.ID] = statsHealth{generation: u.Generation, received: now, sourceAt: u.SampledAt}
				v.rates[u.Stats.ID] = rates
				if !v.noHighlight {
					if h == nil || h.generation != u.Generation {
						h = &statsHistory{generation: u.Generation}
						v.history[u.Stats.ID] = h
					}
					if !h.add(u.Stats, u.SampledAt, now) {
						continue
					}
				}
				v.data[u.Stats.ID] = u.Stats
			}
		}
		v.expireSamples(now)
		v.loaded = true
		v.rebuildItems()
		return waitStats(v.ch)
	case statsErrMsg:
		if msg.generation != v.watchGen || v.ctx.Err() != nil {
			return nil
		}
		v.watchErr = msg.err
		for id, h := range v.health {
			v.markUnavailable(id, h, msg.err)
		}
		v.rowCache = nil
		if v.grouped {
			v.rebuildItems()
		}
		return v.retryWatch()
	}
	return nil
}

func (v *StatsView) historyVisible() bool { return !v.noHighlight && v.width >= 80 && v.height >= 10 }

func (v *StatsView) SetSize(width, height int) {
	v.list.SetSize(width, height)
	if v.historyVisible() {
		v.page = max(height-7, 1)
	}
}

func (v *StatsView) View() string {
	if v.watchErr != nil && len(v.data) == 0 {
		return render.Error.Render("stats unavailable: "+connectionError(v.watchErr)) + "\n" + render.Dim.Render("reconnecting… (ctrl+r retries now)")
	}
	if !v.historyVisible() || v.err != nil || !v.loaded {
		return v.list.View()
	}
	id, name, ok := v.Selected()
	if !ok {
		return v.list.View()
	}
	panel := v.historyPanel(id, name, time.Now())
	if panel == "" {
		return v.list.View()
	}
	height := v.height - 5
	table := v.view(height)
	padding := max(height-strings.Count(table, "\n")-1, 0)
	return table + strings.Repeat("\n", padding) + "\n\n" + panel
}

// statsShortcuts are single keys that pick a sort key directly.
var statsShortcuts = []struct {
	key  string
	sort sortx.Key
}{{"c", sortx.CPU}, {"m", sortx.Mem}, {"n", sortx.Net}, {"i", sortx.IO}, {"p", sortx.PIDs}, {"a", sortx.Name}}

// shortcutHelp returns the shortcut keys and the sort keys they select,
// joined by sep (e.g. "c/m/n/i/p/a" and "cpu/mem/net/io/pids/name").
func shortcutHelp(sep string) (keys, sorts string) {
	var k []string
	var s []sortx.Key
	for _, sc := range statsShortcuts {
		k, s = append(k, sc.key), append(s, sc.sort)
	}
	return strings.Join(k, sep), sortx.Join(s, sep)
}

func (v *StatsView) HandleKey(key string) tea.Cmd {
	if key == "v" && !v.noHighlight {
		v.SetGrouped(!v.grouped)
		return nil
	}
	if key == "space" && v.grouped {
		v.toggleStatsProject(true)
		return nil
	}
	if key == "ctrl+r" {
		return v.retryNow()
	}
	if key == "t" {
		v.SetTotals(!v.totals)
		return nil
	}
	for _, sc := range statsShortcuts {
		if sc.key == key {
			v.setSort(sc.sort, v.reverse)
			return nil
		}
	}
	return v.list.HandleKey(key)
}

func (v *StatsView) Status() string {
	var cpu float64
	var mem uint64
	available := 0
	var problem error
	for id, s := range v.data {
		if err := v.health[id].err; err != nil {
			problem = err
			continue
		}
		available++
		cpu += s.CPUPercent
		mem += s.MemUsage
	}
	mode := "rates"
	if v.totals {
		mode = "totals"
	}
	usage := fmt.Sprintf("CPU %.1f%% · MEM %s", cpu, render.BinSize(mem))
	if available == 0 && (len(v.data) > 0 || v.watchErr != nil || (v.grouped && v.groupMembers > 0)) {
		usage = "CPU — · MEM —"
	}
	total := len(v.data)
	if v.grouped {
		total = v.groupMembers
	}
	status := fmt.Sprintf("%d/%d available · %s · I/O: %s · %s", available, total, usage, mode, sortLabel(v.key, v.reverse))
	if v.watchErr != nil {
		status += " · reconnecting: " + connectionError(v.watchErr)
	} else if problem != nil {
		status += " · recovering streams: " + connectionError(problem)
	}
	if v.grouped {
		status = fmt.Sprintf("%d projects · ", len(v.projects)) + status
	}
	if v.membershipErr != nil && v.grouped {
		status += " · Compose metadata stale: " + connectionError(v.membershipErr)
	}
	if v.noHighlight && v.endpoint != "" {
		status = v.endpoint + " · " + status
	}
	return status
}

func (v *StatsView) Help() [][2]string {
	keys, sorts := shortcutHelp(" / ")
	help := append([][2]string{{keys, "sort by " + sorts}, {"t", "toggle I/O rates / cumulative totals (sorting follows mode)"}}, v.list.Help()...)
	if !v.noHighlight {
		help = append(help, [2]string{"v", "toggle Compose projects / flat containers"}, [2]string{"space / enter (project)", "expand/collapse project; space on a member returns to its project"})
	}
	return help
}

// statsProgram runs StatsView on its own for `dn stats`.
type statsProgram struct{ v *StatsView }

// RunStats shows live stats inline until the user quits.
func RunStats(ctx context.Context, c dockerClient, key sortx.Key, reverse, totals bool) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	v := NewStatsView(ctx, c, key, reverse)
	v.SetTotals(totals)
	v.noHighlight = true
	v.SetSize(120, 28)
	_, err := tea.NewProgram(statsProgram{v}, programOptions(ctx)...).Run()
	return err
}

func (p statsProgram) Init() tea.Cmd { return p.v.Focus() }

func (p statsProgram) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.v.SetSize(msg.Width, msg.Height-2) // minus the status line and a spare
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return p, tea.Quit
		}
		return p, p.v.HandleKey(msg.String())
	default:
		return p, p.v.Update(msg)
	}
	return p, nil
}

func (p statsProgram) View() tea.View {
	keys, _ := shortcutHelp("/")
	status := render.Dim.Render(p.v.Status() + "  ·  " + keys + " sort · r reverse · t rates/totals · q quit")
	return tea.NewView(strings.Join([]string{status, p.v.View()}, "\n"))
}
