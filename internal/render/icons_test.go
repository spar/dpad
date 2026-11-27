package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestLookupIcon(t *testing.T) {
	for image, want := range map[string]rune{
		"postgres":                                   nfPostgres,
		"postgres:16-alpine":                         nfPostgres,
		"docker.io/library/postgres:16":              nfPostgres,
		"postgres@sha256:abc":                        nfPostgres,
		"registry:5000/redis":                        nfRedis,
		"localhost/redis:7":                          nfRedis,
		"nginx:1.27-alpine":                          nfNginx,
		"lscr.io/linuxserver/plex:latest":            nfPlex,
		"ghcr.io/immich-app/immich-server:release":   nfPhotos,
		"pihole/pihole:latest":                       nfPiHole,
		"grafana/loki:3.0":                           nfChart,
		"portainer/portainer-ce:2.21":                nfMdDocker,
		"gitlab/gitlab-ee:17.0":                      nfGitLab,
		"kindest/node:v1.31":                         nfKubernetes,
		"node:22":                                    nfNode,
		"ghcr.io/home-assistant/home-assistant:2024": nfHome,
		"vaultwarden/server":                         nfShield,
		"Python:3.13":                                nfPython,
		"mcr.microsoft.com/dotnet/aspnet:8.0":        nfDotnet,
	} {
		if got, ok := lookupIcon(image); !ok || got != want {
			t.Errorf("lookupIcon(%q) = %U, %v; want %U", image, got, ok, want)
		}
	}
	for _, image := range []string{"mycompany/internal-tool", "sha256:0123456789ab", "", "scratch"} {
		if r, ok := lookupIcon(image); ok {
			t.Errorf("lookupIcon(%q) = %U; want no icon", image, r)
		}
	}
}

func TestImageIcon(t *testing.T) {
	t.Cleanup(func() { SetIcons(false) })
	if got := ImageIcon("postgres"); got != "" {
		t.Fatalf("icons off: ImageIcon = %q; want empty", got)
	}
	gen := ThemeGeneration()
	SetIcons(true)
	if ThemeGeneration() == gen {
		t.Error("SetIcons did not change the generation, so cached rows would keep the old icons")
	}
	for _, image := range []string{"postgres", "mycompany/internal-tool"} {
		got := ImageIcon(image)
		if w := ansi.StringWidth(got); w != 2 {
			t.Errorf("ImageIcon(%q) is %d cells wide; want 2", image, w)
		}
		if width(got) != ansi.StringWidth(got) {
			t.Errorf("width(%q) = %d; ansi.StringWidth = %d", got, width(got), ansi.StringWidth(got))
		}
		if !strings.HasSuffix(got, " ") {
			t.Errorf("ImageIcon(%q) = %q; want a trailing space, as wide glyphs spill into it", image, got)
		}
	}
}

func TestIconGlyphsAreOneCell(t *testing.T) {
	for name, r := range imageIcons {
		if w := ansi.StringWidth(string(r)); w != 1 {
			t.Errorf("%s: %U is %d cells", name, r, w)
		}
	}
}
