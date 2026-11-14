package render

import (
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
)

// ContainerDetails renders inspect data without exposing environment variables.
func ContainerDetails(c container.InspectResponse) string {
	var b detailBuilder
	b.field("Name", strings.TrimPrefix(c.Name, "/"))
	b.field("ID", c.ID)
	b.field("Created", c.Created)
	if c.State != nil {
		s := c.State
		b.section("State")
		b.field("Status", StateStyle(string(s.Status), s.ExitCode).Render(string(s.Status)))
		b.field("Exit code", strconv.Itoa(s.ExitCode))
		b.field("OOM killed", strconv.FormatBool(s.OOMKilled))
		b.field("Started", detailsTime(s.StartedAt))
		b.field("Finished", detailsTime(s.FinishedAt))
		if s.Error != "" {
			b.field("Error", s.Error)
		}
	}
	b.field("Restarts", strconv.Itoa(c.RestartCount))
	b.section("Configuration")
	if c.Config != nil {
		b.field("Image", c.Config.Image)
		b.field("Entrypoint", commandArgs(c.Config.Entrypoint))
		b.field("Command", commandArgs(c.Config.Cmd))
		b.field("Working directory", c.Config.WorkingDir)
		b.field("User", c.Config.User)
	}
	if c.HostConfig != nil {
		p := c.HostConfig.RestartPolicy
		policy := string(p.Name)
		if policy == "" {
			policy = "no"
		}
		if p.MaximumRetryCount > 0 {
			policy += fmt.Sprintf(" (max retries: %d)", p.MaximumRetryCount)
		}
		b.field("Restart policy", policy)
	}
	b.section("Health")
	if c.State == nil || c.State.Health == nil {
		b.field("Status", "no health check")
	} else {
		h := c.State.Health
		b.field("Status", HealthBadge(string(h.Status)))
		b.field("Failing streak", strconv.Itoa(h.FailingStreak))
		for _, result := range h.Log {
			if result == nil {
				continue
			}
			b.field(result.Start.Format(time.RFC3339), fmt.Sprintf("exit %d", result.ExitCode))
			if output := strings.TrimSpace(result.Output); output != "" {
				fmt.Fprintln(&b, "  "+strings.ReplaceAll(output, "\n", "\n  "))
			}
		}
	}
	b.section("Ports")
	var ports []string
	if c.NetworkSettings != nil {
		for port, bindings := range c.NetworkSettings.Ports {
			if len(bindings) == 0 {
				ports = append(ports, port.String()+" (not published)")
			}
			for _, binding := range bindings {
				host := "*"
				if binding.HostIP.IsValid() {
					host = binding.HostIP.String()
				}
				ports = append(ports, net.JoinHostPort(host, binding.HostPort)+" → "+port.String())
			}
		}
	}
	if c.Config != nil {
		for port := range c.Config.ExposedPorts {
			if c.NetworkSettings != nil {
				if _, ok := c.NetworkSettings.Ports[port]; ok {
					continue
				}
			}
			ports = append(ports, port.String()+" (not published)")
		}
	}
	slices.Sort(ports)
	if len(ports) == 0 {
		ports = []string{"none"}
	}
	fmt.Fprintln(&b, strings.Join(ports, "\n"))
	b.section("Networks")
	var networks []string
	if c.NetworkSettings != nil {
		for name, endpoint := range c.NetworkSettings.Networks {
			line := name
			if endpoint != nil {
				if endpoint.IPAddress.IsValid() {
					line += "  " + endpoint.IPAddress.String()
				}
				if endpoint.GlobalIPv6Address.IsValid() {
					line += "  " + endpoint.GlobalIPv6Address.String()
				}
			}
			networks = append(networks, line)
		}
	}
	slices.Sort(networks)
	if len(networks) == 0 {
		networks = []string{"none"}
	}
	fmt.Fprintln(&b, strings.Join(networks, "\n"))
	b.section("Mounts")
	if len(c.Mounts) == 0 {
		fmt.Fprintln(&b, "none")
	}
	for _, m := range c.Mounts {
		source := m.Source
		if m.Name != "" {
			source = m.Name
		}
		mode := "ro"
		if m.RW {
			mode = "rw"
		}
		fmt.Fprintf(&b, "%s  %s → %s (%s)\n", m.Type, source, m.Destination, mode)
	}
	return strings.TrimRight(b.String(), "\n")
}

func commandArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = strconv.Quote(arg)
	}
	return strings.Join(quoted, " ")
}

func detailsTime(value string) string {
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil && t.IsZero() {
		return ""
	}
	return value
}

// detailBuilder keeps the inspect screens' text layout consistent.
type detailBuilder struct{ strings.Builder }

func (b *detailBuilder) field(label, value string) {
	if value == "" {
		value = "—"
	}
	fmt.Fprintf(b, "%s  %s\n", Bold.Render(label+":"), value)
}

func (b *detailBuilder) section(title string) {
	fmt.Fprintln(b, "\n"+Header.Render(title))
}

func (b *detailBuilder) labels(labels map[string]string) {
	b.section("Labels")
	lines := make([]string, 0, len(labels))
	for key, value := range labels {
		lines = append(lines, key+"="+value)
	}
	slices.Sort(lines)
	if len(lines) == 0 {
		lines = append(lines, "none")
	}
	fmt.Fprintln(b, strings.Join(lines, "\n"))
}
