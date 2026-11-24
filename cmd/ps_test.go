package cmd

import (
	"maps"
	"slices"
	"testing"
)

func TestParseFilters(t *testing.T) {
	got, err := parseFilters([]string{"status=exited", "label=env=prod", "Status=dead", "name="})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"status": {"exited", "dead"}, "label": {"env=prod"}, "name": {""}}
	if !maps.EqualFunc(got, want, slices.Equal) {
		t.Errorf("parseFilters = %v, want %v", got, want)
	}
	for _, bad := range []string{"exited", "=x"} {
		if _, err := parseFilters([]string{bad}); err == nil {
			t.Errorf("parseFilters(%q) accepted a filter without a name", bad)
		}
	}
}
