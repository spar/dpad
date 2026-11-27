package tui

import (
	"fmt"
	"slices"

	"github.com/moby/moby/api/types/container"

	"dpad/internal/docker"
	"dpad/internal/render"
	"dpad/internal/sortx"
)

type statsMembership struct {
	project, service, name, image string
	running                       bool
}
type statsProject struct {
	name      string
	members   []docker.Stats
	summary   docker.Stats
	rates     docker.Rates
	available int
}

func statsProjectKey(name string) string { return "stats-project:" + name }

func (v *StatsView) SetGrouped(grouped bool) {
	if v.grouped == grouped {
		return
	}
	id, _, _ := v.Selected()
	rows, keys := v.visible()
	if len(rows) > 0 {
		if p := v.projects[keys[v.cur.index(keys)]]; p != nil && len(p.members) > 0 {
			v.sortSamples(p.members, false)
			id = p.members[0].ID
		}
	}
	v.grouped = grouped
	v.SetTotals(v.totals)
	v.rebuildItems()
	if id != "" {
		v.cur.key = id
		if grouped {
			key := statsProjectKey(v.membership[id].project)
			if v.projects[key] != nil && !v.expanded[key] {
				v.cur.key = key
			}
		}
	}
}

func (v *StatsView) updateMembership(msg containersMsg) {
	v.membershipErr = msg.err
	if msg.err != nil {
		return
	}
	membership := make(map[string]statsMembership, len(msg.items))
	for _, c := range msg.items {
		// A restarting container has no stats stream between attempts, so
		// waiting for its sample would blank a crash-looping project for good.
		active := c.State == container.StateRunning || c.State == container.StatePaused
		membership[c.ID] = statsMembership{project: c.Labels[composeProjectLabel], service: c.Labels[composeServiceLabel], name: docker.ContainerName(c.Names), image: c.Image, running: active}
	}
	v.membership = membership
	if v.requested {
		v.rebuildItems()
	}
}

func (v *StatsView) sampleAvailable(id string) bool {
	_, ok := v.data[id]
	return ok && v.health[id].err == nil
}

func (v *StatsView) rebuildItems() {
	loaded := v.loaded
	items := v.items[:0]
	if !v.grouped {
		for _, s := range v.data {
			items = append(items, s)
		}
	} else {
		if v.projects == nil {
			v.projects = make(map[string]*statsProject)
			v.projectRates = make(map[string]docker.Rates)
			v.expanded = make(map[string]bool)
		}
		clear(v.projects)
		clear(v.projectRates)
		v.groupMembers = 0
		add := func(s docker.Stats) {
			m := v.membership[s.ID]
			v.groupMembers++
			if m.project == "" {
				items = append(items, s)
				v.projectRates[s.ID] = v.rates[s.ID]
				return
			}
			key := statsProjectKey(m.project)
			p := v.projects[key]
			if p == nil {
				p = &statsProject{name: m.project, summary: docker.Stats{ID: key, Name: m.project}, rates: docker.Rates{NetValid: true, BlockValid: true}}
				v.projects[key] = p
			}
			p.members = append(p.members, s)
			if !v.sampleAvailable(s.ID) {
				p.rates.NetValid = false
				p.rates.BlockValid = false
				return
			}
			p.available++
			p.summary.CPUPercent += s.CPUPercent
			p.summary.MemUsage += s.MemUsage
			p.summary.PIDs += s.PIDs
			p.summary.NetRx += s.NetRx
			p.summary.NetTx += s.NetTx
			p.summary.BlockRead += s.BlockRead
			p.summary.BlockWrite += s.BlockWrite
			r := v.rates[s.ID]
			p.rates.NetValid = p.rates.NetValid && r.NetValid
			p.rates.BlockValid = p.rates.BlockValid && r.BlockValid
			p.rates.NetRx += r.NetRx
			p.rates.NetTx += r.NetTx
			p.rates.BlockRead += r.BlockRead
			p.rates.BlockWrite += r.BlockWrite
		}
		for _, s := range v.data {
			add(s)
		}
		// Running members with no first sample yet keep the project incomplete.
		for id, m := range v.membership {
			if _, ok := v.data[id]; !ok && m.running && m.project != "" {
				add(docker.Stats{ID: id, Name: m.name})
			}
		}
		for key, p := range v.projects {
			items = append(items, p.summary)
			v.projectRates[key] = p.rates
		}
		for key := range v.expanded {
			if v.projects[key] == nil {
				delete(v.expanded, key)
			}
		}
		m := v.membership[v.cur.key]
		key := statsProjectKey(m.project)
		_, sampled := v.data[v.cur.key]
		if v.projects[key] != nil && (!v.expanded[key] || (!sampled && !m.running)) {
			v.cur.key = key
		}
	}
	v.setItems(items, nil)
	v.loaded = loaded || len(items) > 0
}

