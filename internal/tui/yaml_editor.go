package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"go.yaml.in/yaml/v3"

	"github.com/spar/dpad/internal/render"
)

// builtinEditor is the editor: value that picks the editor built into dpad.
// It is also what runs when no editor is chosen and neither $VISUAL nor
// $EDITOR is set.
const builtinEditor = "builtin"

// yamlEditor is the built-in editor for Compose files: no modes, the usual
// keys, YAML colored from the theme and checked as you type.
type yamlEditor struct {
	lines    [][]rune
	row, col int // col counts runes
	goal     int // the column up and down aim for
	top      int
	left     int // first cell shown, for lines wider than the screen
	width    int
	height   int // rows for text, without the status line

	crlf, finalNewline bool
	saved              string
	dirty              bool
	undo, redo         []editorSnapshot
	typing             bool // the last change was typing, so undo groups it
	version            int  // counts changes to the text

	prompt string // "find" or "line" while one is open
	input  []rune
	query  string

	lintErr string
	lintRow int

	note       string
	closeArmed bool
	quitArmed  bool
}

type editorSnapshot struct {
	lines    [][]rune
	row, col int
}

type editorAction int

const (
	editorNone editorAction = iota
	editorSave
	editorClose
)

const maxUndo = 500

func newYAMLEditor(text string) *yamlEditor {
	e := &yamlEditor{
		crlf:         strings.Contains(text, "\r\n"),
		finalNewline: text == "" || strings.HasSuffix(text, "\n"),
		lintRow:      -1,
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	for l := range strings.SplitSeq(text, "\n") {
		e.lines = append(e.lines, []rune(l))
	}
	e.saved = e.Value()
	e.lint()
	return e
}

// Value is the text with the line endings and final newline it was opened with.
func (e *yamlEditor) Value() string {
	if len(e.lines) == 1 && len(e.lines[0]) == 0 {
		return ""
	}
	nl := "\n"
	if e.crlf {
		nl = "\r\n"
	}
	var b strings.Builder
	for i, l := range e.lines {
		if i > 0 {
			b.WriteString(nl)
		}
		b.WriteString(string(l))
	}
	if e.finalNewline {
		b.WriteString(nl)
	}
	return b.String()
}

func (e *yamlEditor) SetSize(width, height int) {
	e.width, e.height = max(width, 1), max(height-1, 1)
	e.scroll()
}

// HandleKey applies a key and says whether the person asked to save or close.
func (e *yamlEditor) HandleKey(msg tea.KeyPressMsg) editorAction {
	key := msg.String()
	if e.prompt != "" {
		e.promptKey(msg)
		return editorNone
	}
	armed := e.closeArmed
	e.note, e.closeArmed, e.quitArmed = "", false, false
	typed, vertical, version := false, false, e.version
	switch key {
	case "esc":
		if e.dirty && !armed {
			e.note, e.closeArmed = "Unsaved changes: esc again discards them, ctrl+s saves", true
			return editorNone
		}
		return editorClose
	case "ctrl+s":
		return editorSave
	case "up", "down", "pgup", "pgdown":
		step := map[string]int{"up": -1, "down": 1, "pgup": -e.height, "pgdown": e.height}[key]
		e.moveRow(e.row + step)
		vertical = true
	case "left":
		if e.col > 0 {
			e.col--
		} else if e.row > 0 {
			e.row--
			e.col = len(e.lines[e.row])
		}
	case "right":
		if e.col < len(e.lines[e.row]) {
			e.col++
		} else if e.row < len(e.lines)-1 {
			e.row, e.col = e.row+1, 0
		}
	case "ctrl+left", "alt+left", "alt+b":
		e.col = wordLeft(e.lines[e.row], e.col)
	case "ctrl+right", "alt+right", "alt+f":
		e.col = wordRight(e.lines[e.row], e.col)
	case "home", "ctrl+a":
		e.col = 0
	case "end", "ctrl+e":
		e.col = len(e.lines[e.row])
	case "ctrl+home":
		e.row, e.col = 0, 0
	case "ctrl+end":
		e.row = len(e.lines) - 1
		e.col = len(e.lines[e.row])
	case "ctrl+f":
		e.prompt, e.input = "find", []rune(e.query)
	case "ctrl+g":
		e.prompt, e.input = "line", nil
	case "ctrl+n", "f3":
		e.find(true)
	case "ctrl+p", "shift+f3":
		e.find(false)
	case "ctrl+z":
		e.restore(&e.undo, &e.redo)
	case "ctrl+y":
		e.restore(&e.redo, &e.undo)
	case "enter":
		e.change(false)
		l := e.lines[e.row]
		indent := lineIndent(l[:e.col])
		rest := append(indent, l[e.col:]...)
		e.lines[e.row] = l[:e.col:e.col]
		e.lines = append(e.lines[:e.row+1], append([][]rune{rest}, e.lines[e.row+1:]...)...)
		e.row, e.col = e.row+1, len(indent)
	case "backspace":
		if e.col == 0 && e.row == 0 {
			break
		}
		e.change(false)
		if e.col > 0 {
			l := e.lines[e.row]
			e.lines[e.row] = append(l[:e.col-1:e.col-1], l[e.col:]...)
			e.col--
		} else {
			prev := e.lines[e.row-1]
			e.col = len(prev)
			e.lines[e.row-1] = append(slices.Clip(prev), e.lines[e.row]...)
			e.lines = append(e.lines[:e.row], e.lines[e.row+1:]...)
			e.row--
		}
	case "delete":
		l := e.lines[e.row]
		if e.col == len(l) && e.row == len(e.lines)-1 {
			break
		}
		e.change(false)
		if e.col < len(l) {
			e.lines[e.row] = append(l[:e.col:e.col], l[e.col+1:]...)
		} else {
			e.lines[e.row] = append(slices.Clip(l), e.lines[e.row+1]...)
			e.lines = append(e.lines[:e.row+1], e.lines[e.row+2:]...)
		}
	case "ctrl+k":
		e.change(false)
		if len(e.lines) == 1 {
			e.lines[0] = nil
		} else {
			e.lines = append(e.lines[:e.row], e.lines[e.row+1:]...)
		}
		e.row = min(e.row, len(e.lines)-1)
		e.col = 0
	case "tab":
		// YAML does not allow tabs for indentation.
		e.insert("  ", true)
		typed = true
	case "shift+tab":
		l := e.lines[e.row]
		n := min(len(lineIndent(l)), 2)
		if n > 0 {
			e.change(false)
			e.lines[e.row] = append([]rune(nil), l[n:]...)
			e.col = max(e.col-n, 0)
		}
	default:
		if msg.Text == "" || msg.Mod&(tea.ModCtrl|tea.ModAlt) != 0 {
			return editorNone
		}
		e.insert(msg.Text, true)
		typed = true
	}
	if !typed {
		e.typing = false
	}
	if !vertical {
		e.goal = e.col
	}
	e.edited(e.version != version)
	return editorNone
}

// Paste inserts pasted text in one undo step.
func (e *yamlEditor) Paste(text string) {
	if e.prompt != "" {
		e.input = append(e.input, []rune(strings.ReplaceAll(text, "\n", " "))...)
		return
	}
	e.note, e.closeArmed = "", false
	version := e.version
	e.insert(text, false)
	e.typing = false
	e.goal = e.col
	e.edited(e.version != version)
}

func (e *yamlEditor) promptKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "esc":
		e.prompt = ""
	case "enter":
		value := strings.TrimSpace(string(e.input))
		switch e.prompt {
		case "find":
			e.query = value
			if value != "" {
				e.find(true)
			}
		case "line":
			e.goal = 0
			if value == "$" {
				e.moveRow(len(e.lines) - 1)
			} else if n, err := strconv.Atoi(value); err == nil {
				e.moveRow(n - 1)
			}
		}
		e.prompt = ""
	case "backspace":
		if len(e.input) > 0 {
			e.input = e.input[:len(e.input)-1]
		}
	default:
		if msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			e.input = append(e.input, []rune(msg.Text)...)
		}
	}
	e.scroll()
}

