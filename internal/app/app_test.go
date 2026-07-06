package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/henrypoydar/cowrite/internal/buffer"
	"github.com/henrypoydar/cowrite/internal/filesync"
	"github.com/henrypoydar/cowrite/internal/vim"
)

func newModel(t *testing.T, content string) (*Model, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.md")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	return m, path
}

// press feeds a key string through the model; \x1b and \r map to specials.
func press(m *Model, s string) {
	for _, r := range s {
		var msg tea.KeyMsg
		switch r {
		case '\x1b':
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case '\r':
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case ' ':
			msg = tea.KeyMsg{Type: tea.KeySpace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		}
		m.Update(msg)
	}
}

func TestTypeAndSave(t *testing.T) {
	m, path := newModel(t, "")
	press(m, "iHello, world\x1b:w\r")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "Hello, world\n" {
		t.Errorf("file = %q", data)
	}
	if m.buf.Dirty() {
		t.Error("buffer should be clean after :w")
	}
}

func TestEditingCommands(t *testing.T) {
	m, _ := newModel(t, "one two three\nsecond line\n")

	press(m, "dw")
	if got := m.buf.Lines()[0]; got != "two three" {
		t.Errorf("dw: %q", got)
	}
	press(m, "u")
	if got := m.buf.Lines()[0]; got != "one two three" {
		t.Errorf("undo: %q", got)
	}
	press(m, "dd")
	if got := m.buf.Contents(); got != "second line" {
		t.Errorf("dd: %q", got)
	}
	press(m, "p")
	if got := m.buf.Contents(); got != "second line\none two three" {
		t.Errorf("p after dd: %q", got)
	}
	press(m, "ggcwfirst\x1b")
	if got := m.buf.Lines()[0]; got != "first line" {
		t.Errorf("cw: %q", got)
	}
}

func TestInsertSessionIsOneUndo(t *testing.T) {
	m, _ := newModel(t, "start\n")
	press(m, "A abc def\x1b")
	if got := m.buf.Contents(); got != "start abc def" {
		t.Fatalf("insert: %q", got)
	}
	press(m, "u")
	if got := m.buf.Contents(); got != "start" {
		t.Errorf("one undo should revert the whole insert session: %q", got)
	}
}

func TestAutosaveDebounce(t *testing.T) {
	m, path := newModel(t, "seed\n")
	press(m, "Amore\x1b")

	// stale tick (superseded generation) must not save
	m.Update(saveTickMsg(m.editGen - 1))
	if data, _ := os.ReadFile(path); string(data) != "seed\n" {
		t.Fatalf("stale tick saved the file: %q", data)
	}
	// current tick saves
	m.Update(saveTickMsg(m.editGen))
	if data, _ := os.ReadFile(path); string(data) != "seedmore\n" {
		t.Errorf("file = %q", data)
	}
}

func TestEagerFirstSaveOfEmptyBuffer(t *testing.T) {
	m, path := newModel(t, "")
	press(m, "if") // a single keystroke, no debounce tick yet
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("first edit of an empty buffer should save immediately")
	}
	if string(data) != "f\n" {
		t.Errorf("file = %q", data)
	}
	_ = m
}

func TestMergeCleanBuffer(t *testing.T) {
	m, _ := newModel(t, "title\n\npara one\n")
	press(m, ":w\r")

	// agent appends while our buffer is clean
	m.Update(extChangeMsg(filesync.Change{
		Lines: []string{"title", "", "para one", "", "para two"},
	}))
	if got := m.buf.Contents(); got != "title\n\npara one\n\npara two" {
		t.Errorf("merge: %q", got)
	}
	if m.buf.Dirty() {
		t.Error("buffer should be clean: it matches disk")
	}
	// the agent's turn is one undo step
	press(m, "u")
	if got := m.buf.Contents(); got != "title\n\npara one" {
		t.Errorf("undo of agent turn: %q", got)
	}
}

func TestMergeDirtyBufferKeepsBothSides(t *testing.T) {
	m, _ := newModel(t, "title\n\npara one\n")
	press(m, ":w\r")

	// we edit the title; agent appends a paragraph — disjoint regions
	press(m, "ggcwTITLE\x1b")
	m.Update(extChangeMsg(filesync.Change{
		Lines: []string{"title", "", "para one", "", "para two"},
	}))
	if got := m.buf.Contents(); got != "TITLE\n\npara one\n\npara two" {
		t.Errorf("merge kept: %q", got)
	}
	if !m.buf.Dirty() {
		t.Error("buffer holds our edit disk lacks; must be dirty for autosave")
	}
}

func TestMergeConflictDiskWins(t *testing.T) {
	m, _ := newModel(t, "title\n\npara one\n")
	press(m, ":w\r")

	press(m, "GcwOURS\x1b") // rewrite "para" on the last saved line
	m.Update(extChangeMsg(filesync.Change{
		Lines: []string{"title", "", "THEIRS one"},
	}))
	if got := m.buf.Contents(); got != "title\n\nTHEIRS one" {
		t.Errorf("conflict resolution: %q", got)
	}
}

