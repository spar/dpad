package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"dpad/internal/render"
)

const maxComposeResources = 256

type composeResource struct {
	kind, name, status, detail string
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

// Compose plain progress repeats each resource's ID as its state changes. Keep
// only the latest state, including resources that leave the bounded raw tail.
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
		resource, ok := parseComposeResource(line)
		if !ok {
			// Compose prints a failed pull once per service that uses the image.
			if len(p.messages) > 0 && p.messages[len(p.messages)-1] == line {
				continue
			}
			if len(p.messages) == 8 {
				p.messages = p.messages[1:]
			}
			p.messages = append(p.messages, line)
			continue
		}
		key := resource.kind + ":" + resource.name
		switch {
		case resource.kind == "Container" && (resource.status == "Recreate" || resource.status == "Recreated" || resource.status == "Creating" || resource.status == "Created"):
			p.recreated[resource.name] = true
		case resource.kind == "Layer" && resource.status != "Already exists":
			p.downloaded[resource.name] = true
		}
		if _, exists := p.resources[key]; !exists {
			if len(p.order) == maxComposeResources {
				delete(p.resources, p.order[0])
				p.order = p.order[1:]
				p.omitted = true
			}
			p.order = append(p.order, key)
		}
		p.resources[key] = resource
	}
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

func (p *composeProgress) render(width int, state string) string {
	// Compose stops reporting resources when it aborts, so their last state is stale.
	aborted := state == "failed" || state == "cancelled" || state == "unknown"
	var lines []string
	for _, group := range []struct {
		kinds []string
		title string
	}{
		{[]string{"Image", "Layer"}, "[1/2] Pull images"},
		{[]string{"Network", "Volume", "Container"}, "[2/2] Start updated services"},
	} {
		var rows []composeResource
		for _, kind := range group.kinds {
			for _, key := range p.order {
				if r := p.resources[key]; r.kind == kind {
					if r.kind == "Layer" && p.pulled && !r.done() && r.status != "Error" && r.status != "Failed" {
						r.status, r.detail = "Pull complete", ""
					}
					rows = append(rows, r)
				}
			}
		}
		if len(rows) == 0 {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, render.Bold.Render(group.title))
		for _, kind := range group.kinds {
			complete, active, failed, warnings, interrupted, nameWidth := 0, 0, 0, 0, 0, 0
			for _, r := range rows {
				if r.kind != kind {
					continue
				}
				switch {
				case r.done():
					complete++
				case r.status == "Error" || r.status == "Failed":
					failed++
				case r.status == "Warning":
					warnings++
				case aborted:
					interrupted++
				default:
					active++
				}
				nameWidth = max(nameWidth, ansi.StringWidth(r.name))
			}
			if complete+active+failed+warnings+interrupted == 0 {
				continue
			}
			summary := []string{kind + "s"}
			for _, count := range []struct {
				n     int
				label string
			}{{complete, "complete"}, {active, "in progress"}, {failed, "failed"}, {warnings, "warning"}, {interrupted, "interrupted"}} {
				if count.n > 0 {
					summary = append(summary, fmt.Sprintf("%d %s", count.n, count.label))
				}
			}
			lines = append(lines, "  "+render.Dim.Render(strings.Join(summary, " · ")))
			nameWidth = min(nameWidth, max(width-28, 12))
			for _, r := range rows {
				if r.kind != kind {
					continue
				}
				icon, style := "…", render.Accent
				if r.done() {
					icon, style = "✓", lipgloss.NewStyle().Foreground(render.Green)
				}
				if r.status == "Error" || r.status == "Failed" {
					icon, style = "✗", render.Error
				}
				if r.status == "Warning" {
					icon, style = "!", render.Warn
				}
				status := r.status
				if r.detail != "" {
					status += " " + r.detail
				}
				if aborted && icon == "…" {
					icon, style, status = "–", render.Dim, "Interrupted"
				}
				name := r.name
				if r.kind == "Layer" {
					name = render.Dim.Render(name)
				}
				padding := strings.Repeat(" ", max(nameWidth-ansi.StringWidth(r.name), 0))
				lines = append(lines, "    "+style.Render(icon)+" "+name+padding+"  "+style.Render(status))
			}
		}
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
		if r := p.resources[key]; (r.status == "Error" || r.status == "Failed") && r.detail != "" {
			return r.detail
		}
	}
	if len(p.messages) > 0 {
		return p.messages[len(p.messages)-1]
	}
	return ""
}
