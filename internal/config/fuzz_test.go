package config

import (
	"slices"
	"strings"
	"testing"
)

func FuzzParse(f *testing.F) {
	f.Add([]byte("theme: dracula\nmouse: false\n"))
	f.Add([]byte(`customCommands:
  - name: Shell
    key: S
    command: docker exec -it {{.ID}} sh
  - name: Up
    key: U
    scope: project
    command: docker compose -p {{.Project}} up -d
`))
	f.Add([]byte("customCommands:\n  - name: Bad\n    key: x\n    command: '{{.Nope}}'\n"))
	f.Add([]byte("composeProjects:\n  web:\n    env:\n      A: b\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, _ := Parse(data)
		for _, c := range cfg.CustomCommands {
			if c.Name == "" || c.Key == "" || c.parts == nil || slices.Contains(ReservedKeys, c.Key) {
				t.Fatalf("invalid command kept: %+v", c)
			}
			if c.Scope != ScopeContainer && c.Scope != ScopeProject {
				t.Fatalf("command %q kept with scope %q", c.Name, c.Scope)
			}
			if got := c.Render(Vars{}); strings.Contains(got, "{{") {
				t.Fatalf("command %q rendered with a placeholder left: %q", c.Name, got)
			}
		}
	})
}