func TestMergeCursorFollowsText(t *testing.T) {
	m, _ := newModel(t, "alpha\nbeta\ngamma\n")
	press(m, ":w\rjj") // cursor on gamma, line 2

	m.Update(extChangeMsg(filesync.Change{
		Lines: []string{"alpha", "inserted", "beta", "gamma"},
	}))
	if m.cursor.Line != 3 {
		t.Errorf("cursor line = %d, want 3 (still on gamma)", m.cursor.Line)
	}
}

func TestWatcherRoundTrip(t *testing.T) {
	m, path := newModel(t, "start\n")

	// run Init's watcher wait in the background, as tea.Program would
	got := make(chan tea.Msg, 1)
	go func() { got <- m.Init()() }()

	if err := os.WriteFile(path, []byte("start\nagent line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-got:
		m.Update(msg)
	case <-time.After(2 * time.Second):
		t.Fatal("watcher never delivered the external write")
	}
	if got := m.buf.Contents(); got != "start\nagent line" {
		t.Errorf("after watcher merge: %q", got)
	}
}

func TestQuitSavesByDefault(t *testing.T) {
	m, path := newModel(t, "")
	press(m, "iabc\x1b:q\r")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "abc\n" {
		t.Errorf(":q should flush: %q", data)
	}
}

func TestVisualModeOps(t *testing.T) {
	m, _ := newModel(t, "one two three\nnext line\n")

	// charwise: select "one two" and delete it
	press(m, "vwed")
	if got := m.buf.Lines()[0]; got != " three" {
		t.Errorf("vwed: %q", got)
	}
	if m.visual.active {
		t.Error("selection should end after the operator")
	}

	// linewise: V selects the whole line
	press(m, "Vd")
	if got := m.buf.Contents(); got != "next line" {
		t.Errorf("Vd: %q", got)
	}
}

func TestTextObjects(t *testing.T) {
	m, _ := newModel(t, "alpha beta gamma\n\npara two here\n\npara three\n")

	press(m, "wdiw") // cursor onto beta, delete inner word
	if got := m.buf.Lines()[0]; got != "alpha  gamma" {
		t.Errorf("diw: %q", got)
	}
	press(m, "udaw") // undo, then delete a word (with trailing space)
	if got := m.buf.Lines()[0]; got != "alpha gamma" {
		t.Errorf("daw: %q", got)
	}

	m, _ = newModel(t, "para one\nstill one\n\npara two\n")
	press(m, "dip") // delete the whole first paragraph, linewise
	if got := m.buf.Contents(); got != "\npara two" {
		t.Errorf("dip: %q", got)
	}
}

func TestParagraphMotionAndJoin(t *testing.T) {
	m, _ := newModel(t, "para one\nstill one\n\npara two\n")
	press(m, "}")
	if m.cursor.Line != 2 {
		t.Errorf("} landed on line %d, want 2 (the blank)", m.cursor.Line)
	}
	press(m, "gg")
	press(m, "J")
	if got := m.buf.Lines()[0]; got != "para one still one" {
		t.Errorf("J: %q", got)
	}
}

func TestDotRepeat(t *testing.T) {
	m, _ := newModel(t, "aaa bbb ccc\n")
	press(m, "dw..") // delete word, repeat twice
	if got := m.buf.Contents(); got != "" {
		t.Errorf("dw..: %q", got)
	}

	m, _ = newModel(t, "x\n")
	press(m, "A!\x1b.") // append '!', repeat the whole insert session
	if got := m.buf.Contents(); got != "x!!" {
		t.Errorf("A!.: %q", got)
	}
}

func TestSearch(t *testing.T) {
	m, _ := newModel(t, "alpha beta\ngamma beta\nbeta again\n")

	press(m, "/beta\r")
	if m.cursor != (buffer.Pos{Line: 0, Col: 6}) {
		t.Errorf("/beta landed at %v", m.cursor)
	}
	press(m, "n")
	if m.cursor != (buffer.Pos{Line: 1, Col: 6}) {
		t.Errorf("n landed at %v", m.cursor)
	}
	press(m, "nn") // line 2, then wrap back to line 0
	if m.cursor != (buffer.Pos{Line: 0, Col: 6}) {
		t.Errorf("wrap landed at %v", m.cursor)
	}
	press(m, "N") // reverse wraps to line 2
	if m.cursor != (buffer.Pos{Line: 2, Col: 0}) {
		t.Errorf("N landed at %v", m.cursor)
	}
	press(m, "/nosuch\r")
	if m.msg != "pattern not found: nosuch" {
		t.Errorf("msg = %q", m.msg)
	}
	if m.cursor != (buffer.Pos{Line: 2, Col: 0}) {
		t.Errorf("failed search moved the cursor to %v", m.cursor)
	}
	// a failed search still becomes the last pattern (as in vim), so
	// re-seed before testing that an empty / repeats it
	press(m, "/beta\rgg/\r")
	if m.cursor != (buffer.Pos{Line: 0, Col: 6}) {
		t.Errorf("empty / repeat landed at %v", m.cursor)
	}
}

func TestFirstNonBlank(t *testing.T) {
	m, _ := newModel(t, "   indented text\n")
	press(m, "$^")
	if m.cursor != (buffer.Pos{Line: 0, Col: 3}) {
		t.Errorf("^ landed at %v", m.cursor)
	}
}

func TestInsertArrows(t *testing.T) {
	m, _ := newModel(t, "hello\n")
	press(m, "A")
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	press(m, "XX")
	if got := m.buf.Lines()[0]; got != "helXXlo" {
		t.Errorf("insert arrows: %q", got)
	}
	if m.eng.Mode() != vim.ModeInsert {
		t.Errorf("arrows left insert mode: %v", m.eng.Mode())
	}
}

func TestWordCount(t *testing.T) {
	m, _ := newModel(t, "one two\n\nthree four five\n")
	if got := m.wordCount(); got != 5 {
		t.Errorf("wordCount = %d, want 5", got)
	}
}

func TestMergeVisibility(t *testing.T) {
	m, _ := newModel(t, "alpha\nbeta\ngamma\n")
	press(m, ":w\rG") // cursor on gamma

	m.Update(extChangeMsg(filesync.Change{
		Lines: []string{"alpha", "inserted one", "inserted two", "beta", "gamma"},
	}))
	if m.msg != "co-writer: +2 -0 lines (g; to jump)" {
		t.Errorf("msg = %q", m.msg)
	}
	if !m.hlLines[1] || !m.hlLines[2] || m.hlLines[0] || m.hlLines[3] {
		t.Errorf("hlLines = %v, want lines 1,2", m.hlLines)
	}
	press(m, "g;")
	if m.cursor != (buffer.Pos{Line: 1, Col: 0}) {
		t.Errorf("g; landed at %v", m.cursor)
	}
	// the fade clears only when the generation matches
	m.Update(hlClearMsg(m.hlGen - 1))
	if len(m.hlLines) == 0 {
		t.Error("stale fade cleared the highlight")
	}
	m.Update(hlClearMsg(m.hlGen))
	if len(m.hlLines) != 0 {
		t.Error("fade did not clear the highlight")
	}
}

func TestJumpChangeWithoutMerge(t *testing.T) {
	m, _ := newModel(t, "text\n")
	press(m, "g;")
	if m.msg != "no co-writer changes yet" {
		t.Errorf("msg = %q", m.msg)
	}
}

func TestTextColumnCapAndCenter(t *testing.T) {
	m, _ := newModel(t, "words\n")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 10})
	if m.layout.Width != 80 {
		t.Errorf("layout width = %d, want 80", m.layout.Width)
	}
	if m.pad != 20 {
		t.Errorf("pad = %d, want 20", m.pad)
	}
	v := ansiRE.ReplaceAllString(m.View(), "")
	if !strings.HasPrefix(v, strings.Repeat(" ", 20)+"words") {
		t.Errorf("view not centered:\n%q", strings.SplitN(v, "\n", 2)[0])
	}
	// narrow terminals get the full width
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	if m.layout.Width != 40 || m.pad != 0 {
		t.Errorf("narrow: width %d pad %d", m.layout.Width, m.pad)
	}
}

