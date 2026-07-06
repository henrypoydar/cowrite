// Package vim implements a modal editing engine as a pure state machine:
// keypresses go in, editing commands come out. It never touches the buffer
// or the terminal, which keeps it fully table-testable.
package vim

type Mode int

const (
	ModeNormal Mode = iota
	ModeInsert
	ModeCommand
	ModeVisual
	ModeVisualLine
)

func (m Mode) String() string {
	switch m {
	case ModeInsert:
		return "INSERT"
	case ModeCommand:
		return "COMMAND"
	case ModeVisual:
		return "VISUAL"
	case ModeVisualLine:
		return "V-LINE"
	default:
		return "NORMAL"
	}
}

type Special int

const (
	KeyNone Special = iota
	KeyEsc
	KeyEnter
	KeyBackspace
	KeyCtrlR
)

// Key is one decoded keypress. Either Rune or Special is set.
type Key struct {
	Rune    rune
	Special Special
}

type CmdKind int

const (
	CmdNone CmdKind = iota
	CmdMove
	CmdEnterInsert
	CmdExitInsert
	CmdInsertText
	CmdBackspace
	CmdNewline
	CmdDelete
	CmdChange // delete + stay for insert; engine is already in ModeInsert
	CmdYank
	CmdPaste
	CmdUndo
	CmdRedo
	CmdJoin
	CmdRepeat // .
	CmdEnterVisual
	CmdExitVisual
	CmdSelectObject // visual mode iw/ip: reshape the selection
	CmdEx           // Text carries the command line, e.g. "wq"
)

// InsertAt says where CmdEnterInsert places the cursor.
type InsertAt int

const (
	AtCursor    InsertAt = iota // i
	AtAfter                     // a
	AtLineStart                 // I
	AtLineEnd                   // A
	AtLineBelow                 // o
	AtLineAbove                 // O
)

type Cmd struct {
	Kind      CmdKind
	Motion    Motion
	Text      string
	At        InsertAt
	Before    bool // paste before cursor (P)
	Selection bool // operator applies to the visual selection
	Linewise  bool // visual line mode (V)
}

// Engine holds pending modal state between keypresses.
type Engine struct {
	mode    Mode
	count   int  // count typed since the operator (or since idle)
	opCount int  // count typed before the operator
	op      rune // pending operator: d, c, y, or 0
	obj     rune // pending text-object qualifier: i or a
	prefixG bool
	find    rune // pending f or t awaiting its target character
	cmdline []rune
}

func New() *Engine { return &Engine{} }

func (e *Engine) Mode() Mode      { return e.mode }
func (e *Engine) Cmdline() string { return string(e.cmdline) }

// Feed processes one keypress and returns the commands it produced.
func (e *Engine) Feed(k Key) []Cmd {
	switch e.mode {
	case ModeInsert:
		return e.insert(k)
	case ModeCommand:
		return e.command(k)
	case ModeVisual, ModeVisualLine:
		return e.visual(k)
	default:
		return e.normal(k)
	}
}

// motionKind maps single-key motions shared by normal and visual mode.
func motionKind(r rune) (MotionKind, bool) {
	switch r {
	case 'h':
		return MotionLeft, true
	case 'l', ' ':
		return MotionRight, true
	case 'j':
		return MotionDown, true
	case 'k':
		return MotionUp, true
	case '0':
		return MotionLineStart, true
	case '$':
		return MotionLineEnd, true
	case 'w':
		return MotionWordForward, true
	case 'b':
		return MotionWordBack, true
	case 'e':
		return MotionWordEnd, true
	case '{':
		return MotionParaBack, true
	case '}':
		return MotionParaForward, true
	}
	return MotionNone, false
}

// pendingMotion handles the multi-key motion machinery shared by normal
// and visual mode: counts, f/t targets, and the g prefix. It returns the
// completed motion if this key finished one, and whether the key was
// consumed either way.
func (e *Engine) pendingMotion(r rune) (Motion, bool, bool) {
	if e.find != 0 {
		kind := MotionFind
		if e.find == 't' {
			kind = MotionTill
		}
		e.find = 0
		return Motion{Kind: kind, Count: e.totalCount(), Char: r}, true, true
	}
	if e.prefixG {
		e.prefixG = false
		if r == 'g' {
			return Motion{Kind: MotionFileStart, Count: e.rawCount()}, true, true
		}
		e.reset()
		return Motion{}, false, true
	}
	if r >= '1' && r <= '9' || (r == '0' && e.count > 0) {
		e.count = e.count*10 + int(r-'0')
		return Motion{}, false, true
	}
	switch r {
	case 'g':
		e.prefixG = true
		return Motion{}, false, true
	case 'f', 't':
		e.find = r
		return Motion{}, false, true
	case 'G':
		return Motion{Kind: MotionFileEnd, Count: e.rawCount()}, true, true
	}
	if kind, ok := motionKind(r); ok {
		return Motion{Kind: kind, Count: e.totalCount()}, true, true
	}
	return Motion{}, false, false
}

