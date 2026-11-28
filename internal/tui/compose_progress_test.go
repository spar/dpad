package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/spar/dpad/internal/compose"
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
	for _, id := range []string{"37fb86c2a767", "1c9ad473d925", "6141c6522f61", "be21ec85edee"} {
		if strings.Contains(plain, id) {
			t.Fatalf("finished layer %s listed in compact output:\n%s", id, plain)
		}
	}
	if strings.Count(plain, "example-app") != 1 || !strings.Contains(plain, "Layers · 10 of 10 done") {
		t.Fatalf("container or layer count missing:\n%s", plain)
	}
	for _, stale := range []string{"Downloading", "Extracting", "Verifying Checksum", "Recreated", "Starting", "0B"} {
		if strings.Contains(plain, stale) {
			t.Fatalf("old state %q survived:\n%s", stale, plain)
		}
	}
	if !strings.Contains(plain, "✓ example-app") || !strings.Contains(plain, "Started") || !strings.Contains(rendered, "\x1b[32m") {
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

func TestComposeProgressAbortedRunHasNoActiveRows(t *testing.T) {
	var progress composeProgress
	progress.apply("Image nginx:1 Error 429 Too Many Requests\nImage alpine:3 Pulling\nImage redis:7 Interrupted\nImage python:3 Pulled\nError response from daemon: 429\nError response from daemon: 429\npulling failed: exit status 1\n", true)
	plain := ansi.Strip(progress.render(100, "failed"))
	if strings.Contains(plain, "in progress") || strings.Contains(plain, "Pulling") || !strings.Contains(plain, "2 interrupted") {
		t.Fatal("aborted pull still shown as running", plain)
	}
	if strings.Count(plain, "Error response from daemon: 429") != 1 {
		t.Fatal("repeated message not collapsed", plain)
	}
	if running := ansi.Strip(progress.render(100, "running")); !strings.Contains(running, "Pulling") {
		t.Fatal("live run lost its in-progress state", running)
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
	if v.showOutput || !strings.Contains(ansi.Strip(v.View()), "✓ updated, 1 recreated") {
		t.Fatal("bulk update did not finish on its summary table", v.View())
	}
	v.HandleKey(key("enter"))
	plain := ansi.Strip(v.View())
	if strings.Contains(plain, "6141c6522f61") || strings.Contains(plain, "Downloading") || !strings.Contains(v.Footer(), "raw log") {
		t.Fatal("default view still dumps the noisy transcript", plain)
	}
	v.HandleKey(key("l"))
	if !v.rawOutput || strings.Count(ansi.Strip(v.View()), "6141c6522f61") < 2 || !strings.Contains(v.Footer(), "compact progress") {
		t.Fatal("raw log cannot be inspected")
	}
	v.HandleKey(key("l"))
	if v.rawOutput || strings.Contains(ansi.Strip(v.View()), "6141c6522f61") {
		t.Fatal("cannot return to compact progress")
	}
}

func TestComposeProgressSummarizesChangesAndFailures(t *testing.T) {
	var unchanged composeProgress
	unchanged.apply("Image web:latest Pulling\nImage web:latest Pulled\n[2/2] Start updated services\nContainer demo-web-1 Running\n", true)
	if recreated, downloaded := unchanged.changes(); recreated != 0 || downloaded != 0 {
		t.Fatal("an up-to-date project reported changes", recreated, downloaded)
	}
	var updated composeProgress
	updated.apply("Image web:latest Pulling\n6141c6522f61 Downloading 1kB\n6141c6522f61 Download complete\nImage web:latest Pulled\n[2/2] Start updated services\nContainer demo-web-1 Recreate\nContainer demo-web-1 Recreated\nContainer demo-web-1 Started\n", true)
	if recreated, downloaded := updated.changes(); recreated != 1 || downloaded != 1 {
		t.Fatal("recreated containers or new layers were not counted", recreated, downloaded)
	}
	if plain := ansi.Strip(updated.render(100, "succeeded")); !strings.Contains(plain, "Layers · 1 of 1 done") || strings.Contains(plain, "Download complete") {
		t.Fatal("a layer stayed in progress after its image was pulled:\n" + plain)
	}
	var failed composeProgress
	failed.apply("Image web:9 Pulling\nImage web:9 Error manifest unknown\nError response from daemon: manifest for web:9 not found\n\npulling failed: exit status 1\n", true)
	if got := failed.failure(); got != "Error response from daemon: manifest for web:9 not found" {
		t.Fatalf("failure() = %q, want Compose's own error", got)
	}
}

func TestComposeJSONProgressGroupsLayersUnderImages(t *testing.T) {
	var out strings.Builder
	for i := range 12 {
		fmt.Fprintf(&out, `{"id":"Image app%02d:1","status":"Working","text":"Pulling"}`+"\n", i)
		for l := range 8 {
			fmt.Fprintf(&out, `{"id":"%02d%02d00000000","parent_id":"Image app%02d:1","status":"Done","text":"Pull complete"}`+"\n", i, l, i)
		}
		if i < 10 {
			fmt.Fprintf(&out, `{"id":"Image app%02d:1","status":"Done","text":"Pulled"}`+"\n", i)
		}
	}
	out.WriteString(`{"id":"aaaaaaaaaaaa","parent_id":"Image app11:1","status":"Working","text":"Downloading","details":"5MB","current":5000000,"total":20000000}` + "\n")
	out.WriteString(`{"id":"Image bad:1","status":"Error","text":"Error","details":"manifest unknown"}` + "\n")
	out.WriteString(`{"error":true,"message":"Error response from daemon: manifest unknown\nError response from daemon: manifest unknown"}` + "\n")
	var progress composeProgress
	progress.apply(out.String(), false)
	plain := ansi.Strip(progress.render(140, "pulling"))
	lines := strings.Split(plain, "\n")
	if len(lines) > 14 {
		t.Fatalf("12 images and 97 layers took %d lines:\n%s", len(lines), plain)
	}
	for _, want := range []string{
		"Images · 10 of 13 pulled · 1 failed", "✓ 10 images pulled: app00:1", "app10:1", "8 of 8 layers",
		"app11:1", "8 of 9 layers", "aaaaaaaaaaaa  Downloading [====>", "5MB / 20MB", "✗ bad:1", "manifest unknown",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "0000000000") && strings.Count(plain, "00000000") > 0 {
		t.Fatalf("finished layers listed:\n%s", plain)
	}
	if strings.Count(plain, "Error response from daemon: manifest unknown") != 1 || progress.failure() != "Error response from daemon: manifest unknown" {
		t.Fatalf("JSON error message not shown once:\n%s", plain)
	}
}

func TestPlainComposeLogRewritesJSONLines(t *testing.T) {
	got := plainComposeLog(`$ docker compose pull` + "\n" + `{"id":"Image web:1","status":"Working","text":"Pulling"}` + "\n" + `{"id":"abc123def456","parent_id":"Image web:1","status":"Working","text":"Downloading","details":"5MB"}` + "\n" + `{"error":true,"message":"Error response from daemon: denied"}`)
	want := "$ docker compose pull\nImage web:1 Pulling\nabc123def456 Downloading 5MB\nError response from daemon: denied"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
