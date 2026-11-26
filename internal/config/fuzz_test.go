package config

import (
	"slices"
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
			if c.Name == "" || c.Key == "" || c.Template == nil || slices.Contains(ReservedKeys, c.Key) {
				t.Fatalf("invalid command kept: %+v", c)
			}
			if c.Scope != ScopeContainer && c.Scope != ScopeProject {
				t.Fatalf("command %q kept with scope %q", c.Name, c.Scope)
			}
			if _, err := c.Render(Vars{ID: "abc'def", Name: "web", Project: "p q"}); err != nil {
				t.Fatalf("command %q passed validation but failed to render: %v", c.Name, err)
			}
		}
	})
}
