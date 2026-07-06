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
	MotionFind          // f
	MotionTill          // t
	MotionLine          // whole-line target used by dd/cc/yy
	MotionParaForward   // }
	MotionParaBack      // {
	MotionObjWord       // iw / aw text object
	MotionObjPara       // ip / ap text object
	MotionFirstNonBlank // ^
)

type Motion struct {
	Kind  MotionKind
	Count int
	Char  rune // target for f/t
	Inner bool // i vs a for text objects
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
	case MotionFirstNonBlank:
		line := l.Line(cur.Line)
		col := 0
		for col < len(line) && (line[col] == ' ' || line[col] == '\t') {
			col++
		}
		if col == len(line) {
			col = 0
		}
		return Target{Pos: buffer.Pos{Line: cur.Line, Col: col}}
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
	case MotionParaForward:
		line := cur.Line
		for range n {
			line = nextBlank(l, line)
		}
		return Target{Pos: buffer.Pos{Line: line, Col: 0}}
	case MotionParaBack:
		line := cur.Line
		for range n {
			line = prevBlank(l, line)
		}
		return Target{Pos: buffer.Pos{Line: line, Col: 0}}
	}
	return Target{Pos: cur}
}

// nextBlank finds the next empty line after from (vim's }), or the last line.
func nextBlank(l Lines, from int) int {
	for i := from + 1; i < l.LineCount(); i++ {
		if len(l.Line(i)) == 0 {
			return i
		}
	}
	return l.LineCount() - 1
}

// prevBlank finds the previous empty line before from (vim's {), or line 0.
func prevBlank(l Lines, from int) int {
	for i := from - 1; i >= 0; i-- {
		if len(l.Line(i)) == 0 {
			return i
		}
	}
	return 0
}

// Object resolves a text object at cur to a [start,end) span. Word objects
// are charwise; paragraph objects are linewise (end spans the last line).
func Object(m Motion, l Lines, cur buffer.Pos) (start, end buffer.Pos, linewise bool) {
	switch m.Kind {
	case MotionObjWord:
		line := l.Line(cur.Line)
		if len(line) == 0 {
			return cur, cur, false
		}
		col := min(cur.Col, len(line)-1)
		c := class(line[col])
		s, e := col, col+1
		for s > 0 && class(line[s-1]) == c {
			s--
		}
		for e < len(line) && class(line[e]) == c {
			e++
		}
		if !m.Inner { // aw: take the trailing spaces, or leading if none trail
			e2 := e
			for e2 < len(line) && line[e2] == ' ' {
				e2++
			}
			if e2 == e {
				for s > 0 && line[s-1] == ' ' {
					s--
				}
			}
			e = e2
		}
		return buffer.Pos{Line: cur.Line, Col: s}, buffer.Pos{Line: cur.Line, Col: e}, false

	case MotionObjPara:
		blank := func(i int) bool { return len(l.Line(i)) == 0 }
		last := l.LineCount() - 1
		on := blank(cur.Line) // ip on a blank run selects the blank run
		lo, hi := cur.Line, cur.Line
		for lo > 0 && blank(lo-1) == on {
			lo--
		}
		for hi < last && blank(hi+1) == on {
			hi++
		}
		if !m.Inner && !on { // ap: include the trailing blank run
			for hi < last && blank(hi+1) {
				hi++
			}
		}
		return buffer.Pos{Line: lo, Col: 0}, buffer.Pos{Line: hi, Col: len(l.Line(hi))}, true
	}
	return cur, cur, false
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