func (e *yamlEditor) moveRow(row int) {
	e.row = min(max(row, 0), len(e.lines)-1)
	e.col = min(e.goal, len(e.lines[e.row]))
}

// change records the text before an edit so ctrl+z can bring it back.
// Typing in a row is one step.
func (e *yamlEditor) change(typing bool) {
	e.version++
	if typing && e.typing {
		return
	}
	e.typing = typing
	e.undo = append(e.undo, e.snapshot())
	if len(e.undo) > maxUndo {
		e.undo = e.undo[1:]
	}
	e.redo = nil
}

func (e *yamlEditor) snapshot() editorSnapshot {
	lines := make([][]rune, len(e.lines))
	for i, l := range e.lines {
		lines[i] = append([]rune(nil), l...)
	}
	return editorSnapshot{lines: lines, row: e.row, col: e.col}
}

func (e *yamlEditor) restore(from, to *[]editorSnapshot) {
	n := len(*from)
	if n == 0 {
		return
	}
	e.version++
	*to = append(*to, e.snapshot())
	s := (*from)[n-1]
	*from = (*from)[:n-1]
	e.lines, e.row, e.col = s.lines, s.row, s.col
}

func (e *yamlEditor) insert(text string, typing bool) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	if text == "" {
		return
	}
	e.change(typing)
	parts := strings.Split(text, "\n")
	l := e.lines[e.row]
	tail := append([]rune(nil), l[e.col:]...)
	first := append(l[:e.col:e.col], []rune(parts[0])...)
	if len(parts) == 1 {
		e.lines[e.row] = append(first, tail...)
		e.col = len(first)
		return
	}
	added := [][]rune{first}
	for _, p := range parts[1 : len(parts)-1] {
		added = append(added, []rune(p))
	}
	last := []rune(parts[len(parts)-1])
	added = append(added, append(last, tail...))
	e.lines = append(e.lines[:e.row], append(added, e.lines[e.row+1:]...)...)
	e.row += len(parts) - 1
	e.col = len(last)
}

