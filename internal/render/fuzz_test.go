package render

import "testing"

func FuzzDetectLevel(f *testing.F) {
	for _, s := range []string{
		`{"level":"error","msg":"boom"}`,
		`ts=1 level=warn msg="slow"`,
		`2026/11/26 [notice] 1#1: start`,
		`ERROR something failed`,
		`no level here`,
		`level=`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, line string) {
		lvl, start, end := DetectLevel(line)
		if lvl == LevelNone {
			if start != -1 || end != -1 {
				t.Fatalf("DetectLevel(%q) = none with span %d:%d", line, start, end)
			}
			return
		}
		if start < 0 || start >= end || end > len(line) {
			t.Fatalf("DetectLevel(%q) span %d:%d is outside the line", line, start, end)
		}
		if parseLevel(line[start:end]) != lvl {
			t.Fatalf("DetectLevel(%q) = %v but its token %q is not that level", line, lvl, line[start:end])
		}
	})
}

func FuzzSplitRepoTag(f *testing.F) {
	for _, s := range []string{"nginx:latest", "registry:5000/app:v1", "registry:5000/app", "app", ":", "<none>:<none>"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, rt string) {
		repo, tag := SplitRepoTag(rt)
		if repo+":"+tag != rt && (tag != "<none>" || repo != rt) {
			t.Fatalf("SplitRepoTag(%q) = %q, %q", rt, repo, tag)
		}
	})
}
