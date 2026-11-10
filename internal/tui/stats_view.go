package tui

import (
	"context"
	"fmt"
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
	ctx     context.Context
	client  *docker.Client
	ch      <-chan docker.StatsUpdate
	data    map[string]docker.Stats
	key     string
	reverse bool
	cur     cursor
	page    int
	filter  string // last filter passed to View, for navigation
	started bool
	noSel   bool // hide the row highlight (standalone `dn stats`)
	err     error
}

func NewStatsView(ctx context.Context, c *docker.Client, key string, reverse bool) *StatsView {
	return &StatsView{ctx: ctx, client: c, data: map[string]docker.Stats{}, key: key, reverse: reverse, page: 10}
}

func (v *StatsView) Title() string { return "Stats" }

func (v *StatsView) Init() tea.Cmd {
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
		v.ch, v.started = msg.ch, true
		return waitStats(v.ch)
	case statsBatchMsg:
		for _, u := range msg {
			if u.Gone {
				delete(v.data, u.Stats.ID)
			} else {
				v.data[u.Stats.ID] = u.Stats
			}
		}
		return waitStats(v.ch)
	case statsErrMsg:
		v.err = msg.err
	}
	return nil
}

func (v *StatsView) items(filter string) []docker.Stats {
	out := make([]docker.Stats, 0, len(v.data))
	for _, s := range v.data {
		if matches(filter, s.Name, s.ID) {
			out = append(out, s)
		}
	}
	sortx.Stats(out, v.key, v.reverse)
	return out
}

func statsKeys(items []docker.Stats) []string {
	keys := make([]string, len(items))
	for i, s := range items {
		keys[i] = s.ID
	}
	return keys
}

var statsSortKeys = map[string]string{"c": "cpu", "m": "mem", "n": "net", "i": "io", "p": "pids", "a": "name"}

func (v *StatsView) HandleKey(key string) (tea.Cmd, bool) {
	if k, ok := statsSortKeys[key]; ok {
		v.key = k
		return nil, true
	}
	switch key {
	case "s":
		v.key = sortx.Next(v.key, sortx.StatsKeys)
	case "r":
		v.reverse = !v.reverse
	default:
		return nil, v.cur.navKey(statsKeys(v.items(v.filter)), key, v.page)
	}
	return nil, true
}

func (v *StatsView) View(width, height int, filter string) string {
	switch {
	case v.err != nil:
		return render.StateStyle("dead", 0).Render("error: " + v.err.Error())
	case !v.started:
		return render.Dim.Render("connecting…")
	case len(v.data) == 0:
		return render.Dim.Render("no running containers")
	}
	v.filter = filter
	items := v.items(filter)
	v.page = max(height-2, 1)
	sel := v.cur.index(statsKeys(items))
	if v.noSel {
		sel = -1
	}
	rows := make([][]string, len(items))
	for i, s := range items {
		rows[i] = render.StatsRow(s)
	}
	return tableWindow(render.Table{
		Headers:  render.StatsHeaders,
		Rows:     rows,
		Align:    render.StatsAlign,
		MaxWidth: render.StatsMax,
		Selected: sel,
		Width:    width,
	}, height)
}

// Selected returns the highlighted row, resolved the same way View does so a
// cursor on a filtered-out container never targets something off screen.
func (v *StatsView) Selected() (string, string, bool) {
	items := v.items(v.filter)
	if len(items) == 0 {
		return "", "", false
	}
	s := items[v.cur.index(statsKeys(items))]
	return s.ID, s.Name, true
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
	return [][2]string{
		{"c m n i p a", "sort by cpu / mem / net / io / pids / name"},
		{"s", "cycle sort key"},
		{"r", "reverse sort"},
	}
}

// statsProgram runs StatsView on its own for `dn stats`.
type statsProgram struct {
	v             *StatsView
	width, height int
}

// RunStats shows live stats inline until the user quits.
func RunStats(ctx context.Context, c *docker.Client, key string, reverse bool) error {
	v := NewStatsView(ctx, c, key, reverse)
	v.noSel = true
	_, err := tea.NewProgram(statsProgram{v: v, width: 120, height: 30}, tea.WithContext(ctx)).Run()
	return err
}

func (p statsProgram) Init() tea.Cmd { return p.v.Init() }

func (p statsProgram) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return p, tea.Quit
		}
		cmd, _ := p.v.HandleKey(msg.String())
		return p, cmd
	default:
		return p, p.v.Update(msg)
	}
	return p, nil
}

func (p statsProgram) View() string {
	status := render.Dim.Render(p.v.Status() + "  ·  c/m/n/i/p/a sort · r reverse · q quit")
	body := p.v.View(p.width, p.height-2, "")
	return strings.Join([]string{status, body}, "\n")
}
