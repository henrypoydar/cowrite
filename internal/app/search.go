package app

import (
	"fmt"

	"github.com/henrypoydar/cowrite/internal/buffer"
)

// Search is plain substring matching, case-sensitive, within a line —
// enough to find your place in prose. n and N continue with wrap-around.

// doSearch handles / <pattern> <enter>. An empty pattern repeats the last.
func (m *Model) doSearch(pattern string) {
	if pattern != "" {
		m.lastSearch = pattern
	}
	m.searchMove(false)
}

// searchMove jumps to the next (or previous) match, wrapping.
func (m *Model) searchMove(reverse bool) {
	if m.lastSearch == "" {
		m.msg = "no previous search"
		return
	}
	if pos, ok := m.findMatch(m.cursor, reverse); ok {
		m.cursor = m.clampNormal(pos)
		m.setGoal()
	} else {
		m.msg = fmt.Sprintf("pattern not found: %s", m.lastSearch)
	}
}

// findMatch scans from just past cur, wrapping around the buffer once.
func (m *Model) findMatch(cur buffer.Pos, reverse bool) (buffer.Pos, bool) {
	pat := []rune(m.lastSearch)
	n := m.buf.LineCount()
	if reverse {
		for i := range n + 1 {
			line := (cur.Line - i + 2*n) % n
			limit := -1 // whole line
			if i == 0 {
				limit = cur.Col // strictly before the cursor
			}
			if col := lastIndexRunes(m.buf.Line(line), pat, limit); col != -1 {
				return buffer.Pos{Line: line, Col: col}, true
			}
		}
		return buffer.Pos{}, false
	}
	for i := range n + 1 {
		line := (cur.Line + i) % n
		from := 0
		if i == 0 {
			from = cur.Col + 1 // strictly after the cursor
		}
		if col := indexRunes(m.buf.Line(line), pat, from); col != -1 {
			return buffer.Pos{Line: line, Col: col}, true
		}
	}
	return buffer.Pos{}, false
}

// indexRunes finds pat in line at or after from, by rune offset.
func indexRunes(line, pat []rune, from int) int {
	if len(pat) == 0 {
		return -1
	}
	for i := max(0, from); i+len(pat) <= len(line); i++ {
		if matchAt(line, pat, i) {
			return i
		}
	}
	return -1
}

// lastIndexRunes finds the last match starting strictly before limit
// (limit -1 means anywhere in the line).
func lastIndexRunes(line, pat []rune, limit int) int {
	if len(pat) == 0 {
		return -1
	}
	last := len(line) - len(pat)
	if limit >= 0 {
		last = min(last, limit-1)
	}
	for i := last; i >= 0; i-- {
		if matchAt(line, pat, i) {
			return i
		}
	}
	return -1
}

func matchAt(line, pat []rune, at int) bool {
	for k, r := range pat {
		if line[at+k] != r {
			return false
		}
	}
	return true
}
