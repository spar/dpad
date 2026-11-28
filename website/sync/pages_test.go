package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// Search results cut titles and descriptions beyond about these lengths.
const (
	maxTitle       = 70
	maxDescription = 160
)

func TestPageFields(t *testing.T) {
	pages, err := readPages("../pages")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pages {
		title := p.Title
		if p.Path != "/" {
			title += " · dpad"
		}
		if n := utf8.RuneCountInString(title); n > maxTitle {
			t.Errorf("%s: title is %d characters, over %d", p.Src, n, maxTitle)
		}
		if n := utf8.RuneCountInString(p.Description); n > maxDescription && p.Path != "/" {
			t.Errorf("%s: description is %d characters, over %d", p.Src, n, maxDescription)
		}
	}
}

var localRef = regexp.MustCompile(`(?:href|src)="(/[^"#]*)`)

// Every address a page links to on the site must be a page or a file.
func TestInternalLinks(t *testing.T) {
	out, err := renderPages("../pages")
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range out {
		for _, m := range localRef.FindAllSubmatch(b, -1) {
			ref := string(m[1])
			if ref == "/" {
				continue
			}
			if _, ok := out[strings.TrimPrefix(ref, "/")+"index.html"]; ok && strings.HasSuffix(ref, "/") {
				continue
			}
			if _, err := os.Stat(filepath.Join("../public", filepath.FromSlash(ref))); err == nil && !strings.HasSuffix(ref, "/") {
				continue
			}
			t.Errorf("%s links to %s, which doesn't exist", name, ref)
		}
	}
}

// Each guide and comparison page is listed on its section's overview.
func TestHubsListEveryPage(t *testing.T) {
	pages, err := readPages("../pages")
	if err != nil {
		t.Fatal(err)
	}
	hubs := map[string]string{}
	for _, p := range pages {
		if p.Path == "/docs/" || p.Path == "/compare/" {
			hubs[p.Path] = string(p.Body)
		}
	}
	for _, p := range pages {
		hub := hubPath(p)
		if hub == "" || hub == p.Path {
			continue
		}
		if !strings.Contains(hubs[hub], `href="`+p.Path+`"`) {
			t.Errorf("%s is not linked from %s", p.Path, hub)
		}
	}
}
