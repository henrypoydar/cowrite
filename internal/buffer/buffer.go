// Package buffer implements the text buffer underlying a cowrite session.
// All mutations flow through Replace so that undo history and change
// tracking see every edit, whether it came from a keystroke or a merged
// external change.
package buffer

import "strings"

// Pos addresses a rune within the buffer. Col is a rune offset, not bytes.
// Col == len(line) is valid and means "just past the last rune".
type Pos struct {
	Line, Col int
}

// Before reports whether p precedes o in document order.
func (p Pos) Before(o Pos) bool {
	return p.Line < o.Line || (p.Line == o.Line && p.Col < o.Col)
}

type change struct {
	start   Pos
	oldText string
	newText string
}

type group struct {
	changes []change
	cursor  Pos // cursor position when the group began, restored on undo
}

// Buffer holds the document as rune lines. A buffer always has at least
// one line; an empty document is one empty line.
type Buffer struct {
	lines [][]rune
	dirty bool
	undo  []group
	redo  []group
	open  *group
}

// New builds a buffer from file content. A single trailing newline is
// stripped; Save restores it.
func New(content string) *Buffer {
	content = strings.TrimSuffix(content, "\n")
	parts := strings.Split(content, "\n")
	lines := make([][]rune, len(parts))
	for i, p := range parts {
		lines[i] = []rune(p)
	}
	return &Buffer{lines: lines}
}

func (b *Buffer) LineCount() int    { return len(b.lines) }
func (b *Buffer) Line(i int) []rune { return b.lines[i] }
func (b *Buffer) LineLen(i int) int { return len(b.lines[i]) }
func (b *Buffer) Dirty() bool       { return b.dirty }
func (b *Buffer) MarkClean()        { b.dirty = false }
func (b *Buffer) Grouping() bool    { return b.open != nil }

// Lines returns the document as strings, one per line.
func (b *Buffer) Lines() []string {
	out := make([]string, len(b.lines))
	for i, l := range b.lines {
		out[i] = string(l)
	}
	return out
}

// Contents returns the document as a single string, no trailing newline.
func (b *Buffer) Contents() string {
	return strings.Join(b.Lines(), "\n")
}

// Clamp constrains p to a valid position (Col may equal line length).
func (b *Buffer) Clamp(p Pos) Pos {
	if p.Line < 0 {
		p.Line = 0
	}
	if p.Line >= len(b.lines) {
		p.Line = len(b.lines) - 1
	}
	if p.Col < 0 {
		p.Col = 0
	}
	if n := len(b.lines[p.Line]); p.Col > n {
		p.Col = n
	}
	return p
}

// Slice returns the text between start and end (end exclusive), with lines
// joined by \n. Both positions must be valid and start must not follow end.
func (b *Buffer) Slice(start, end Pos) string {
	if start.Line == end.Line {
		return string(b.lines[start.Line][start.Col:end.Col])
	}
	var sb strings.Builder
	sb.WriteString(string(b.lines[start.Line][start.Col:]))
	for i := start.Line + 1; i < end.Line; i++ {
		sb.WriteByte('\n')
		sb.WriteString(string(b.lines[i]))
	}
	sb.WriteByte('\n')
	sb.WriteString(string(b.lines[end.Line][:end.Col]))
	return sb.String()
}

// Replace substitutes the text between start and end (end exclusive) with
// text, records the change for undo, and returns the position just past the
// inserted text.
func (b *Buffer) Replace(start, end Pos, text string) Pos {
	old := b.Slice(start, end)
	if old == text {
		return end
	}
	endPos := b.apply(start, end, text)
	b.record(change{start: start, oldText: old, newText: text})
	b.dirty = true
	b.redo = nil
	return endPos
}

// SetLines replaces the whole document as a single undoable change.
func (b *Buffer) SetLines(lines []string) {
	last := len(b.lines) - 1
	end := Pos{Line: last, Col: len(b.lines[last])}
	b.Replace(Pos{}, end, strings.Join(lines, "\n"))
}

// BeginGroup opens an undo group; subsequent Replace calls join it until
// EndGroup. cursor is where the cursor returns on undo.
func (b *Buffer) BeginGroup(cursor Pos) {
	if b.open == nil {
		b.open = &group{cursor: cursor}
	}
}

// EndGroup closes the open undo group, if any.
func (b *Buffer) EndGroup() {
	if b.open == nil {
		return
	}
	if len(b.open.changes) > 0 {
		b.undo = append(b.undo, *b.open)
	}
	b.open = nil
}

// Undo reverts the most recent change group. It returns the cursor position
// recorded when the group began and whether anything was undone.
func (b *Buffer) Undo() (Pos, bool) {
	b.EndGroup()
	if len(b.undo) == 0 {
		return Pos{}, false
	}
	g := b.undo[len(b.undo)-1]
	b.undo = b.undo[:len(b.undo)-1]
	for i := len(g.changes) - 1; i >= 0; i-- {
		c := g.changes[i]
		b.apply(c.start, endOf(c.start, c.newText), c.oldText)
	}
	b.redo = append(b.redo, g)
	b.dirty = true
	return b.Clamp(g.cursor), true
}

// Redo reapplies the most recently undone group.
func (b *Buffer) Redo() (Pos, bool) {
	if len(b.redo) == 0 {
		return Pos{}, false
	}
	g := b.redo[len(b.redo)-1]
	b.redo = b.redo[:len(b.redo)-1]
	var last Pos
	for _, c := range g.changes {
		last = b.apply(c.start, endOf(c.start, c.oldText), c.newText)
	}
	b.undo = append(b.undo, g)
	b.dirty = true
	return b.Clamp(last), true
}

// apply performs the splice without touching history.
func (b *Buffer) apply(start, end Pos, text string) Pos {
	prefix := string(b.lines[start.Line][:start.Col])
	suffix := string(b.lines[end.Line][end.Col:])
	mid := strings.Split(text, "\n")
	last := len(mid) - 1

	newLines := make([][]rune, len(mid))
	var endPos Pos
	if last == 0 {
		newLines[0] = []rune(prefix + mid[0] + suffix)
		endPos = Pos{Line: start.Line, Col: start.Col + len([]rune(mid[0]))}
	} else {
		newLines[0] = []rune(prefix + mid[0])
		for i := 1; i < last; i++ {
			newLines[i] = []rune(mid[i])
		}
		newLines[last] = []rune(mid[last] + suffix)
		endPos = Pos{Line: start.Line + last, Col: len([]rune(mid[last]))}
	}

	lines := make([][]rune, 0, len(b.lines)-(end.Line-start.Line+1)+len(newLines))
	lines = append(lines, b.lines[:start.Line]...)
	lines = append(lines, newLines...)
	lines = append(lines, b.lines[end.Line+1:]...)
	b.lines = lines
	return endPos
}

func (b *Buffer) record(c change) {
	if b.open != nil {
		b.open.changes = append(b.open.changes, c)
		return
	}
	b.undo = append(b.undo, group{changes: []change{c}, cursor: c.start})
}

// endOf computes the position just past text placed at start.
func endOf(start Pos, text string) Pos {
	lines := strings.Split(text, "\n")
	last := len(lines) - 1
	if last == 0 {
		return Pos{Line: start.Line, Col: start.Col + len([]rune(lines[0]))}
	}
	return Pos{Line: start.Line + last, Col: len([]rune(lines[last]))}
}
