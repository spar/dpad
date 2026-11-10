package tui

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
	ctx      context.Context
	client   *docker.Client
	ch       <-chan docker.StatsUpdate
	data     map[string]docker.Stats
	watching bool // WatchStats requested
}

func NewStatsView(ctx context.Context, c *docker.Client, key sortx.Key, reverse bool) *StatsView {
	return &StatsView{ctx: ctx, client: c, data: map[string]docker.Stats{}, list: list[docker.Stats]{
		columns: render.StatsColumns,
		keys:    sortx.StatsKeys,
		key:     key,
		reverse: reverse,
		match:   func(s docker.Stats, f string) bool { return matches(f, s.Name, s.ID) },
		sort:    sortx.Stats,
		rows: func(s docker.Stats) []row {
			return []row{{key: s.ID, cells: render.StatsRow(s), id: s.ID, name: s.Name}}
		},
		loading: "connecting…",
		empty:   "no running containers",
		page:    10,
	}}
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
		v.ch, v.loaded = msg.ch, true
		return waitStats(v.ch)
	case statsBatchMsg:
		for _, u := range msg {
			if u.Gone {
				delete(v.data, u.Stats.ID)
			} else {
				v.data[u.Stats.ID] = u.Stats
			}
		}
		v.items = slices.Collect(maps.Values(v.data))
		return waitStats(v.ch)
	case statsErrMsg:
		v.err = msg.err
	}
	return nil
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
	for _, sc := range statsShortcuts {
		if sc.key == key {
			v.key = sc.sort
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
	return fmt.Sprintf("%d running · CPU %.1f%% · MEM %s · %s",
		len(v.data), cpu, render.BinSize(mem), sortLabel(v.key, v.reverse))
}

func (v *StatsView) Help() [][2]string {
	keys, sorts := shortcutHelp(" / ")
	return append([][2]string{{keys, "sort by " + sorts}}, v.list.Help()...)
}

// statsProgram runs StatsView on its own for `dn stats`.
type statsProgram struct {
	v             *StatsView
	width, height int
}

// RunStats shows live stats inline until the user quits.
func RunStats(ctx context.Context, c *docker.Client, key sortx.Key, reverse bool) error {
	v := NewStatsView(ctx, c, key, reverse)
	v.noHighlight = true
	_, err := tea.NewProgram(statsProgram{v: v, width: 120, height: 30}, tea.WithContext(ctx)).Run()
	return err
}

func (p statsProgram) Init() tea.Cmd { return p.v.Focus() }

func (p statsProgram) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
	case tea.KeyMsg:
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

func (p statsProgram) View() string {
	keys, _ := shortcutHelp("/")
	status := render.Dim.Render(p.v.Status() + "  ·  " + keys + " sort · r reverse · q quit")
	body := p.v.View(p.width, p.height-2, "")
	return strings.Join([]string{status, body}, "\n")
}
