package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const site = "https://dpad.sh"

// page is one file in website/pages: a comment block of fields, then the
// page's HTML.
type page struct {
	Src         string
	Path        string // address on the site, such as /docs/remote-docker-ssh/
	Title       string // <title>, before " · dpad"
	Heading     string // the h1, defaulting to the title
	Nav         string // label in the docs sidebar, defaulting to the heading
	Description string
	OGDesc      string
	Updated     string // YYYY-MM-DD
	Order       int    // position in the sidebar
	Layout      string // "article" (the default) or "bare" for pages that draw their own <main>
	NoIndex     bool
	RelatedRefs []string
	Body        template.HTML
}

type pageData struct {
	Page     page
	Guides   []page
	Compares []page
	Related  []page
	Crumb    *page  // the section hub, for the breadcrumb
	CrumbFor string // the hub's name in the breadcrumb
	JSONLD   template.JS
}

// readPages loads every page under dir. Files starting with _ are templates.
func readPages(dir string) ([]page, error) {
	var pages []page
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".html") || strings.HasPrefix(d.Name(), "_") {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		p, err := parsePage(filepath.ToSlash(rel), b)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		pages = append(pages, p)
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(pages, func(a, b page) int { return cmp.Compare(a.Path, b.Path) })
	return pages, nil
}

func parsePage(rel string, b []byte) (page, error) {
	p := page{Src: rel, Path: urlPath(rel), Layout: "article"}
	head, body, ok := bytes.Cut(b, []byte("-->\n"))
	if !ok || !bytes.HasPrefix(head, []byte("<!--\n")) {
		return p, fmt.Errorf("must start with a <!-- ... --> block of fields")
	}
	for line := range strings.Lines(string(head[len("<!--\n"):])) {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			return p, fmt.Errorf("field line %q has no colon", strings.TrimSpace(line))
		}
		value = strings.TrimSpace(value)
		switch key {
		case "title":
			p.Title = value
		case "heading":
			p.Heading = value
		case "nav":
			p.Nav = value
		case "description":
			p.Description = value
		case "og":
			p.OGDesc = value
		case "updated":
			p.Updated = value
		case "order":
			n, err := strconv.Atoi(value)
			if err != nil {
				return p, fmt.Errorf("order: %w", err)
			}
			p.Order = n
		case "layout":
			p.Layout = value
		case "noindex":
			p.NoIndex = value == "true"
		case "related":
			for r := range strings.SplitSeq(value, ",") {
				p.RelatedRefs = append(p.RelatedRefs, strings.TrimSpace(r))
			}
		default:
			return p, fmt.Errorf("unknown field %q", key)
		}
	}
	if p.Title == "" || p.Description == "" {
		return p, fmt.Errorf("title and description are required")
	}
	if _, err := time.Parse(time.DateOnly, p.Updated); err != nil {
		return p, fmt.Errorf("updated: %w", err)
	}
	if p.Layout != "article" && p.Layout != "bare" {
		return p, fmt.Errorf("layout must be article or bare")
	}
	p.Heading = cmp.Or(p.Heading, p.Title)
	p.Nav = cmp.Or(p.Nav, p.Heading)
	p.OGDesc = cmp.Or(p.OGDesc, p.Description)
	p.Body = template.HTML(body)
	return p, nil
}

// urlPath maps a source file to its address: docs/index.html is /docs/ and
// docs/keys.html is /docs/keys/.
func urlPath(rel string) string {
	switch {
	case rel == "404.html":
		return "/404.html"
	case rel == "index.html":
		return "/"
	case strings.HasSuffix(rel, "/index.html"):
		return "/" + strings.TrimSuffix(rel, "index.html")
	default:
		return "/" + strings.TrimSuffix(rel, ".html") + "/"
	}
}

// outFile is where a page is written, relative to website/public.
func outFile(p page) string {
	if p.Path == "/404.html" {
		return "404.html"
	}
	return strings.TrimPrefix(p.Path, "/") + "index.html"
}

func section(p page) string {
	s, _, _ := strings.Cut(strings.TrimPrefix(p.Path, "/"), "/")
	return s
}