func TestCrashSaveFlushesBuffer(t *testing.T) {
	m, path := newModel(t, "precious words\n")
	press(m, "A more\x1b")
	func() {
		defer func() { _ = recover() }() // crashSave re-panics by design
		defer m.crashSave()
		panic("boom")
	}()
	data, err := os.ReadFile(path + ".crash")
	if err != nil {
		t.Fatal("no crash file written")
	}
	if string(data) != "precious words more\n" {
		t.Errorf("crash file = %q", data)
	}
}

func TestCursorShapePerMode(t *testing.T) {
	// tests run without a TTY, so lipgloss strips styling unless forced
	lipgloss.SetColorProfile(termenv.ANSI)
	m, _ := newModel(t, "hello\n")
	if v := m.View(); !strings.Contains(v, "\x1b[7m") {
		t.Error("normal mode should draw a reverse-video block cursor")
	}
	press(m, "i")
	// lipgloss may emit the underline attribute as "4" or doubled "4;4"
	if v := m.View(); !strings.Contains(v, "\x1b[4m") && !strings.Contains(v, "\x1b[4;") {
		t.Error("insert mode should draw an underline cursor")
	}
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestViewRenders(t *testing.T) {
	m, _ := newModel(t, "hello world this is a long line that wraps\n")
	v := ansiRE.ReplaceAllString(m.View(), "")
	if !strings.Contains(v, "hello world") {
		t.Errorf("view missing text:\n%s", v)
	}
	if !strings.Contains(v, "NORMAL") {
		t.Errorf("view missing mode:\n%s", v)
	}
}
