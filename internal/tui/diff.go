package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spar/dpad/internal/render"
)

// maxDiffCells bounds the LCS table for the changed middle of a file. Compose
// files are short and edits are local, so it is only reached by rewrites,
// which are then shown as everything removed and added.
const maxDiffCells = 1 << 22

type diffOp struct {
	kind byte // ' ', '-' or '+'
	text string
}

func diffLines(a, b []string) []diffOp {
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	var ops []diffOp
	for _, line := range a[:prefix] {
		ops = append(ops, diffOp{' ', line})
	}
	ops = append(ops, diffMiddle(a[prefix:len(a)-suffix], b[prefix:len(b)-suffix])...)
	for _, line := range a[len(a)-suffix:] {
		ops = append(ops, diffOp{' ', line})
	}
	return ops
}

func diffMiddle(a, b []string) []diffOp {
	var ops []diffOp
	if len(a)*len(b) > maxDiffCells {
		for _, line := range a {
			ops = append(ops, diffOp{'-', line})
		}
		for _, line := range b {
			ops = append(ops, diffOp{'+', line})
		}
		return ops
	}
	// lcs[i][j] is the longest common subsequence of a[i:] and b[j:].
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i, line := range slices.Backward(a) {
		for j := len(b) - 1; j >= 0; j-- {
			if line == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i, j = i+1, j+1
		case i < len(a) && (j == len(b) || lcs[i+1][j] >= lcs[i][j+1]):
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	return ops
}

// unifiedDiff renders the changes from old to new as unified-diff hunks with
// context lines around each change, colored with the theme.
func unifiedDiff(old, new string, context int) []string {
	ops := diffLines(splitLines(old), splitLines(new))
	var out []string
	for start := 0; start < len(ops); {
		first := start
		for first < len(ops) && ops[first].kind == ' ' {
			first++
		}
		if first == len(ops) {
			break
		}
		// A hunk ends once more than two contexts' worth of unchanged lines
		// separate it from the next change.
		last := first
		for k := first; k < len(ops); k++ {
			if ops[k].kind != ' ' {
				last = k
			} else if k-last > 2*context {
				break
			}
		}
		from, to := max(first-context, 0), min(last+context+1, len(ops))
		oldLine, newLine := 1, 1
		for _, op := range ops[:from] {
			if op.kind != '+' {
				oldLine++
			}
			if op.kind != '-' {
				newLine++
			}
		}
		oldCount, newCount := 0, 0
		var body []string
		for _, op := range ops[from:to] {
			line := string(op.kind) + op.text
			switch op.kind {
			case '-':
				oldCount++
				line = render.Error.Render(line)
			case '+':
				newCount++
				line = render.Success.Render(line)
			default:
				oldCount++
				newCount++
			}
			body = append(body, line)
		}
		out = append(out, render.Accent.Render(fmt.Sprintf("@@ -%d,%d +%d,%d @@", oldLine, oldCount, newLine, newCount)))
		out = append(out, body...)
		start = to
	}
	return out
}

func splitLines(s string) []string {
	s = strings.TrimSuffix(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
