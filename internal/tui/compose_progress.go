package tui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/docker/go-units"

	"dpad/internal/render"
)

const (
	maxComposeResources = 1024
	// Beyond this many images or containers, finished ones collapse into one
	// line so a large project's progress fits the screen.
	composeListLimit = 8
	// Active layers shown under each pulling image, like docker pull.
	composeLayerLimit = 4
)

type composeResource struct {
	kind, name, status, detail string
	// parent is the image a layer belongs to; only JSON progress reports it.
	parent         string
	current, total int64
}

type composeProgress struct {
	resources map[string]composeResource
	order     []string
	messages  []string
	omitted   bool
	// pulled is set once Compose reaches the up step. Plain progress does not
	// always print "Pull complete" for every layer, so earlier layer states are
	// stale after that.
	pulled bool
	// recreated and downloaded survive later status lines so the result can say
	// whether anything changed.
	recreated, downloaded map[string]bool
}

func composeOutputText(output string) string {
	return ansi.Strip(strings.ReplaceAll(strings.ReplaceAll(output, "\r\n", "\n"), "\r", "\n"))
}

// Compose repeats each resource's ID as its state changes. Keep only the
// latest state, including resources that leave the bounded raw tail.
func (p *composeProgress) apply(output string, final bool) {
	if p.resources == nil {
		p.resources, p.recreated, p.downloaded = map[string]composeResource{}, map[string]bool{}, map[string]bool{}
	}
	text := composeOutputText(output)
	p.messages = nil
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i == len(lines)-1 && line != "" && !final {
			break
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[2/2]") {
			p.pulled = true
		}
		if line == "" || strings.HasPrefix(line, "$") || strings.HasPrefix(line, "[1/2]") || strings.HasPrefix(line, "[2/2]") || line == "Update complete." {
			continue
		}
		resource, messages, ok := parseComposeLine(line)
		if !ok {
			for _, message := range messages {
				p.addMessage(message)
			}
			continue
		}
		p.record(resource)
	}
}

func (p *composeProgress) addMessage(line string) {
	// Compose prints a failed pull once per service that uses the image.
	if line == "" || len(p.messages) > 0 && p.messages[len(p.messages)-1] == line {
		return
	}
	if len(p.messages) == 8 {
		p.messages = p.messages[1:]
	}
	p.messages = append(p.messages, line)
}

func (p *composeProgress) record(resource composeResource) {
	key := resource.kind + ":" + resource.name
	switch {
	case resource.kind == "Container" && (resource.status == "Recreate" || resource.status == "Recreated" || resource.status == "Creating" || resource.status == "Created"):
		p.recreated[resource.name] = true
	case resource.kind == "Layer" && resource.status != "Already exists":
		p.downloaded[resource.name] = true
	}
	previous, exists := p.resources[key]
	if !exists {
		if len(p.order) == maxComposeResources {
			delete(p.resources, p.order[0])
			p.order = p.order[1:]
			p.omitted = true
		}
		p.order = append(p.order, key)
	}
	if resource.parent == "" {
		resource.parent = previous.parent
	}
	p.resources[key] = resource
}

