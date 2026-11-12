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
	statsStartedMsg struct{ ch <-chan docker.StatsUpdate }
	statsBatchMsg   []docker.StatsUpdate
	statsErrMsg     struct{ err error }
)

// StatsView shows live, sortable resource usage for running containers.
type StatsView struct {
	list[docker.Stats]
	ctx          context.Context
	client       dockerClient
	ch           <-chan docker.StatsUpdate
	data         map[string]docker.Stats
	watching     bool // WatchStats requested
	history      map[string]*statsHistory
	historyCache statsHistoryCache
	rates        map[string]docker.Rates
	totals       bool
}

func NewStatsView(ctx context.Context, c dockerClient, key sortx.Key, reverse bool) *StatsView {
	v := &StatsView{ctx: ctx, client: c, data: map[string]docker.Stats{}, history: map[string]*statsHistory{}, rates: map[string]docker.Rates{}}
	v.list = list[docker.Stats]{
		columns: render.StatsColumns,
		keys:    sortx.StatsKeys,
		sortCol: map[sortx.Key]int{sortx.Name: 0, sortx.CPU: 1, sortx.Mem: 3, sortx.Net: 6, sortx.IO: 7, sortx.PIDs: 8},
		key:     key,
		reverse: reverse,
		match:   func(s docker.Stats, f string) bool { return matches(f, s.Name, s.ID) },
		sort: func(items []docker.Stats, key sortx.Key, reverse bool) {
			if v.totals {
				sortx.Stats(items, key, reverse)
			} else {
				sortx.StatsByRates(items, key, reverse, v.rates)
			}
		},
		rows: func(s docker.Stats) []row {
			var cells []string
			if v.totals {
				cells = render.StatsRow(s)
			} else {
				cells = render.StatsRateRow(s, v.rates[s.ID])
			}
			return []row{{key: s.ID, cells: cells, id: s.ID, name: s.Name}}
		},
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
	v.rowCache = nil
}

func (v *StatsView) Title() string { return "Stats" }

func (v *StatsView) Init() tea.Cmd { return nil }

// Focus starts watching stats the first time the tab is shown: that opens one
// stream per running container, which isn't worth it if the tab never is.
func (v *StatsView) Focus() tea.Cmd {
	if v.watching {
		return nil
	}
	v.watching = true
	return func() tea.Msg {
		ch, err := v.client.WatchStats(v.ctx)
		if err != nil {
			return statsErrMsg{err}
		}
		return statsStartedMsg{ch}
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
		u, ok := <-ch
		if !ok {
			return nil
		}
		batch := statsBatchMsg{u}
		flush := time.After(statsFlushInterval)
		for {
			select {
			case u, ok := <-ch:
				if !ok {
					return batch
				}
				batch = append(batch, u)
			case <-flush:
				return batch
			}
		}
	}
}

func (v *StatsView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case statsStartedMsg:
		v.ch = msg.ch
		clear(v.data)
		clear(v.history)
		clear(v.rates)
		v.historyCache = statsHistoryCache{}
		v.setItems(nil, nil)
		return waitStats(v.ch)
	case statsBatchMsg:
		now := time.Now()
		for _, u := range msg {
			h := v.history[u.Stats.ID]
			if h != nil && u.Generation < h.generation {
				continue
			}
			if u.Gone {
				delete(v.data, u.Stats.ID)
				delete(v.history, u.Stats.ID)
				delete(v.rates, u.Stats.ID)
				if v.historyCache.history == h {
					v.historyCache = statsHistoryCache{}
				}
			} else {
				v.rates[u.Stats.ID] = u.Rates
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
		items := v.items[:0]
		for _, s := range v.data {
			items = append(items, s)
		}
		v.setItems(items, nil)
		return waitStats(v.ch)
	case statsErrMsg:
		v.err = msg.err
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
	for _, s := range v.data {
		cpu += s.CPUPercent
		mem += s.MemUsage
	}
	mode := "rates"
	if v.totals {
		mode = "totals"
	}
	return fmt.Sprintf("%d running · CPU %.1f%% · MEM %s · I/O: %s · %s",
		len(v.data), cpu, render.BinSize(mem), mode, sortLabel(v.key, v.reverse))
}

func (v *StatsView) Help() [][2]string {
	keys, sorts := shortcutHelp(" / ")
	return append([][2]string{{keys, "sort by " + sorts}, {"t", "toggle I/O rates / cumulative totals (sorting follows mode)"}}, v.list.Help()...)
}

// statsProgram runs StatsView on its own for `dn stats`.
type statsProgram struct{ v *StatsView }

// RunStats shows live stats inline until the user quits.
func RunStats(ctx context.Context, c dockerClient, key sortx.Key, reverse, totals bool) error {
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
