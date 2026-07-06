// Package render computes the soft-wrap layout: logical buffer lines
// broken into display rows at word boundaries, with position mapping in
// both directions. Display-line motions (j/k in prose) and the viewport
// are built on this mapping.
package render

import "github.com/henrypoydar/cowrite/internal/buffer"

// Lines is the read-only buffer view the layout is computed from.
type Lines interface {
	Line(i int) []rune
	LineCount() int
}

// Row is one display row: the span [Start,End) of logical line Line.
type Row struct {
	Line       int
	Start, End int
}

type Layout struct {
	Rows  []Row
	Width int
	first []int // first[line] = index in Rows of the line's first row
}

// Wrap breaks every logical line into rows at most width runes wide,
// preferring to break just after a space.
func Wrap(l Lines, width int) Layout {
	if width < 1 {
		width = 1
	}
	ly := Layout{Width: width, first: make([]int, l.LineCount())}
	for i := range l.LineCount() {
		ly.first[i] = len(ly.Rows)
		line := l.Line(i)
		start := 0
		for {
			if len(line)-start <= width {
				ly.Rows = append(ly.Rows, Row{Line: i, Start: start, End: len(line)})
				break
			}
			brk := start + width
			for j := start + width; j > start; j-- {
				if line[j-1] == ' ' {
					brk = j
					break
				}
			}
			ly.Rows = append(ly.Rows, Row{Line: i, Start: start, End: brk})
			start = brk
		}
	}
	return ly
}

// lastOfLine reports whether row r is its logical line's final row.
func (ly Layout) lastOfLine(r int) bool {
	return r == len(ly.Rows)-1 || ly.Rows[r+1].Line != ly.Rows[r].Line
}

// PosToRow maps a buffer position to (display row, column within row).
// A cursor sitting at end-of-line maps onto the line's last row, one past
// its final rune.
func (ly Layout) PosToRow(p buffer.Pos) (int, int) {
	r := ly.first[p.Line]
	for {
		row := ly.Rows[r]
		if p.Col < row.End || ly.lastOfLine(r) {
			return r, p.Col - row.Start
		}
		r++
	}
}

// RowToPos maps (display row, wanted column) back to a buffer position,
// clamping the column into the row. Used for j/k over wrapped lines.
func (ly Layout) RowToPos(r, col int) buffer.Pos {
	r = max(0, min(r, len(ly.Rows)-1))
	row := ly.Rows[r]
	if col < 0 {
		col = 0
	}
	limit := row.End - row.Start
	if !ly.lastOfLine(r) && limit > 0 {
		limit-- // stay on this visual row, not the first rune of the next
	}
	if col > limit {
		col = limit
	}
	return buffer.Pos{Line: row.Line, Col: row.Start + col}
}
