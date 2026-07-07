// Package app is the Bubble Tea model that wires buffer, vim, render, and
// filesync into an editor. The update loop is the only writer to model
// state; the watcher goroutine and debounce timer only deliver messages.
package app

import (
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/henrypoydar/cowrite/internal/buffer"
	"github.com/henrypoydar/cowrite/internal/filesync"
	"github.com/henrypoydar/cowrite/internal/render"
	"github.com/henrypoydar/cowrite/internal/vim"
)

const saveDelay = 400 * time.Millisecond

type register struct {
	text     string
	linewise bool
}

type visual struct {
	active   bool
	linewise bool
	anchor   buffer.Pos
}

type (
	extChangeMsg filesync.Change
	saveTickMsg  int
	hlClearMsg   int
)

// maxTextWidth caps the wrap column: full-terminal prose lines are hard
// to read, so the text block is capped and centered.
const maxTextWidth = 80

type Model struct {
	path    string
	buf     *buffer.Buffer
	eng     *vim.Engine
	fs      *filesync.Engine
	changes <-chan filesync.Change
	done    chan struct{}

	width, height int
	layout        render.Layout
	top           int // first visible display row
	cursor        buffer.Pos
	goal          int // visual column j/k aims for
	reg           register
	visual        visual
	lastSearch    string
	msg           string
	msgCo         bool // style the message as a co-writer notice
	editGen       int
	eagerSave     bool // save on the very first edit: closes the new-file race
	pad           int  // left padding centering the capped text column

	// merge visibility: which lines the co-writer's last merge touched
	hlLines       map[int]bool
	hlGen         int
	lastMergeLine int

	// dot-repeat: the last completed change as replayable commands
	lastChange []vim.Cmd
	rec        []vim.Cmd
	recOpen    bool
}

func New(path string) (*Model, error) {
	content, err := filesync.Load(path)
	if err != nil {
		return nil, err
	}
	m := &Model{
		path:          path,
		buf:           buffer.New(content),
		eng:           vim.New(),
		fs:            filesync.NewEngine(path),
		done:          make(chan struct{}),
		eagerSave:     content == "",
		lastMergeLine: -1,
	}
	m.fs.SetBase(m.buf.Lines())
	m.changes, err = m.fs.Watch(m.done)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Model) Init() tea.Cmd { return m.waitChange() }

