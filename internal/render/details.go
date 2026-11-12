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
	var b strings.Builder
	section := func(title string) { fmt.Fprintln(&b, "\n"+Header.Render(title)) }
	field := func(label, value string) {
		if value == "" {
			value = "—"
		}
		fmt.Fprintf(&b, "%s  %s\n", Bold.Render(label+":"), value)
	}
	field("Name", strings.TrimPrefix(c.Name, "/"))
	field("ID", c.ID)
	field("Created", c.Created)
	if c.State != nil {
		s := c.State
		section("State")
		field("Status", StateStyle(string(s.Status), s.ExitCode).Render(string(s.Status)))
		field("Exit code", strconv.Itoa(s.ExitCode))
		field("OOM killed", strconv.FormatBool(s.OOMKilled))
		field("Started", detailsTime(s.StartedAt))
		field("Finished", detailsTime(s.FinishedAt))
		if s.Error != "" {
			field("Error", s.Error)
		}
	}
	field("Restarts", strconv.Itoa(c.RestartCount))
	section("Configuration")
	if c.Config != nil {
		field("Image", c.Config.Image)
		field("Entrypoint", commandArgs(c.Config.Entrypoint))
		field("Command", commandArgs(c.Config.Cmd))
		field("Working directory", c.Config.WorkingDir)
		field("User", c.Config.User)
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
		field("Restart policy", policy)
	}
	section("Health")
	if c.State == nil || c.State.Health == nil {
		field("Status", "no health check")
	} else {
		h := c.State.Health
		field("Status", HealthBadge(string(h.Status)))
		field("Failing streak", strconv.Itoa(h.FailingStreak))
		for _, result := range h.Log {
			if result == nil {
				continue
			}
			field(result.Start.Format(time.RFC3339), fmt.Sprintf("exit %d", result.ExitCode))
			if output := strings.TrimSpace(result.Output); output != "" {
				fmt.Fprintln(&b, "  "+strings.ReplaceAll(output, "\n", "\n  "))
			}
		}
	}
	section("Ports")
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
	section("Networks")
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
	section("Mounts")
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