func (v *StatsView) sortSamples(items []docker.Stats, projects bool) {
	rates := v.rates
	if projects {
		rates = v.projectRates
	}
	if v.totals {
		sortx.Stats(items, v.key, v.reverse)
	} else {
		sortx.StatsByRates(items, v.key, v.reverse, rates)
	}
	if projects && v.key != sortx.Name {
		complete := func(s docker.Stats) bool {
			if p := v.projects[s.ID]; p != nil {
				return p.available == len(p.members)
			}
			return v.sampleAvailable(s.ID)
		}
		slices.SortStableFunc(items, func(a, b docker.Stats) int {
			if complete(a) == complete(b) {
				return 0
			}
			if complete(a) {
				return -1
			}
			return 1
		})
	}
}

func (v *StatsView) statsMatches(s docker.Stats, filter string) bool {
	if v.grouped {
		if p := v.projects[s.ID]; p != nil {
			if matches(filter, p.name) {
				return true
			}
			for _, c := range p.members {
				m := v.membership[c.ID]
				if matches(filter, c.Name, c.ID, m.service) {
					return true
				}
			}
			return false
		}
	}
	m := v.membership[s.ID]
	return matches(filter, s.Name, s.ID, m.project, m.service)
}

func (v *StatsView) statsCells(s docker.Stats) []string {
	if !v.sampleAvailable(s.ID) {
		return render.UnavailableStatsRow(s)
	}
	if v.totals {
		return render.StatsRow(s)
	}
	return render.StatsRateRow(s, v.rates[s.ID])
}

func (v *StatsView) statsRows(s docker.Stats) []row {
	if !v.grouped || v.projects[s.ID] == nil {
		cells := v.statsCells(s)
		cells[0] = render.ImageIcon(v.membership[s.ID].image) + cells[0]
		return []row{{key: s.ID, id: s.ID, name: s.Name, cells: cells}}
	}
	p := v.projects[s.ID]
	marker := "▸ "
	if v.expanded[s.ID] {
		marker = "▾ "
	}
	var cells []string
	if p.available != len(p.members) {
		cells = render.UnavailableStatsRow(p.summary)
	} else if v.totals {
		cells = render.StatsRow(p.summary)
	} else {
		cells = render.StatsRateRow(p.summary, p.rates)
	}
	cells[0] = render.Bold.Render(fmt.Sprintf("%s%s (%d/%d)", marker, p.name, p.available, len(p.members)))
	if p.available == len(p.members) {
		cells[3] = render.BinSize(p.summary.MemUsage)
		cells[4] = "—"
		cells[5] = ""
	}
	rows := []row{{key: s.ID, cells: cells}}
	if !v.expanded[s.ID] {
		return rows
	}
	v.sortSamples(p.members, false)
	for _, c := range p.members {
		cells := v.statsCells(c)
		service := v.membership[c.ID].service
		label := c.Name
		if service != "" {
			label = service + " · " + label
		}
		cells[0] = "  " + render.ImageIcon(v.membership[c.ID].image) + label
		rows = append(rows, row{key: c.ID, id: c.ID, name: c.Name, cells: cells})
	}
	return rows
}

func (v *StatsView) selectedStatsProject() *statsProject {
	if !v.grouped {
		return nil
	}
	rows, keys := v.visible()
	if len(rows) == 0 {
		return nil
	}
	return v.projects[keys[v.cur.index(keys)]]
}
func (v *StatsView) toggleStatsProject(includeChild bool) bool {
	if !v.grouped {
		return false
	}
	rows, keys := v.visible()
	if len(rows) == 0 {
		return false
	}
	key := keys[v.cur.index(keys)]
	if v.projects[key] == nil && includeChild {
		key = statsProjectKey(v.membership[key].project)
	}
	if v.projects[key] == nil {
		return false
	}
	v.expanded[key] = !v.expanded[key]
	v.cur.key = key
	v.rowCache = nil
	return true
}