func (m *Model) waitChange() tea.Cmd {
	return func() tea.Msg {
		c, ok := <-m.changes
		if !ok {
			return nil
		}
		return extChangeMsg(c)
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer m.crashSave()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.relayout()
		m.scrollIntoView()
		return m, nil

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, m.quit(true)
		}
		m.msg = ""
		m.msgCo = false
		if m.eng.Mode() == vim.ModeInsert && m.insertArrow(msg.Type) {
			m.relayout()
			m.scrollIntoView()
			return m, nil
		}
		var cmds []tea.Cmd
		for _, k := range translate(msg, m.eng.Mode()) {
			for _, c := range m.eng.Feed(k) {
				m.record(c)
				if cmd := m.apply(c); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
		m.relayout()
		m.scrollIntoView()
		return m, tea.Batch(cmds...)

	case extChangeMsg:
		cmd := m.mergeExternal(filesync.Change(msg))
		m.relayout()
		m.scrollIntoView()
		return m, tea.Batch(m.waitChange(), cmd)

	case saveTickMsg:
		if int(msg) == m.editGen && m.buf.Dirty() {
			m.save()
		}
		return m, nil

	case hlClearMsg:
		if int(msg) == m.hlGen {
			m.hlLines = nil
		}
		return m, nil
	}
	if csiEscape(msg) {
		return m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	}
	return m, nil
}

// csiEscape reports whether msg is an unrecognized CSI key sequence meaning
// Escape. Some terminals (Ghostty, per the fixterms spec) encode ctrl+[ as
// "CSI 91;5u" instead of a bare ESC byte so the two are distinguishable;
// Bubble Tea v1 can't parse CSI-u keys and surfaces the sequence only as an
// unexported unknown-sequence message, so we match its []byte shape by
// reflection and decode it ourselves.
func csiEscape(msg tea.Msg) bool {
	v := reflect.ValueOf(msg)
	if !v.IsValid() || v.Kind() != reflect.Slice || v.Type().Elem().Kind() != reflect.Uint8 {
		return false
	}
	s := string(v.Bytes())
	if !strings.HasPrefix(s, "\x1b[") || len(s) < 4 {
		return false
	}
	params := strings.Split(s[2:len(s)-1], ";")
	num := func(i int) int {
		if i >= len(params) {
			return 0
		}
		n, err := strconv.Atoi(params[i])
		if err != nil {
			return -1
		}
		return n
	}
	ctrl := func(mods int) bool { return mods > 0 && (mods-1)&4 != 0 }
	switch s[len(s)-1] {
	case 'u': // fixterms / kitty: CSI code;mods u
		return num(0) == 27 || (num(0) == 91 && ctrl(num(1)))
	case '~': // xterm modifyOtherKeys: CSI 27;mods;code ~
		return num(0) == 27 && (num(2) == 27 || (num(2) == 91 && ctrl(num(1))))
	}
	return false
}

// crashSave is the last line of defense: if editing code panics, flush the
// buffer to <file>.crash before letting Bubble Tea restore the terminal.
func (m *Model) crashSave() {
	if r := recover(); r != nil {
		content := strings.Join(m.buf.Lines(), "\n") + "\n"
		_ = os.WriteFile(m.path+".crash", []byte(content), 0o644)
		panic(r)
	}
}

// insertArrow moves the cursor in insert mode without leaving it; the
// open undo group simply keeps accumulating, which is simpler than vim's
// break-undo-at-arrow rule and rarely noticed.
func (m *Model) insertArrow(t tea.KeyType) bool {
	switch t {
	case tea.KeyLeft:
		if m.cursor.Col > 0 {
			m.cursor.Col--
		}
	case tea.KeyRight:
		m.cursor.Col = min(m.cursor.Col+1, m.buf.LineLen(m.cursor.Line))
	case tea.KeyUp, tea.KeyDown:
		m.relayout()
		row, _ := m.layout.PosToRow(m.cursor)
		if t == tea.KeyUp {
			row--
		} else {
			row++
		}
		m.cursor = m.buf.Clamp(m.layout.RowToPos(row, m.goal))
		return true
	default:
		return false
	}
	m.setGoal()
	return true
}

// translate decodes a terminal key event into engine keys. Arrow keys act
// as motions in normal mode and are ignored elsewhere (for now).
func translate(msg tea.KeyMsg, mode vim.Mode) []vim.Key {
	switch msg.Type {
	case tea.KeyEsc:
		return []vim.Key{{Special: vim.KeyEsc}}
	case tea.KeyEnter:
		return []vim.Key{{Special: vim.KeyEnter}}
	case tea.KeyBackspace:
		return []vim.Key{{Special: vim.KeyBackspace}}
	case tea.KeyCtrlR:
		return []vim.Key{{Special: vim.KeyCtrlR}}
	case tea.KeySpace:
		return []vim.Key{{Rune: ' '}}
	case tea.KeyTab:
		if mode == vim.ModeInsert {
			return []vim.Key{{Rune: ' '}, {Rune: ' '}}
		}
		return nil
	case tea.KeyUp, tea.KeyDown, tea.KeyLeft, tea.KeyRight:
		if mode != vim.ModeNormal {
			return nil
		}
		r := map[tea.KeyType]rune{
			tea.KeyUp: 'k', tea.KeyDown: 'j', tea.KeyLeft: 'h', tea.KeyRight: 'l',
		}[msg.Type]
		return []vim.Key{{Rune: r}}
	case tea.KeyRunes:
		keys := make([]vim.Key, len(msg.Runes))
		for i, r := range msg.Runes {
			keys[i] = vim.Key{Rune: r}
		}
		return keys
	}
	return nil
}

func (m *Model) apply(c vim.Cmd) tea.Cmd {
	switch c.Kind {
	case vim.CmdMove:
		m.move(c.Motion)

	case vim.CmdEnterInsert:
		m.buf.BeginGroup(m.cursor)
		line := m.cursor.Line
		switch c.At {
		case vim.AtAfter:
			m.cursor.Col = min(m.cursor.Col+1, m.buf.LineLen(line))
		case vim.AtLineStart:
			m.cursor.Col = 0
		case vim.AtLineEnd:
			m.cursor.Col = m.buf.LineLen(line)
		case vim.AtLineBelow:
			end := buffer.Pos{Line: line, Col: m.buf.LineLen(line)}
			m.buf.Replace(end, end, "\n")
			m.cursor = buffer.Pos{Line: line + 1, Col: 0}
			return m.edited()
		case vim.AtLineAbove:
			start := buffer.Pos{Line: line, Col: 0}
			m.buf.Replace(start, start, "\n")
			m.cursor = buffer.Pos{Line: line, Col: 0}
			return m.edited()
		}
		m.setGoal()

	case vim.CmdExitInsert:
		m.buf.EndGroup()
		m.cursor.Col = max(0, min(m.cursor.Col-1, m.buf.LineLen(m.cursor.Line)-1))
		m.setGoal()

	case vim.CmdInsertText:
		m.cursor = m.buf.Replace(m.cursor, m.cursor, c.Text)
		m.setGoal()
		return m.edited()

	case vim.CmdNewline:
		m.cursor = m.buf.Replace(m.cursor, m.cursor, "\n")
		m.setGoal()
		return m.edited()

	case vim.CmdBackspace:
		if m.cursor.Col > 0 {
			start := buffer.Pos{Line: m.cursor.Line, Col: m.cursor.Col - 1}
			m.buf.Replace(start, m.cursor, "")
			m.cursor = start
		} else if m.cursor.Line > 0 {
			start := buffer.Pos{Line: m.cursor.Line - 1, Col: m.buf.LineLen(m.cursor.Line - 1)}
			m.buf.Replace(start, buffer.Pos{Line: m.cursor.Line, Col: 0}, "")
			m.cursor = start
		} else {
			return nil
		}
		m.setGoal()
		return m.edited()

	case vim.CmdDelete, vim.CmdChange, vim.CmdYank:
		return m.operate(c)

	case vim.CmdEnterVisual:
		m.visual = visual{active: true, linewise: c.Linewise, anchor: m.cursor}

	case vim.CmdExitVisual:
		m.visual.active = false

	case vim.CmdSelectObject:
		start, end, linewise := vim.Object(c.Motion, m.buf, m.cursor)
		if start == end {
			return nil
		}
		if linewise {
			m.visual.linewise = true
			m.visual.anchor = buffer.Pos{Line: start.Line, Col: 0}
			m.cursor = m.clampNormal(buffer.Pos{Line: end.Line, Col: 0})
		} else {
			m.visual.anchor = start
			m.cursor = m.clampNormal(buffer.Pos{Line: end.Line, Col: end.Col - 1})
		}
		m.setGoal()

	case vim.CmdJoin:
		return m.join()

	case vim.CmdRepeat:
		return m.repeatLast()

	case vim.CmdPaste:
		return m.paste(c.Before)

	case vim.CmdUndo:
		if pos, ok := m.buf.Undo(); ok {
			m.cursor = m.clampNormal(pos)
			m.setGoal()
			return m.edited()
		}
		m.msg = "already at oldest change"

	case vim.CmdRedo:
		if pos, ok := m.buf.Redo(); ok {
			m.cursor = m.clampNormal(pos)
			m.setGoal()
			return m.edited()
		}
		m.msg = "already at newest change"

	case vim.CmdEx:
		return m.ex(c.Text)

	case vim.CmdSearch:
		m.doSearch(c.Text)

	case vim.CmdSearchNext:
		m.searchMove(c.Before)

	case vim.CmdJumpChange:
		if m.lastMergeLine >= 0 {
			m.cursor = m.clampNormal(buffer.Pos{Line: m.lastMergeLine, Col: 0})
			m.setGoal()
		} else {
			m.msg = "no co-writer changes yet"
		}
	}
	return nil
}

func (m *Model) move(mo vim.Motion) {
	if mo.Kind == vim.MotionUp || mo.Kind == vim.MotionDown {
		// j/k travel display rows so wrapped prose navigates naturally
		m.relayout()
		row, _ := m.layout.PosToRow(m.cursor)
		if mo.Kind == vim.MotionUp {
			row -= max(1, mo.Count)
		} else {
			row += max(1, mo.Count)
		}
		m.cursor = m.clampNormal(m.layout.RowToPos(row, m.goal))
		return
	}
	t := vim.Resolve(mo, m.buf, m.cursor)
	m.cursor = m.clampNormal(t.Pos)
	m.setGoal()
}

// operandRange resolves what an operator acts on — the visual selection,
// a text object, or a motion — as a [start,end) span plus linewise-ness.
func (m *Model) operandRange(c vim.Cmd) (buffer.Pos, buffer.Pos, bool) {
	if c.Selection {
		start, end, _ := m.selectionSpan()
		m.visual.active = false
		if m.visual.linewise {
			return buffer.Pos{Line: start.Line, Col: 0},
				buffer.Pos{Line: end.Line, Col: m.buf.LineLen(end.Line)}, true
		}
		end.Col = min(end.Col+1, m.buf.LineLen(end.Line)) // selection is inclusive
		return start, end, false
	}
	if c.Motion.Kind == vim.MotionObjWord || c.Motion.Kind == vim.MotionObjPara {
		return vim.Object(c.Motion, m.buf, m.cursor)
	}
	t := vim.Resolve(c.Motion, m.buf, m.cursor)
	if t.Linewise {
		lo, hi := m.cursor.Line, t.Pos.Line
		if lo > hi {
			lo, hi = hi, lo
		}
		return buffer.Pos{Line: lo, Col: 0}, buffer.Pos{Line: hi, Col: m.buf.LineLen(hi)}, true
	}
	start, end := m.cursor, t.Pos
	if end.Before(start) {
		start, end = end, start
	}
	if t.Inclusive {
		end.Col = min(end.Col+1, m.buf.LineLen(end.Line))
	}
	return start, end, false
}

// operate applies d, c, or y over a selection, text object, or motion.
func (m *Model) operate(c vim.Cmd) tea.Cmd {
	start, end, linewise := m.operandRange(c)
	// a blank line yields a zero-width linewise range that still means
	// "this whole line" — only charwise ops bail on an empty span
	if start == end && !linewise {
		if c.Kind == vim.CmdChange {
			m.buf.BeginGroup(m.cursor)
		}
		return nil
	}
	m.reg = register{text: m.buf.Slice(start, end), linewise: linewise}

	if c.Kind == vim.CmdYank {
		m.cursor = m.clampNormal(start)
		m.setGoal()
		return nil
	}
	if c.Kind == vim.CmdChange {
		// linewise change clears the lines but keeps one to type into
		m.buf.BeginGroup(m.cursor)
		m.buf.Replace(start, end, "")
		m.cursor = start
		m.setGoal()
		return m.edited()
	}
	if linewise {
		m.deleteLines(start.Line, end.Line)
		m.cursor = m.clampNormal(buffer.Pos{Line: start.Line, Col: 0})
	} else {
		m.buf.Replace(start, end, "")
		m.cursor = m.clampNormal(start)
	}
	m.setGoal()
	return m.edited()
}

// join splices the next line onto the current one, vim J style: newline
// and leading indent become a single space.
func (m *Model) join() tea.Cmd {
	line := m.cursor.Line
	if line >= m.buf.LineCount()-1 {
		return nil
	}
	next := m.buf.Line(line + 1)
	indent := 0
	for indent < len(next) && next[indent] == ' ' {
		indent++
	}
	sep := " "
	if len(next) == indent { // joining a blank line adds no space
		sep = ""
	}
	start := buffer.Pos{Line: line, Col: m.buf.LineLen(line)}
	m.buf.Replace(start, buffer.Pos{Line: line + 1, Col: indent}, sep)
	m.cursor = m.clampNormal(start)
	m.setGoal()
	return m.edited()
}

// record accumulates commands into the dot register. Insert sessions are
// captured whole (enter, keystrokes, exit); standalone changes replace the
// register directly. Selection ops aren't repeatable — the selection is gone.
func (m *Model) record(c vim.Cmd) {
	switch c.Kind {
	case vim.CmdEnterInsert:
		m.rec, m.recOpen = []vim.Cmd{c}, true
	case vim.CmdChange:
		if c.Selection {
			m.rec, m.recOpen = nil, false
			return
		}
		m.rec, m.recOpen = []vim.Cmd{c}, true
	case vim.CmdInsertText, vim.CmdNewline, vim.CmdBackspace:
		if m.recOpen {
			m.rec = append(m.rec, c)
		}
	case vim.CmdExitInsert:
		if m.recOpen {
			m.rec = append(m.rec, c)
			m.lastChange = m.rec
			m.recOpen = false
		}
	case vim.CmdDelete, vim.CmdPaste, vim.CmdJoin:
		m.recOpen = false
		if !c.Selection {
			m.lastChange = []vim.Cmd{c}
		}
	}
}

func (m *Model) repeatLast() tea.Cmd {
	if len(m.lastChange) == 0 {
		return nil
	}
	var last tea.Cmd
	for _, c := range m.lastChange {
		if cmd := m.apply(c); cmd != nil {
			last = cmd
		}
	}
	return last
}

// deleteLines removes whole lines lo..hi including a bounding newline,
// leaving a single empty line when everything goes.
func (m *Model) deleteLines(lo, hi int) {
	last := m.buf.LineCount() - 1
	switch {
	case hi < last:
		m.buf.Replace(buffer.Pos{Line: lo, Col: 0}, buffer.Pos{Line: hi + 1, Col: 0}, "")
	case lo > 0:
		m.buf.Replace(
			buffer.Pos{Line: lo - 1, Col: m.buf.LineLen(lo - 1)},
			buffer.Pos{Line: hi, Col: m.buf.LineLen(hi)}, "")
	default:
		m.buf.Replace(buffer.Pos{}, buffer.Pos{Line: hi, Col: m.buf.LineLen(hi)}, "")
	}
}

func (m *Model) paste(before bool) tea.Cmd {
	if m.reg.text == "" && !m.reg.linewise { // a yanked blank line is "" + linewise
		return nil
	}
	if m.reg.linewise {
		if before {
			start := buffer.Pos{Line: m.cursor.Line, Col: 0}
			m.buf.Replace(start, start, m.reg.text+"\n")
			m.cursor = buffer.Pos{Line: m.cursor.Line, Col: 0}
		} else {
			end := buffer.Pos{Line: m.cursor.Line, Col: m.buf.LineLen(m.cursor.Line)}
			m.buf.Replace(end, end, "\n"+m.reg.text)
			m.cursor = buffer.Pos{Line: m.cursor.Line + 1, Col: 0}
		}
	} else {
		at := m.cursor
		if !before {
			at.Col = min(at.Col+1, m.buf.LineLen(at.Line))
		}
		end := m.buf.Replace(at, at, m.reg.text)
		m.cursor = m.clampNormal(buffer.Pos{Line: end.Line, Col: end.Col - 1})
	}
	m.setGoal()
	return m.edited()
}

func (m *Model) ex(line string) tea.Cmd {
	switch strings.TrimSpace(line) {
	case "w":
		m.save()
	case "q":
		return m.quit(true)
	case "q!":
		return m.quit(false)
	case "wq", "x":
		return m.quit(true)
	case "":
	default:
		m.msg = fmt.Sprintf("not a command: %s", line)
	}
	return nil
}

// quit leaves the editor, first flushing unsaved changes unless the user
// asked not to (:q!). In a continuous-save editor :q refusing to exit over
// "unsaved changes" would be noise — the buffer is already current or about
// to be.
func (m *Model) quit(saveFirst bool) tea.Cmd {
	m.buf.EndGroup()
	if saveFirst && m.buf.Dirty() {
		m.save()
	}
	close(m.done)
	return tea.Quit
}

// edited notes a buffer change and schedules the debounced autosave: the
// tick only saves if no further edit superseded it. The very first edit of
// a buffer that started empty saves immediately — until something is on
// disk there is no base, and an agent write racing the debounce would
// win the whole line (the cold-start race; see DESIGN.md).
func (m *Model) edited() tea.Cmd {
	if m.eagerSave {
		m.eagerSave = false
		m.save()
	}
	m.editGen++
	gen := m.editGen
	return tea.Tick(saveDelay, func(time.Time) tea.Msg { return saveTickMsg(gen) })
}

func (m *Model) save() {
	if err := m.fs.Save(m.buf.Lines()); err != nil {
		m.msg = "save failed: " + err.Error()
		return
	}
	m.buf.MarkClean()
}

// mergeExternal folds an agent's write into the buffer. See DESIGN.md:
// three-way merge against base, disk wins on conflict, cursor follows its
// text, and the whole merge is one undoable group.
func (m *Model) mergeExternal(c filesync.Change) tea.Cmd {
	if c.Err != nil {
		m.msg = "watch error: " + c.Err.Error()
		return nil
	}
	ours := m.buf.Lines()
	merged := filesync.Merge3(m.fs.Base(), ours, c.Lines)
	m.fs.SetBase(c.Lines)
	if slices.Equal(merged, ours) {
		if slices.Equal(merged, c.Lines) {
			m.buf.MarkClean()
			return nil
		}
		return m.edited() // buffer holds text disk lost; save it back out
	}

	// Suspend any open insert-mode group: the agent's turn must be its own
	// undo step, not part of the user's typing.
	wasGrouping := m.buf.Grouping()
	m.buf.EndGroup()
	newLine := filesync.AdjustLine(ours, merged, m.cursor.Line)
	m.buf.BeginGroup(m.cursor)
	m.buf.SetLines(merged)
	m.buf.EndGroup()
	if wasGrouping {
		m.buf.BeginGroup(m.cursor)
	}
	m.noteMerge(filesync.Diff(ours, merged))

	m.cursor = m.buf.Clamp(buffer.Pos{Line: newLine, Col: m.cursor.Col})
	if m.eng.Mode() != vim.ModeInsert {
		m.cursor = m.clampNormal(m.cursor)
	}
	if m.visual.active {
		m.visual.anchor = m.buf.Clamp(m.visual.anchor)
	}

	if slices.Equal(merged, c.Lines) {
		m.buf.MarkClean()
		return m.hlFade()
	}
	return tea.Batch(m.edited(), m.hlFade())
}

// noteMerge records what the co-writer's merge touched: a message with
// line counts, a highlight on the merged lines, and the g; jump target.
func (m *Model) noteMerge(hunks []filesync.Hunk) {
	m.hlLines = map[int]bool{}
	m.lastMergeLine = -1
	added, removed, delta := 0, 0, 0
	for _, h := range hunks {
		start := h.Start + delta
		for i := range h.Lines {
			m.hlLines[start+i] = true
		}
		if m.lastMergeLine == -1 {
			m.lastMergeLine = start
		}
		added += len(h.Lines)
		removed += h.End - h.Start
		delta += len(h.Lines) - (h.End - h.Start)
	}
	m.msg = fmt.Sprintf("co-writer: +%d -%d lines (g; to jump)", added, removed)
	m.msgCo = true
}

// hlFade schedules the merge highlight to clear.
func (m *Model) hlFade() tea.Cmd {
	m.hlGen++
	gen := m.hlGen
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return hlClearMsg(gen) })
}

// clampNormal keeps a normal-mode cursor on a rune (col < len), or col 0 on
// an empty line.
func (m *Model) clampNormal(p buffer.Pos) buffer.Pos {
	p = m.buf.Clamp(p)
	if n := m.buf.LineLen(p.Line); p.Col >= n {
		p.Col = max(0, n-1)
	}
	return p
}

func (m *Model) relayout() {
	textW := min(m.width, maxTextWidth)
	m.layout = render.Wrap(m.buf, max(1, textW))
	m.pad = max(0, (m.width-textW)/2)
}

func (m *Model) setGoal() {
	m.relayout()
	_, m.goal = m.layout.PosToRow(m.cursor)
}

func (m *Model) viewRows() int { return max(1, m.height-3) } // rule + status + message

func (m *Model) scrollIntoView() {
	if m.width == 0 {
		return
	}
	row, _ := m.layout.PosToRow(m.cursor)
	if row < m.top {
		m.top = row
	}
	if row >= m.top+m.viewRows() {
		m.top = row - m.viewRows() + 1
	}
	m.top = max(0, min(m.top, len(m.layout.Rows)-1))
}