func (e *Engine) normal(k Key) []Cmd {
	switch k.Special {
	case KeyEsc:
		e.reset()
		return nil
	case KeyCtrlR:
		e.reset()
		return one(Cmd{Kind: CmdRedo})
	case KeyEnter:
		return e.motion(Motion{Kind: MotionDown, Count: e.totalCount()})
	case KeyBackspace:
		return e.motion(Motion{Kind: MotionLeft, Count: e.totalCount()})
	}
	r := k.Rune
	if r == 0 {
		return nil
	}

	if e.obj != 0 {
		inner := e.obj == 'i'
		e.obj = 0
		switch r {
		case 'w':
			return e.operate(Motion{Kind: MotionObjWord, Inner: inner})
		case 'p':
			return e.operate(Motion{Kind: MotionObjPara, Inner: inner})
		}
		e.reset()
		return nil
	}

	// i/a after an operator introduce a text object, not insert mode —
	// unless a pending f/t is waiting for its target character
	if e.op != 0 && e.find == 0 && !e.prefixG && (r == 'i' || r == 'a') {
		e.obj = r
		return nil
	}

	if m, done, consumed := e.pendingMotion(r); consumed {
		if done {
			return e.motion(m)
		}
		return nil
	}

	switch r {
	case 'd', 'c', 'y':
		if e.op == r {
			return e.operate(Motion{Kind: MotionLine, Count: e.totalCount()})
		}
		if e.op != 0 {
			e.reset()
			return nil
		}
		e.op = r
		e.opCount = e.count
		e.count = 0
		return nil
	case 'x':
		n := e.totalCount()
		e.reset()
		return one(Cmd{Kind: CmdDelete, Motion: Motion{Kind: MotionRight, Count: n}})
	case 'D':
		e.reset()
		return one(Cmd{Kind: CmdDelete, Motion: Motion{Kind: MotionLineEnd, Count: 1}})
	case 'C':
		e.reset()
		e.mode = ModeInsert
		return one(Cmd{Kind: CmdChange, Motion: Motion{Kind: MotionLineEnd, Count: 1}})
	case 'i', 'a', 'I', 'A', 'o', 'O':
		if e.op != 0 {
			e.reset()
			return nil
		}
		e.reset()
		e.mode = ModeInsert
		at := map[rune]InsertAt{
			'i': AtCursor, 'a': AtAfter, 'I': AtLineStart,
			'A': AtLineEnd, 'o': AtLineBelow, 'O': AtLineAbove,
		}[r]
		return one(Cmd{Kind: CmdEnterInsert, At: at})
	case 'v':
		if e.op != 0 {
			e.reset()
			return nil
		}
		e.reset()
		e.mode = ModeVisual
		return one(Cmd{Kind: CmdEnterVisual})
	case 'V':
		if e.op != 0 {
			e.reset()
			return nil
		}
		e.reset()
		e.mode = ModeVisualLine
		return one(Cmd{Kind: CmdEnterVisual, Linewise: true})
	case 'J':
		e.reset()
		return one(Cmd{Kind: CmdJoin})
	case '.':
		e.reset()
		return one(Cmd{Kind: CmdRepeat})
	case 'p':
		e.reset()
		return one(Cmd{Kind: CmdPaste})
	case 'P':
		e.reset()
		return one(Cmd{Kind: CmdPaste, Before: true})
	case 'u':
		e.reset()
		return one(Cmd{Kind: CmdUndo})
	case ':':
		if e.op != 0 {
			e.reset()
			return nil
		}
		e.reset()
		e.mode = ModeCommand
		e.cmdline = nil
		return nil
	}
	e.reset()
	return nil
}

