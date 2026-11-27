package main

import (
	"bytes"
	"os"
	"testing"
)

// The site serves committed copies, so a new screenshot must be followed by
// `make website`.
func TestAssetsUpToDate(t *testing.T) {
	files, err := outputs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range files {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s is missing; run make website", path)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is out of date; run make website", path)
		}
	}
}

func TestLightenRecolorsPalette(t *testing.T) {
	svg := []byte(`<style>text{fill:#d8dee9}.bg{fill:#141721}` +
		`.p0{fill:#1d2130}.sp0{stroke:#1d2130}.p1{fill:#f2777a}.sp1{stroke:#f2777a}.p2{fill:#8fd18b}.sp2{stroke:#8fd18b}.p3{fill:#f6c177}.sp3{stroke:#f6c177}` +
		`.p4{fill:#7aa2f7}.sp4{stroke:#7aa2f7}.p5{fill:#c49cf2}.sp5{stroke:#c49cf2}.p6{fill:#6fd3e3}.sp6{stroke:#6fd3e3}.p7{fill:#c7ccd8}.sp7{stroke:#c7ccd8}` +
		`.p8{fill:#5c6377}.sp8{stroke:#5c6377}.p9{fill:#ff8f92}.sp9{stroke:#ff8f92}.p10{fill:#a6e3a1}.sp10{stroke:#a6e3a1}.p11{fill:#f9d58e}.sp11{stroke:#f9d58e}` +
		`.p12{fill:#94b6ff}.sp12{stroke:#94b6ff}.p13{fill:#d6b4fa}.sp13{stroke:#d6b4fa}.p14{fill:#8be4f0}.sp14{stroke:#8be4f0}.p15{fill:#eef1f6}.sp15{stroke:#eef1f6}</style>`)
	got, err := lighten(svg)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"text{fill:#1f2328}", ".bg{fill:#fbfaf6}", ".p2{fill:#1a7f37}.sp2{stroke:#1a7f37}", ".p15{fill:#1f2328}"} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("lighten output lacks %q", want)
		}
	}
}

func TestLightenRejectsUnknownFormat(t *testing.T) {
	if _, err := lighten([]byte("<svg/>")); err == nil {
		t.Fatal("want an error for an SVG without the palette classes")
	}
}