// renderPages returns each page's HTML and the sitemap, keyed by path
// relative to website/public.
func renderPages(dir string) (map[string][]byte, error) {
	pages, err := readPages(dir)
	if err != nil {
		return nil, err
	}
	layout, err := template.New("_layout.html").Funcs(template.FuncMap{
		"date": func(s string) string {
			t, _ := time.Parse(time.DateOnly, s)
			return t.Format("2 January 2006")
		},
		"title": func(p page) string {
			if p.Path == "/" {
				return p.Title
			}
			return p.Title + " · dpad"
		},
		"full": func(path string) string { return site + path },
	}).ParseFiles(filepath.Join(dir, "_layout.html"))
	if err != nil {
		return nil, err
	}

	byPath := map[string]page{}
	var guides, compares []page
	for _, p := range pages {
		byPath[p.Path] = p
		switch section(p) {
		case "docs":
			guides = append(guides, p)
		case "compare", "alternatives":
			compares = append(compares, p)
		}
	}
	order := func(a, b page) int { return cmp.Or(cmp.Compare(a.Order, b.Order), cmp.Compare(a.Path, b.Path)) }
	slices.SortFunc(guides, order)
	slices.SortFunc(compares, order)

	out := map[string][]byte{}
	for _, p := range pages {
		d := pageData{Page: p, Guides: guides, Compares: compares}
		for _, ref := range p.RelatedRefs {
			r, ok := byPath[ref]
			if !ok {
				return nil, fmt.Errorf("%s: related page %s does not exist", p.Src, ref)
			}
			d.Related = append(d.Related, r)
		}
		if hub, ok := byPath[hubPath(p)]; ok && hub.Path != p.Path {
			d.Crumb = &hub
			d.CrumbFor = map[string]string{"/docs/": "Docs", "/compare/": "Compare"}[hub.Path]
		}
		if d.JSONLD, err = jsonLD(p, d.Crumb, d.CrumbFor); err != nil {
			return nil, err
		}
		var b bytes.Buffer
		if err := layout.Execute(&b, d); err != nil {
			return nil, fmt.Errorf("%s: %w", p.Src, err)
		}
		out[outFile(p)] = b.Bytes()
	}
	out["sitemap.xml"] = sitemap(pages)
	return out, nil
}

func hubPath(p page) string {
	switch section(p) {
	case "docs":
		return "/docs/"
	case "compare", "alternatives":
		return "/compare/"
	}
	return ""
}

func jsonLD(p page, hub *page, hubName string) (template.JS, error) {
	var v any
	switch {
	case p.NoIndex:
		return "", nil
	case p.Path == "/":
		v = map[string]any{
			"@context":            "https://schema.org",
			"@type":               "SoftwareApplication",
			"name":                "dpad",
			"alternateName":       "dp",
			"description":         "An open source terminal UI and CLI for Docker: live container stats, multi-container logs and Compose project controls.",
			"url":                 site + "/",
			"applicationCategory": "DeveloperApplication",
			"operatingSystem":     "macOS, Linux, Windows",
			"downloadUrl":         "https://github.com/spar/dpad/releases/latest",
			"installUrl":          site + "/#install",
			"license":             "https://www.gnu.org/licenses/gpl-3.0.html",
			"image":               site + "/assets/og-image.png",
			"sameAs":              []string{"https://github.com/spar/dpad"},
			"offers":              map[string]string{"@type": "Offer", "price": "0", "priceCurrency": "USD"},
		}
	default:
		crumbs := []map[string]any{{"@type": "ListItem", "position": 1, "name": "dpad", "item": site + "/"}}
		if hub != nil {
			crumbs = append(crumbs, map[string]any{"@type": "ListItem", "position": 2, "name": hubName, "item": site + hub.Path})
		}
		crumbs = append(crumbs, map[string]any{"@type": "ListItem", "position": len(crumbs) + 1, "name": p.Heading, "item": site + p.Path})
		v = []any{
			map[string]any{
				"@context":         "https://schema.org",
				"@type":            "TechArticle",
				"headline":         p.Heading,
				"description":      p.Description,
				"url":              site + p.Path,
				"dateModified":     p.Updated,
				"image":            site + "/assets/og-image.png",
				"author":           map[string]string{"@type": "Organization", "name": "dpad", "url": site + "/"},
				"isPartOf":         map[string]string{"@type": "WebSite", "name": "dpad", "url": site + "/"},
				"inLanguage":       "en",
				"about":            map[string]string{"@type": "SoftwareApplication", "name": "dpad", "url": site + "/"},
				"mainEntityOfPage": site + p.Path,
			},
			map[string]any{"@context": "https://schema.org", "@type": "BreadcrumbList", "itemListElement": crumbs},
		}
	}
	b, err := json.MarshalIndent(v, "", "  ")
	return template.JS(b), err
}

func sitemap(pages []page) []byte {
	var b bytes.Buffer
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")
	for _, p := range pages {
		if p.NoIndex {
			continue
		}
		fmt.Fprintf(&b, "  <url>\n    <loc>%s%s</loc>\n    <lastmod>%s</lastmod>\n  </url>\n", site, p.Path, p.Updated)
	}
	b.WriteString("</urlset>\n")
	return b.Bytes()
}