// parseComposeLine reads one line of plain or JSON progress. A line that is
// not a resource update comes back as messages.
func parseComposeLine(line string) (composeResource, []string, bool) {
	if !strings.HasPrefix(line, "{") {
		r, ok := parseComposeResource(line)
		if !ok {
			return r, []string{line}, false
		}
		return r, nil, true
	}
	var event struct {
		ID       string `json:"id"`
		ParentID string `json:"parent_id"`
		Status   string `json:"status"`
		Text     string `json:"text"`
		Details  string `json:"details"`
		Current  int64  `json:"current"`
		Total    int64  `json:"total"`
		Error    bool   `json:"error"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return composeResource{}, []string{line}, false
	}
	if event.ID == "" {
		return composeResource{}, strings.Split(strings.TrimSpace(cmpOr(event.Message, event.Text)), "\n"), false
	}
	r := composeResource{kind: "Layer", name: event.ID, status: event.Text, detail: event.Details, current: event.Current, total: event.Total}
	if kind, name, ok := strings.Cut(event.ID, " "); ok {
		r.kind, r.name = kind, name
	}
	if _, image, ok := strings.Cut(event.ParentID, " "); ok {
		r.parent = image
	}
	if event.Status == "Error" {
		r.status = "Error"
		if r.detail == "" && event.Text != "Error" {
			r.detail = event.Text
		}
	}
	if r.detail == "0B" || r.detail == "1B" && r.status == "Extracting" {
		r.detail = ""
	}
	return r, nil, true
}

// plainComposeLog shows JSON progress lines the way plain progress prints
// them, so the raw log stays readable.
func plainComposeLog(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "{") {
			continue
		}
		r, messages, ok := parseComposeLine(strings.TrimSpace(line))
		switch {
		case !ok:
			lines[i] = strings.Join(messages, "\n")
		case r.kind == "Layer":
			lines[i] = strings.TrimSpace(r.name + " " + r.status + " " + r.detail)
		default:
			lines[i] = strings.TrimSpace(r.kind + " " + r.name + " " + r.status + " " + r.detail)
		}
	}
	return strings.Join(lines, "\n")
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func parseComposeResource(line string) (composeResource, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return composeResource{}, false
	}
	r := composeResource{kind: "Image", name: fields[0]}
	offset := 1
	switch fields[0] {
	case "Image", "Container", "Network", "Volume":
		if len(fields) < 3 {
			return composeResource{}, false
		}
		r.kind, r.name, offset = fields[0], fields[1], 2
	default:
		id := strings.TrimPrefix(fields[0], "sha256:")
		hex := len(id) >= 12 && len(id) <= 64
		for _, c := range id {
			if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
				hex = false
				break
			}
		}
		if hex {
			r.kind = "Layer"
		}
	}
	text := strings.Join(fields[offset:], " ")
	for _, status := range []string{
		"Pulling fs layer", "Verifying Checksum", "Download complete", "Already exists", "Pull complete",
		"Downloading", "Extracting", "Pulling", "Pulled", "Creating", "Created", "Recreate", "Recreated",
		"Starting", "Started", "Running", "Restarting", "Restarted", "Stopping", "Stopped", "Removing", "Removed",
		"Waiting", "Healthy", "Exited", "Error", "Failed", "Warning", "Skipped", "Interrupted",
	} {
		if text == status || strings.HasPrefix(text, status+" ") {
			r.status, r.detail = status, strings.TrimSpace(strings.TrimPrefix(text, status))
			if r.detail == "0B" {
				r.detail = ""
			}
			return r, true
		}
	}
	return composeResource{}, false
}

func (r composeResource) done() bool {
	switch r.kind {
	case "Layer":
		return r.status == "Already exists" || r.status == "Pull complete"
	case "Image":
		return r.status == "Pulled" || r.status == "Skipped"
	case "Container":
		return r.status == "Started" || r.status == "Running" || r.status == "Healthy" || r.status == "Removed"
	default:
		return r.status == "Created" || r.status == "Removed"
	}
}

func (r composeResource) failed() bool { return r.status == "Error" || r.status == "Failed" }

// layerDone also counts layers whose image has finished, and every layer once
// Compose has moved on to up: Compose does not always report "Pull complete".
func (p *composeProgress) layerDone(r composeResource) bool {
	if r.done() || p.pulled && !r.failed() {
		return true
	}
	parent, ok := p.resources["Image:"+r.parent]
	return ok && parent.done()
}

type composeRow struct {
	icon, name, status string
	style              lipgloss.Style
}

func (p *composeProgress) row(r composeResource, aborted bool) composeRow {
	row := composeRow{icon: "…", name: r.name, status: r.status, style: render.Accent}
	switch {
	case r.done():
		row.icon, row.style = "✓", lipgloss.NewStyle().Foreground(render.Green)
	case r.failed():
		row.icon, row.style = "✗", render.Error
	case r.status == "Warning":
		row.icon, row.style = "!", render.Warn
	case aborted:
		row.icon, row.style, row.status = "–", render.Dim, "Interrupted"
		return row
	}
	if r.detail != "" {
		row.status += " " + r.detail
	}
	return row
}

func (p *composeProgress) render(width int, state string) string {
	// Compose stops reporting resources when it aborts, so their last state is stale.
	aborted := state == "failed" || state == "cancelled" || state == "unknown"
	byKind := map[string][]composeResource{}
	for _, key := range p.order {
		r := p.resources[key]
		byKind[r.kind] = append(byKind[r.kind], r)
	}
	var lines []string
	if pull := p.renderPull(byKind["Image"], byKind["Layer"], width, aborted); len(pull) > 0 {
		lines = append(lines, render.Bold.Render("[1/2] Pull images"))
		lines = append(lines, pull...)
	}
	var start []string
	for _, kind := range []string{"Network", "Volume", "Container"} {
		start = append(start, p.renderGroup(kind, byKind[kind], width, aborted)...)
	}
	if len(start) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, render.Bold.Render("[2/2] Start updated services"))
		lines = append(lines, start...)
	}
	if len(p.messages) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "", render.Bold.Render("Messages"))
		}
		for _, message := range p.messages {
			if state == "failed" || state == "unknown" {
				message = render.Error.Render(message)
			}
			lines = append(lines, message)
		}
	}
	if p.omitted {
		lines = append(lines, render.Dim.Render("Earlier resource rows omitted."))
	}
	if len(lines) == 0 {
		return "Waiting for Compose progress…"
	}
	return strings.Join(lines, "\n")
}

func countLabel(n int, label string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", strings.TrimSuffix(label, "s"))
	}
	return fmt.Sprintf("%d %s", n, label)
}

// renderGroup lists one kind of resource. Past composeListLimit, finished
// rows collapse into a single count so the active and failed ones stay visible.
func (p *composeProgress) renderGroup(kind string, rows []composeResource, width int, aborted bool) []string {
	if len(rows) == 0 {
		return nil
	}
	complete, failed, interrupted := tally(rows, aborted)
	summary := []string{fmt.Sprintf("%ss · %d of %d done", kind, complete, len(rows))}
	summary = appendCounts(summary, failed, interrupted)
	lines := []string{"  " + render.Dim.Render(strings.Join(summary, " · "))}
	collapse := len(rows) > composeListLimit
	if collapse && complete > 0 {
		names := []string{}
		for _, r := range rows {
			if r.done() {
				names = append(names, r.name)
			}
		}
		text := fmt.Sprintf("%s ready: %s", countLabel(complete, strings.ToLower(kind)+"s"), strings.Join(names, ", "))
		green := lipgloss.NewStyle().Foreground(render.Green)
		lines = append(lines, ansi.Truncate("    "+green.Render("✓")+" "+text, width, "…"))
	}
	var shown []composeRow
	for _, r := range rows {
		if collapse && r.done() {
			continue
		}
		shown = append(shown, p.row(r, aborted))
	}
	return append(lines, alignRows(shown, width, "    ")...)
}

func tally(rows []composeResource, aborted bool) (complete, failed, interrupted int) {
	for _, r := range rows {
		switch {
		case r.done():
			complete++
		case r.failed():
			failed++
		case aborted:
			interrupted++
		}
	}
	return complete, failed, interrupted
}

func appendCounts(summary []string, failed, interrupted int) []string {
	if failed > 0 {
		summary = append(summary, fmt.Sprintf("%d failed", failed))
	}
	if interrupted > 0 {
		summary = append(summary, fmt.Sprintf("%d interrupted", interrupted))
	}
	return summary
}

func alignRows(rows []composeRow, width int, indent string) []string {
	nameWidth := 0
	for _, r := range rows {
		nameWidth = max(nameWidth, ansi.StringWidth(r.name))
	}
	nameWidth = min(nameWidth, max(width-len(indent)-24, 12))
	lines := make([]string, len(rows))
	for i, r := range rows {
		name := ansi.Truncate(r.name, nameWidth, "…")
		padding := strings.Repeat(" ", max(nameWidth-ansi.StringWidth(name), 0))
		lines[i] = ansi.Truncate(indent+r.style.Render(r.icon)+" "+name+padding+"  "+r.style.Render(r.status), width, "…")
	}
	return lines
}

// renderPull shows images like docker pull: one row per image, with the
// layers it is still downloading or extracting underneath and finished layers
// counted rather than listed.
func (p *composeProgress) renderPull(images, layers []composeResource, width int, aborted bool) []string {
	if len(images) == 0 && len(layers) == 0 {
		return nil
	}
	byImage := map[string][]composeResource{}
	var orphans []composeResource
	for _, l := range layers {
		if _, ok := p.resources["Image:"+l.parent]; ok {
			byImage[l.parent] = append(byImage[l.parent], l)
		} else {
			orphans = append(orphans, l)
		}
	}
	complete, failed, interrupted := tally(images, aborted)
	summary := []string{fmt.Sprintf("Images · %d of %d pulled", complete, len(images))}
	summary = appendCounts(summary, failed, interrupted)
	if n := len(p.downloaded); n > 0 {
		summary = append(summary, countLabel(n, "new layers"))
	}
	lines := []string{"  " + render.Dim.Render(strings.Join(summary, " · "))}
	collapse := len(images) > composeListLimit
	if collapse && complete > 0 {
		names := []string{}
		for _, r := range images {
			if r.done() {
				names = append(names, r.name)
			}
		}
		green := lipgloss.NewStyle().Foreground(render.Green)
		lines = append(lines, ansi.Truncate("    "+green.Render("✓")+" "+countLabel(complete, "images")+" pulled: "+strings.Join(names, ", "), width, "…"))
	}
	var shown []composeRow
	var under [][]string
	for _, img := range images {
		if collapse && img.done() {
			continue
		}
		row := p.row(img, aborted)
		active := []composeResource{}
		done, total := 0, len(byImage[img.name])
		var current, size int64
		for _, l := range byImage[img.name] {
			if p.layerDone(l) {
				done++
				continue
			}
			current += l.current
			size += l.total
			if !aborted && l.status != "Pulling fs layer" && l.status != "Waiting" {
				active = append(active, l)
			}
		}
		if total > 0 && !img.done() && !img.failed() && !aborted {
			row.status = fmt.Sprintf("%s · %d of %d layers", row.status, done, total)
			if size > 0 {
				row.status += " · " + units.HumanSize(float64(current)) + " / " + units.HumanSize(float64(size))
			}
		}
		shown = append(shown, row)
		under = append(under, p.layerLines(active, width))
	}
	rows := alignRows(shown, width, "    ")
	for i, line := range rows {
		lines = append(lines, line)
		lines = append(lines, under[i]...)
	}
	if len(orphans) > 0 {
		// Plain progress does not say which image a layer belongs to.
		done, active := 0, []composeResource{}
		for _, l := range orphans {
			if p.layerDone(l) {
				done++
			} else if !aborted && l.status != "Pulling fs layer" && l.status != "Waiting" {
				active = append(active, l)
			}
		}
		lines = append(lines, "  "+render.Dim.Render(fmt.Sprintf("Layers · %d of %d done", done, len(orphans))))
		lines = append(lines, p.layerLines(active, width)...)
	}
	return lines
}

func (p *composeProgress) layerLines(active []composeResource, width int) []string {
	// Moving layers first: they are what docker pull's bars show.
	slices.SortStableFunc(active, func(a, b composeResource) int { return layerRank(a) - layerRank(b) })
	var lines []string
	for i, l := range active {
		if i == composeLayerLimit {
			lines = append(lines, render.Dim.Render(fmt.Sprintf("        +%d more layers", len(active)-i)))
			break
		}
		text := l.status
		switch {
		case l.total > 0 && (l.status == "Downloading" || l.status == "Extracting"):
			text += " " + progressBar(l.current, l.total, 20) + " " + units.HumanSize(float64(l.current)) + " / " + units.HumanSize(float64(l.total))
		case l.detail != "" && l.status != "Extracting":
			text += " " + l.detail
		}
		style := render.Dim
		if l.failed() {
			style = render.Error
		}
		lines = append(lines, ansi.Truncate("        "+render.Dim.Render(l.name)+"  "+style.Render(text), width, "…"))
	}
	return lines
}

func layerRank(l composeResource) int {
	switch l.status {
	case "Error", "Failed":
		return 0
	case "Downloading", "Extracting":
		return 1
	}
	return 2
}

func progressBar(current, total int64, width int) string {
	filled := int(min(current, total) * int64(width) / max(total, 1))
	return "[" + strings.Repeat("=", max(filled-1, 0)) + strings.Repeat(">", min(filled, 1)) + strings.Repeat(" ", width-filled) + "]"
}

// changes reports what an update did, for its result summary.
func (p *composeProgress) changes() (recreated, downloaded int) {
	return len(p.recreated), len(p.downloaded)
}

// failure picks the most useful single line from a failed update: Compose's
// own error over dpad's "pulling failed: exit status 1" trailer.
func (p *composeProgress) failure() string {
	for _, m := range slices.Backward(p.messages) {
		if strings.HasPrefix(strings.ToLower(m), "error") {
			return m
		}
	}
	for _, key := range p.order {
		if r := p.resources[key]; r.failed() && r.detail != "" {
			return r.detail
		}
	}
	if len(p.messages) > 0 {
		return p.messages[len(p.messages)-1]
	}
	return ""
}
