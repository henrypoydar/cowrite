package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/henrypoydar/cowrite/internal/filesync"
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
	m, path := newModel(t, "")
	press(m, "idraft\x1b")

	// stale tick (superseded generation) must not save
	m.Update(saveTickMsg(m.editGen - 1))
	if _, err := os.Stat(path); err == nil {
		t.Fatal("stale tick saved the file")
	}
	// current tick saves
	m.Update(saveTickMsg(m.editGen))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "draft\n" {
		t.Errorf("file = %q", data)
	}
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

func TestViewRenders(t *testing.T) {
	m, _ := newModel(t, "hello world this is a long line that wraps\n")
	v := m.View()
	if !strings.Contains(v, "hello world") {
		t.Errorf("view missing text:\n%s", v)
	}
	if !strings.Contains(v, "NORMAL") {
		t.Errorf("view missing mode:\n%s", v)
	}
}