// edited updates what follows the cursor and, when it changed, the text.
func (e *yamlEditor) edited(changed bool) {
	if changed {
		e.dirty = e.Value() != e.saved
		e.lint()
	}
	e.scroll()
}

func (e *yamlEditor) lint() {
	e.lintErr, e.lintRow = "", -1
	var n yaml.Node
	err := yaml.Unmarshal([]byte(e.Value()), &n)
	if err == nil {
		return
	}
	e.lintErr = strings.TrimPrefix(err.Error(), "yaml: ")
	if rest, ok := strings.CutPrefix(e.lintErr, "line "); ok {
		if end := strings.IndexFunc(rest, func(r rune) bool { return !unicode.IsDigit(r) }); end > 0 {
			if n, err := strconv.Atoi(rest[:end]); err == nil {
				e.lintRow = n - 1
			}
		}
	}
}

// find moves to the next match of the query after the cursor, or the one
// before it, wrapping around the file. Case is ignored.
func (e *yamlEditor) find(forward bool) {
	q := []rune(strings.ToLower(e.query))
	if len(q) == 0 {
		e.prompt, e.input = "find", nil
		return
	}
	n, from := len(e.lines), e.row
	for i := 0; i <= n; i++ {
		r := from + i
		if !forward {
			r = from - i
		}
		r = ((r % n) + n) % n
		hits := matchColumns([]rune(strings.ToLower(string(e.lines[r]))), q)
		if forward {
			for _, c := range hits {
				if i > 0 || c > e.col {
					e.foundAt(r, c, r < from || i == n)
					return
				}
			}
			continue
		}
		for _, c := range slices.Backward(hits) {
			if i > 0 || c < e.col {
				e.foundAt(r, c, r > from || i == n)
				return
			}
		}
	}
	e.note = "no match for " + e.query
}

