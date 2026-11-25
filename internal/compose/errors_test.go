package compose

import (
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestConfigErrorNamesMissingVariableInsteadOfRawStderr(t *testing.T) {
	stderr := strings.Repeat(`time="2026-11-25T12:28:12+11:00" level=warning msg="The \"RUNNER_STACK\" variable is not set. Defaulting to a blank string."`+"\n", 10) +
		"error while interpolating name: required variable RUNNER_STACK is missing a value: Run this through ./runners <stack> so RUNNER_STACK is set\n"
	err := configError(stderr, &exec.ExitError{})
	var missing *MissingVariableError
	if !errors.As(err, &missing) || missing.Name != "RUNNER_STACK" {
		t.Fatalf("got %v, want a missing RUNNER_STACK", err)
	}
	if got, want := err.Error(), "needs RUNNER_STACK: Run this through ./runners <stack> so RUNNER_STACK is set"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestConfigErrorDedupesAndDropsLogPrefixes(t *testing.T) {
	stderr := `time="x" level=warning msg="The \"TAG\" variable is not set. Defaulting to a blank string."
time="x" level=warning msg="The \"TAG\" variable is not set. Defaulting to a blank string."
service "web" refers to undefined volume data: invalid compose project
service "web" refers to undefined volume data: invalid compose project
`
	got := configError(stderr, &exec.ExitError{}).Error()
	if want := `invalid Compose configuration: service "web" refers to undefined volume data: invalid compose project; not set: TAG`; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
	_, _, unset := composeMessages(stderr)
	if !slices.Equal(unset, []string{"TAG"}) {
		t.Fatal("unset variables not collected", unset)
	}
}

func TestHasVariableMatchesWholeName(t *testing.T) {
	env := []string{"RUNNER_STACK_X=1", "TAG="}
	if hasVariable(env, "RUNNER_STACK") || !hasVariable(env, "TAG") {
		t.Fatal("hasVariable matched a prefix or missed an empty value")
	}
}

func TestSupportsJSONProgress(t *testing.T) {
	for version, want := range map[string]bool{"2.31.0": false, "2.32.4": true, "2.40.3": true, "5.7.1": true, "2": false, "x.y": false} {
		if got := supportsJSONProgress(version); got != want {
			t.Errorf("supportsJSONProgress(%q) = %v, want %v", version, got, want)
		}
	}
}
