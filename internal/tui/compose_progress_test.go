package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"dockernext/internal/compose"
)

func screenshotComposeOutput(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("testdata/compose-pull-up-plain.txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestComposeProgressCoalescesScreenshotTranscript(t *testing.T) {
	output := screenshotComposeOutput(t)
	var progress composeProgress
	// Replay cumulative, partially written output like the command snapshots.
	for end := 7; end < len(output); end += 7 {
		progress.apply(output[:end], false)
	}
	progress.apply(output, true)
	if len(progress.resources) != 12 {
		t.Fatalf("got %d resource rows, want one image, ten layers, one container", len(progress.resources))
	}
	rendered := progress.render(120, "succeeded")
	plain := ansi.Strip(rendered)
	for _, id := range []string{"37fb86c2a767", "1c9ad473d925", "6141c6522f61", "be21ec85edee", "example-app"} {
		if strings.Count(plain, id) != 1 {
			t.Fatalf("resource %s repeated in compact output:\n%s", id, plain)
		}
	}
	for _, stale := range []string{"Downloading", "Extracting", "Verifying Checksum", "Recreated", "Starting", "0B"} {
		if strings.Contains(plain, stale) {
			t.Fatalf("old state %q survived:\n%s", stale, plain)
		}
	}
	if !strings.Contains(plain, "Layers · 10 complete") || !strings.Contains(plain, "✓ example-app") || !strings.Contains(plain, "Started") || !strings.Contains(rendered, "\x1b[32m") {
		t.Fatalf("completion summaries or actual completion styling missing:\n%s", rendered)
	}
	if len(strings.Split(plain, "\n")) >= len(strings.Split(output, "\n"))/2 {
		t.Fatal("screenshot transcript was not meaningfully compacted")
	}
}

func TestComposeProgressUpdatesLayerInPlaceAndRetainsEarlierResources(t *testing.T) {
	var progress composeProgress
	progress.apply("Image app:main Pulling\n6141c6522f61 Downloading 1.378kB\n", false)
	progress.apply("6141c6522f61 Downloading 67.61kB\n", false)
	if len(progress.resources) != 2 || progress.resources["Layer:6141c6522f61"].detail != "67.61kB" {
		t.Fatal("new snapshot appended a row or forgot an earlier resource")
	}
	progress.apply("6141c6522f61 Extracting 67.61kB\n", false)
	if progress.resources["Layer:6141c6522f61"].status != "Extracting" {
		t.Fatal("latest phase did not replace download progress")
	}
	progress.apply("Container app Start", false)
	if len(progress.resources) != 2 {
		t.Fatal("partial resource line was committed")
	}
	progress.apply("Container app Started\n", false)
	if !progress.resources["Container:app"].done() {
		t.Fatal("complete resource line was not committed")
	}
}

func TestComposeProgressErrorsAndUnrecognizedOutputStayVisible(t *testing.T) {
	var progress composeProgress
	progress.apply("Image app:main Error registry unauthorized\nError response from daemon: authentication required\nstarting failed: exit status 1", true)
	plain := ansi.Strip(progress.render(100, "failed"))
	for _, text := range []string{"1 failed", "registry unauthorized", "authentication required", "starting failed"} {
		if !strings.Contains(plain, text) {
			t.Fatal("failure detail was hidden", plain)
		}
	}
	if strings.Contains(plain, "in progress") {
		t.Fatal("failed resource counted as still running")
	}
	var legacy composeProgress
	legacy.apply("web Pulling\nweb Pulled\nContainer web-1 Started\n", true)
	if len(legacy.resources) != 2 || !legacy.resources["Image:web"].done() {
		t.Fatal("older Compose output was not coalesced")
	}
}

func TestComposeProgressStateIsBounded(t *testing.T) {
	var output strings.Builder
	for i := range maxComposeResources * 2 {
		fmt.Fprintf(&output, "%012x Downloading 1MB\n", i)
	}
	for i := range 100 {
		fmt.Fprintf(&output, "unrecognized diagnostic %d\n", i)
	}
	var progress composeProgress
	progress.apply(output.String(), true)
	if len(progress.resources) != maxComposeResources || len(progress.order) != maxComposeResources || len(progress.messages) != 8 || !progress.omitted {
		t.Fatal("progress state grew without bounds")
	}
}

func TestComposeDefaultViewIsCompactAndRawLogIsAvailable(t *testing.T) {
	v := progressFixture(t)
	v.SetSize(120, 42)
	output := screenshotComposeOutput(t)
	v.Update(composeEventsMsg{view: v, events: []compose.Event{{Project: "alpha", State: "succeeded", Output: output}}, done: true})
	plain := ansi.Strip(v.View())
	if strings.Count(plain, "6141c6522f61") != 1 || strings.Contains(plain, "Downloading") || !strings.Contains(v.Footer(), "raw log") {
		t.Fatal("default view still dumps the noisy transcript", plain)
	}
	v.HandleKey(key("l"))
	if !v.rawOutput || strings.Count(ansi.Strip(v.View()), "6141c6522f61") < 2 || !strings.Contains(v.Footer(), "compact progress") {
		t.Fatal("raw log cannot be inspected")
	}
	v.HandleKey(key("l"))
	if v.rawOutput || strings.Count(ansi.Strip(v.View()), "6141c6522f61") != 1 {
		t.Fatal("cannot return to compact progress")
	}
}