func (e *yamlEditor) foundAt(row, col int, wrapped bool) {
	e.row, e.col, e.goal = row, col, col
	if wrapped {
		e.note = "search wrapped around the file"
	}
	e.scroll()
}

func matchColumns(line, q []rune) []int {
	var out []int
	for c := 0; c+len(q) <= len(line); c++ {
		if string(line[c:c+len(q)]) == string(q) {
			out = append(out, c)
		}
	}
	return out
}

// scroll keeps the cursor on screen.
func (e *yamlEditor) scroll() {
	e.row = min(max(e.row, 0), len(e.lines)-1)
	e.col = min(max(e.col, 0), len(e.lines[e.row]))
	if e.row < e.top {
		e.top = e.row
	}
	if e.height > 0 && e.row >= e.top+e.height {
		e.top = e.row - e.height + 1
	}
	text := e.textWidth()
	cell := ansi.StringWidth(displayText(e.lines[e.row][:e.col]))
	if cell < e.left {
		e.left = max(cell-text/4, 0)
	}
	if cell >= e.left+text-1 {
		e.left = cell - text + text/4
	}
}

func (e *yamlEditor) gutterWidth() int { return len(strconv.Itoa(len(e.lines))) }

func (e *yamlEditor) textWidth() int { return max(e.width-e.gutterWidth()-2, 4) }

func (e *yamlEditor) View() string {
	gutter, text := e.gutterWidth(), e.textWidth()
	str := lipgloss.NewStyle().Foreground(render.Theme().UI.Success.C())
	out := make([]string, 0, e.height+1)
	block := -1
	for r := 0; r < e.top && r < len(e.lines); r++ {
		block = blockScalar(string(e.lines[r]), block)
	}
	for r := e.top; r < e.top+e.height; r++ {
		if r >= len(e.lines) {
			out = append(out, render.Dim.Render(strings.Repeat(" ", gutter)+" ~"))
			continue
		}
		raw := displayText(e.lines[r])
		inBlock := block >= 0 && strings.TrimSpace(raw) != "" && indentWidth(raw) > block
		block = blockScalar(raw, block)
		var line string
		if inBlock {
			line = paintMatches(raw, e.query, str)
		} else {
			line = highlightYAML(raw, e.query)
		}
		if r == e.row && e.prompt == "" {
			line = withCursor(line, ansi.StringWidth(displayText(e.lines[r][:e.col])))
		}
		line = ansi.Cut(line, e.left, e.left+text)
		if ansi.StringWidth(raw) > e.left+text {
			line = ansi.Cut(line, 0, text-1) + render.Dim.Render("…")
		}
		num := fmt.Sprintf("%*d", gutter, r+1)
		switch r {
		case e.lintRow:
			num = render.Error.Render(num + "▌")
		case e.row:
			num = render.Accent.Render(num) + " "
		default:
			num = render.Dim.Render(num) + " "
		}
		out = append(out, num+" "+line)
	}
	return strings.Join(append(out, e.statusLine()), "\n")
}

func (e *yamlEditor) statusLine() string {
	switch e.prompt {
	case "find":
		return ansi.Truncate(render.Accent.Render("find: ")+string(e.input)+render.Selected.Render(" ")+render.Dim.Render("  enter search · esc cancel"), e.width, "…")
	case "line":
		return ansi.Truncate(render.Accent.Render(fmt.Sprintf("go to line (1-%d, $ for last): ", len(e.lines)))+string(e.input)+render.Selected.Render(" "), e.width, "…")
	}
	left := render.Success.Render("YAML ok")
	switch {
	case e.note != "":
		left = render.Warn.Render(e.note)
	case e.lintErr != "":
		left = render.Error.Render("YAML: " + e.lintErr)
	}
	right := render.Dim.Render(fmt.Sprintf("Ln %d, Col %d", e.row+1, e.col+1))
	if e.dirty {
		right = render.Warn.Render("● modified") + "  " + right
	}
	room := e.width - ansi.StringWidth(right) - 1
	if ansi.StringWidth(left) > room {
		left = ansi.Truncate(left, max(room, 0), "…")
	}
	return left + strings.Repeat(" ", max(e.width-ansi.StringWidth(left)-ansi.StringWidth(right), 1)) + right
}