func (e *Engine) visual(k Key) []Cmd {
	switch k.Special {
	case KeyEsc:
		e.reset()
		e.mode = ModeNormal
		return one(Cmd{Kind: CmdExitVisual})
	case KeyEnter:
		return one(Cmd{Kind: CmdMove, Motion: Motion{Kind: MotionDown, Count: e.takeCount()}})
	case KeyBackspace:
		return one(Cmd{Kind: CmdMove, Motion: Motion{Kind: MotionLeft, Count: e.takeCount()}})
	}
	r := k.Rune
	if r == 0 {
		return nil
	}

	if e.obj != 0 {
		inner := e.obj == 'i'
		e.obj = 0
		switch r {
		case 'w':
			return one(Cmd{Kind: CmdSelectObject, Motion: Motion{Kind: MotionObjWord, Inner: inner}})
		case 'p':
			return one(Cmd{Kind: CmdSelectObject, Motion: Motion{Kind: MotionObjPara, Inner: inner}})
		}
		e.reset()
		return nil
	}

	if m, done, consumed := e.pendingMotion(r); consumed {
		if done {
			e.resetCounts()
			return one(Cmd{Kind: CmdMove, Motion: m})
		}
		return nil
	}

	switch r {
	case 'i', 'a':
		e.obj = r
		return nil
	case 'd', 'x':
		e.reset()
		e.mode = ModeNormal
		return one(Cmd{Kind: CmdDelete, Selection: true})
	case 'y':
		e.reset()
		e.mode = ModeNormal
		return one(Cmd{Kind: CmdYank, Selection: true})
	case 'c':
		e.reset()
		e.mode = ModeInsert
		return one(Cmd{Kind: CmdChange, Selection: true})
	case 'v', 'V':
		e.reset()
		e.mode = ModeNormal
		return one(Cmd{Kind: CmdExitVisual})
	}
	e.reset()
	return nil
}

func (e *Engine) insert(k Key) []Cmd {
	switch k.Special {
	case KeyEsc:
		e.mode = ModeNormal
		return one(Cmd{Kind: CmdExitInsert})
	case KeyEnter:
		return one(Cmd{Kind: CmdNewline})
	case KeyBackspace:
		return one(Cmd{Kind: CmdBackspace})
	}
	if k.Rune != 0 {
		return one(Cmd{Kind: CmdInsertText, Text: string(k.Rune)})
	}
	return nil
}

func (e *Engine) command(k Key) []Cmd {
	switch k.Special {
	case KeyEsc:
		e.mode = ModeNormal
		e.cmdline = nil
		return nil
	case KeyEnter:
		line := string(e.cmdline)
		e.cmdline = nil
		e.mode = ModeNormal
		return one(Cmd{Kind: CmdEx, Text: line})
	case KeyBackspace:
		if len(e.cmdline) > 0 {
			e.cmdline = e.cmdline[:len(e.cmdline)-1]
		} else {
			e.mode = ModeNormal
		}
		return nil
	}
	if k.Rune != 0 {
		e.cmdline = append(e.cmdline, k.Rune)
	}
	return nil
}

// motion routes a completed motion: to the pending operator if there is
// one, otherwise as a cursor move.
func (e *Engine) motion(m Motion) []Cmd {
	if e.op != 0 {
		return e.operate(m)
	}
	e.resetCounts()
	return one(Cmd{Kind: CmdMove, Motion: m})
}

func (e *Engine) operate(m Motion) []Cmd {
	op := e.op
	e.reset()
	switch op {
	case 'd':
		return one(Cmd{Kind: CmdDelete, Motion: m})
	case 'c':
		if m.Kind == MotionWordForward {
			m.Kind = MotionWordEnd // vim quirk: cw changes to word end, not through the space
		}
		e.mode = ModeInsert
		return one(Cmd{Kind: CmdChange, Motion: m})
	case 'y':
		return one(Cmd{Kind: CmdYank, Motion: m})
	}
	return nil
}

// totalCount multiplies counts before and after the operator, as vim does
// (2d3w = 6 words), defaulting to 1.
func (e *Engine) totalCount() int {
	return max(1, e.count) * max(1, e.opCount)
}

// rawCount is like totalCount but 0 when no count was typed at all —
// motions like G treat "no count" differently from "count 1".
func (e *Engine) rawCount() int {
	if e.count == 0 && e.opCount == 0 {
		return 0
	}
	return e.totalCount()
}

// takeCount returns totalCount and clears it.
func (e *Engine) takeCount() int {
	n := e.totalCount()
	e.resetCounts()
	return n
}

func (e *Engine) reset() {
	e.resetCounts()
	e.op = 0
	e.obj = 0
	e.prefixG = false
	e.find = 0
}

func (e *Engine) resetCounts() {
	e.count, e.opCount = 0, 0
}

func one(c Cmd) []Cmd { return []Cmd{c} }
