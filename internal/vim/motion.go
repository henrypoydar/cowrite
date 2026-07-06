package vim

import (
	"unicode"

	"github.com/henrypoydar/cowrite/internal/buffer"
)

type MotionKind int

const (
	MotionNone MotionKind = iota
	MotionLeft
	MotionRight
	MotionUp
	MotionDown
	MotionLineStart
	MotionLineEnd
	MotionWordForward
	MotionWordBack
	MotionWordEnd
	MotionFileStart
	MotionFileEnd
	MotionFind // f
	MotionTill // t
	MotionLine // whole-line target used by dd/cc/yy
)

type Motion struct {
	Kind  MotionKind
	Count int
	Char  rune // target for f/t
}

// Lines is the read-only buffer view motions resolve against.
type Lines interface {
	Line(i int) []rune
	LineCount() int
}

// Target is where a motion lands and how an operator should treat the span.
type Target struct {
	Pos       buffer.Pos
	Linewise  bool
	Inclusive bool // operator span includes the target rune (e, f)
}

// Resolve computes the motion's target from cur. Up/Down resolve against
// logical lines here; plain-move j/k are handled by the app using the
// display-line layout instead.
func Resolve(m Motion, l Lines, cur buffer.Pos) Target {
	n := max(1, m.Count)
	lineLen := len(l.Line(cur.Line))
	lastLine := l.LineCount() - 1

	switch m.Kind {
	case MotionLeft:
		return Target{Pos: buffer.Pos{Line: cur.Line, Col: max(0, cur.Col-n)}}
	case MotionRight:
		return Target{Pos: buffer.Pos{Line: cur.Line, Col: min(lineLen, cur.Col+n)}}
	case MotionUp:
		return Target{Pos: buffer.Pos{Line: max(0, cur.Line-n), Col: cur.Col}, Linewise: true}
	case MotionDown:
		return Target{Pos: buffer.Pos{Line: min(lastLine, cur.Line+n), Col: cur.Col}, Linewise: true}
	case MotionLineStart:
		return Target{Pos: buffer.Pos{Line: cur.Line, Col: 0}}
	case MotionLineEnd:
		line := min(lastLine, cur.Line+n-1) // 2$ ends on the next line
		return Target{Pos: buffer.Pos{Line: line, Col: len(l.Line(line))}}
	case MotionFileStart:
		line := 0
		if m.Count > 0 {
			line = min(m.Count-1, lastLine)
		}
		return Target{Pos: buffer.Pos{Line: line, Col: 0}, Linewise: true}
	case MotionFileEnd:
		line := lastLine
		if m.Count > 0 {
			line = min(m.Count-1, lastLine)
		}
		return Target{Pos: buffer.Pos{Line: line, Col: 0}, Linewise: true}
	case MotionWordForward:
		p := cur
		for range n {
			p = wordForward(l, p)
		}
		return Target{Pos: p}
	case MotionWordBack:
		p := cur
		for range n {
			p = wordBack(l, p)
		}
		return Target{Pos: p}
	case MotionWordEnd:
		p := cur
		for range n {
			p = wordEnd(l, p)
		}
		return Target{Pos: p, Inclusive: true}
	case MotionFind, MotionTill:
		line := l.Line(cur.Line)
		col := cur.Col
		for range n {
			found := -1
			for j := col + 1; j < len(line); j++ {
				if line[j] == m.Char {
					found = j
					break
				}
			}
			if found == -1 {
				return Target{Pos: cur} // no move at all if any hop fails
			}
			col = found
		}
		if m.Kind == MotionTill {
			col--
		}
		return Target{Pos: buffer.Pos{Line: cur.Line, Col: col}, Inclusive: true}
	case MotionLine:
		return Target{Pos: buffer.Pos{Line: min(lastLine, cur.Line+n-1), Col: 0}, Linewise: true}
	}
	return Target{Pos: cur}
}

// class buckets a rune the way vim words do: whitespace, word characters
// (letters, digits, underscore), or other punctuation.
func class(r rune) int {
	switch {
	case unicode.IsSpace(r):
		return 0
	case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
		return 1
	default:
		return 2
	}
}

func wordForward(l Lines, p buffer.Pos) buffer.Pos {
	line := l.Line(p.Line)
	if p.Col < len(line) {
		c := class(line[p.Col])
		if c != 0 {
			for p.Col < len(line) && class(line[p.Col]) == c {
				p.Col++
			}
		}
	}
	for {
		line = l.Line(p.Line)
		for p.Col < len(line) && class(line[p.Col]) == 0 {
			p.Col++
		}
		if p.Col < len(line) {
			return p
		}
		if p.Line == l.LineCount()-1 {
			return p
		}
		p.Line++
		p.Col = 0
		if len(l.Line(p.Line)) == 0 {
			return p // an empty line is a word stop
		}
	}
}

func wordBack(l Lines, p buffer.Pos) buffer.Pos {
	for {
		if p.Col == 0 {
			if p.Line == 0 {
				return p
			}
			p.Line--
			p.Col = len(l.Line(p.Line))
			if p.Col == 0 {
				return p // empty line stop
			}
		} else {
			p.Col--
		}
		line := l.Line(p.Line)
		if p.Col >= len(line) || class(line[p.Col]) == 0 {
			continue
		}
		c := class(line[p.Col])
		for p.Col > 0 && class(line[p.Col-1]) == c {
			p.Col--
		}
		return p
	}
}

func wordEnd(l Lines, p buffer.Pos) buffer.Pos {
	q, ok := advance(l, p)
	if !ok {
		return p
	}
	p = q
	for {
		line := l.Line(p.Line)
		if p.Col < len(line) && class(line[p.Col]) != 0 {
			break
		}
		if q, ok = advance(l, p); !ok {
			return p
		}
		p = q
	}
	line := l.Line(p.Line)
	c := class(line[p.Col])
	for p.Col+1 < len(line) && class(line[p.Col+1]) == c {
		p.Col++
	}
	return p
}

func advance(l Lines, p buffer.Pos) (buffer.Pos, bool) {
	if p.Col+1 < len(l.Line(p.Line)) {
		return buffer.Pos{Line: p.Line, Col: p.Col + 1}, true
	}
	if p.Line+1 >= l.LineCount() {
		return p, false
	}
	return buffer.Pos{Line: p.Line + 1, Col: 0}, true
}