// displayText shows tabs and other control characters as one visible cell,
// so columns on screen match the runes they stand for.
func displayText(rs []rune) string {
	var b strings.Builder
	for _, r := range rs {
		switch {
		case r == '\t':
			b.WriteRune('→')
		case unicode.IsControl(r):
			b.WriteRune('·')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// withCursor shows the cursor at cell col of a styled line.
func withCursor(line string, col int) string {
	w := ansi.StringWidth(line)
	if col >= w {
		return line + render.Selected.Render(" ")
	}
	end := col + 1
	for end < w && ansi.StringWidth(ansi.Cut(line, col, end)) == 0 {
		end++
	}
	return ansi.Cut(line, 0, col) + render.Selected.Render(ansi.Strip(ansi.Cut(line, col, end))) + ansi.Cut(line, end, w)
}

func lineIndent(l []rune) []rune {
	i := 0
	for i < len(l) && l[i] == ' ' {
		i++
	}
	indent := append([]rune(nil), l[:i]...)
	if t := strings.TrimRight(string(l), " "); strings.HasSuffix(t, ":") || strings.HasSuffix(t, "|") || strings.HasSuffix(t, ">") || strings.HasSuffix(t, "|-") || strings.HasSuffix(t, ">-") {
		indent = append(indent, ' ', ' ')
	}
	return indent
}

func indentWidth(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

func wordLeft(l []rune, col int) int {
	for col > 0 && !isWordRune(l[col-1]) {
		col--
	}
	for col > 0 && isWordRune(l[col-1]) {
		col--
	}
	return col
}

func wordRight(l []rune, col int) int {
	for col < len(l) && !isWordRune(l[col]) {
		col++
	}
	for col < len(l) && isWordRune(l[col]) {
		col++
	}
	return col
}

// blockScalar follows | and > block scalars from line to line: open is the
// indent of the line that started one, or -1.
func blockScalar(line string, open int) int {
	if open >= 0 && (strings.TrimSpace(line) == "" || indentWidth(line) > open) {
		return open
	}
	t := strings.TrimSpace(line)
	if i := commentStart(t); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	for _, m := range []string{"|", ">", "|-", ">-", "|+", ">+"} {
		if strings.HasSuffix(t, ": "+m) || t == "- "+m {
			return indentWidth(line)
		}
	}
	return -1
}

// highlightYAML colors one line with roles the themes already have: keys
// use the header color, strings success, numbers and booleans warning,
// anchors, aliases, tags and ${VARS} accent, comments dim.
func highlightYAML(line, query string) string {
	th := render.Theme().UI
	keyStyle := lipgloss.NewStyle().Foreground(th.Header.C())
	accent := lipgloss.NewStyle().Foreground(th.Accent.C())
	var b strings.Builder
	rest := strings.TrimLeft(line, " ")
	b.WriteString(line[:len(line)-len(rest)])
	for strings.HasPrefix(rest, "- ") || rest == "-" {
		b.WriteString(accent.Render("-"))
		rest = rest[1:]
		trimmed := strings.TrimLeft(rest, " ")
		b.WriteString(rest[:len(rest)-len(trimmed)])
		rest = trimmed
	}
	if strings.HasPrefix(rest, "#") || strings.HasPrefix(rest, "---") || strings.HasPrefix(rest, "...") {
		return b.String() + paintMatches(rest, query, render.Dim)
	}
	if k := keyEnd(rest); k > 0 {
		b.WriteString(paintMatches(rest[:k], query, keyStyle))
		b.WriteString(render.Dim.Render(":"))
		rest = rest[k+1:]
	}
	b.WriteString(highlightValue(rest, query))
	return b.String()
}

// keyEnd finds the colon that ends a mapping key, or -1.
func keyEnd(s string) int {
	var quote rune
	for i, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			if i != 0 {
				return -1
			}
			quote = r
		case r == '#' && i > 0 && s[i-1] == ' ':
			return -1
		case r == '{' || r == '[':
			if i == 0 {
				return -1
			}
		case r == ':' && (i+1 == len(s) || s[i+1] == ' '):
			return i
		}
	}
	return -1
}

func highlightValue(v, query string) string {
	comment := ""
	if i := commentStart(v); i >= 0 {
		v, comment = v[:i], v[i:]
	}
	body := strings.TrimLeft(v, " ")
	lead := v[:len(v)-len(body)]
	trimmed := strings.TrimRight(body, " ")
	trail := body[len(trimmed):]
	body = trimmed

	th := render.Theme().UI
	str := lipgloss.NewStyle().Foreground(th.Success.C())
	num := lipgloss.NewStyle().Foreground(th.Warning.C())
	accent := lipgloss.NewStyle().Foreground(th.Accent.C())
	var styled string
	switch {
	case body == "":
	case strings.ContainsAny(body[:1], "|>") && len(body) <= 2:
		styled = render.Dim.Render(body)
	case strings.ContainsAny(body[:1], "&*!"):
		first, rest, _ := strings.Cut(body, " ")
		styled = paintMatches(first, query, accent)
		if rest != "" {
			styled += " " + highlightValue(rest, query)
		}
	case isYAMLKeyword(body):
		styled = paintMatches(body, query, num)
	case strings.ContainsAny(body[:1], "[{"):
		styled = paintScalar(body, lipgloss.NewStyle(), str, accent, query)
	default:
		styled = paintScalar(body, str, str, accent, query)
	}
	out := lead + styled + trail
	if comment != "" {
		out += paintMatches(comment, query, render.Dim)
	}
	return out
}

func isYAMLKeyword(s string) bool {
	switch strings.ToLower(s) {
	case "true", "false", "yes", "no", "on", "off", "null", "~":
		return true
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// commentStart finds a # that starts a comment outside quotes, or -1.
func commentStart(v string) int {
	var quote rune
	for i, r := range v {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			if i == 0 || strings.ContainsRune(" [{,:", rune(v[i-1])) {
				quote = r
			}
		case r == '#' && (i == 0 || v[i-1] == ' '):
			return i
		}
	}
	return -1
}

// paintScalar styles s with base, quoted parts with quoted and ${VAR}
// references with vars.
func paintScalar(s string, base, quoted, vars lipgloss.Style, query string) string {
	var b strings.Builder
	style := base
	var quote byte
	start := 0
	flush := func(end int, st lipgloss.Style) {
		if end > start {
			b.WriteString(paintMatches(s[start:end], query, st))
		}
		start = end
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '$' && i+1 < len(s) && s[i+1] == '{':
			if end := strings.IndexByte(s[i:], '}'); end > 0 {
				flush(i, style)
				flush(i+end+1, vars)
				i += end
			}
		case quote == 0 && (c == '"' || c == '\'') && (i == 0 || strings.IndexByte(" [{,:", s[i-1]) >= 0):
			flush(i, style)
			quote, style = c, quoted
		case quote != 0 && c == quote:
			flush(i+1, style)
			quote, style = 0, base
		}
	}
	flush(len(s), style)
	return b.String()
}

// paintMatches styles s, marking matches of the search query.
func paintMatches(s, query string, st lipgloss.Style) string {
	if query == "" {
		return st.Render(s)
	}
	var b strings.Builder
	q := []rune(strings.ToLower(query))
	rs := []rune(s)
	low := []rune(strings.ToLower(s))
	if len(low) != len(rs) {
		return st.Render(s)
	}
	start := 0
	for _, c := range matchColumns(low, q) {
		if c < start {
			continue
		}
		b.WriteString(st.Render(string(rs[start:c])))
		b.WriteString(render.Match.Render(string(rs[c : c+len(q)])))
		start = c + len(q)
	}
	b.WriteString(st.Render(string(rs[start:])))
	return b.String()
}
