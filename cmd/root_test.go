package cmd

import (
	"slices"
	"testing"
)

func TestDockerArgs(t *testing.T) {
	tests := []struct {
		args     []string
		anywhere bool
		want     []string
	}{
		{[]string{"ps", "--no-color", "-q"}, true, []string{"ps", "-q"}},
		{[]string{"stats", "web", "--no-color=true"}, true, []string{"stats", "web"}},
		{[]string{"logs", "--", "--no-color"}, true, []string{"logs", "--", "--no-color"}},
		{[]string{"--no-color", "network", "ls"}, false, []string{"network", "ls"}},
		{[]string{"exec", "web", "ls", "--no-color"}, false, []string{"exec", "web", "ls", "--no-color"}},
	}
	for _, tt := range tests {
		if got := dockerArgs(tt.args, tt.anywhere); !slices.Equal(got, tt.want) {
			t.Errorf("dockerArgs(%q, %v) = %q, want %q", tt.args, tt.anywhere, got, tt.want)
		}
	}
}
