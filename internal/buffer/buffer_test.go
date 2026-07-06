package buffer

import "testing"

func TestNewAndContents(t *testing.T) {
	cases := []struct {
		in    string
		lines int
		out   string
	}{
		{"", 1, ""},
		{"hello\n", 1, "hello"},
		{"a\nb\nc\n", 3, "a\nb\nc"},
		{"no trailing", 1, "no trailing"},
		{"gap\n\nline\n", 3, "gap\n\nline"},
	}
	for _, c := range cases {
		b := New(c.in)
		if b.LineCount() != c.lines {
			t.Errorf("New(%q): %d lines, want %d", c.in, b.LineCount(), c.lines)
		}
		if got := b.Contents(); got != c.out {
			t.Errorf("New(%q).Contents() = %q, want %q", c.in, got, c.out)
		}
	}
}

func TestReplace(t *testing.T) {
	cases := []struct {
		name       string
		start, end Pos
		text       string
		want       string
		wantEnd    Pos
	}{
		{"insert", Pos{0, 5}, Pos{0, 5}, " brave", "hello brave world\nsecond line", Pos{0, 11}},
		{"delete range", Pos{0, 5}, Pos{0, 11}, "", "hello\nsecond line", Pos{0, 5}},
		{"replace across lines", Pos{0, 5}, Pos{1, 6}, "", "hello line", Pos{0, 5}},
		{"insert newline", Pos{0, 5}, Pos{0, 5}, "\n", "hello\n world\nsecond line", Pos{1, 0}},
		{"multiline insert", Pos{1, 0}, Pos{1, 0}, "x\ny\n", "hello world\nx\ny\nsecond line", Pos{3, 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := New("hello world\nsecond line\n")
			end := b.Replace(c.start, c.end, c.text)
			if got := b.Contents(); got != c.want {
				t.Errorf("contents = %q, want %q", got, c.want)
			}
			if end != c.wantEnd {
				t.Errorf("end = %v, want %v", end, c.wantEnd)
			}
			if !b.Dirty() {
				t.Error("buffer not marked dirty")
			}
		})
	}
}

func TestUndoRedo(t *testing.T) {
	b := New("one\ntwo\n")
	b.Replace(Pos{0, 3}, Pos{0, 3}, " more")
	b.Replace(Pos{1, 0}, Pos{1, 3}, "TWO")
	if b.Contents() != "one more\nTWO" {
		t.Fatalf("setup: %q", b.Contents())
	}

	if _, ok := b.Undo(); !ok {
		t.Fatal("undo failed")
	}
	if b.Contents() != "one more\ntwo" {
		t.Errorf("after undo: %q", b.Contents())
	}
	b.Undo()
	if b.Contents() != "one\ntwo" {
		t.Errorf("after second undo: %q", b.Contents())
	}
	if _, ok := b.Undo(); ok {
		t.Error("undo past history should fail")
	}

	b.Redo()
	b.Redo()
	if b.Contents() != "one more\nTWO" {
		t.Errorf("after redo: %q", b.Contents())
	}
	if _, ok := b.Redo(); ok {
		t.Error("redo past history should fail")
	}
}

func TestUndoGroup(t *testing.T) {
	b := New("start\n")
	b.BeginGroup(Pos{0, 5})
	b.Replace(Pos{0, 5}, Pos{0, 5}, " a")
	b.Replace(Pos{0, 7}, Pos{0, 7}, "b")
	b.Replace(Pos{0, 8}, Pos{0, 8}, "c")
	b.EndGroup()

	cur, ok := b.Undo()
	if !ok {
		t.Fatal("undo failed")
	}
	if b.Contents() != "start" {
		t.Errorf("grouped undo should revert all: %q", b.Contents())
	}
	if cur != (Pos{0, 5}) {
		t.Errorf("cursor = %v, want {0 5}", cur)
	}

	b.Redo()
	if b.Contents() != "start abc" {
		t.Errorf("grouped redo: %q", b.Contents())
	}
}

func TestSetLines(t *testing.T) {
	b := New("old one\nold two\n")
	b.SetLines([]string{"new one", "new two", "new three"})
	if b.Contents() != "new one\nnew two\nnew three" {
		t.Errorf("SetLines: %q", b.Contents())
	}
	b.Undo()
	if b.Contents() != "old one\nold two" {
		t.Errorf("SetLines undo: %q", b.Contents())
	}
}

func TestRedoClearedByNewChange(t *testing.T) {
	b := New("abc\n")
	b.Replace(Pos{0, 3}, Pos{0, 3}, "d")
	b.Undo()
	b.Replace(Pos{0, 0}, Pos{0, 0}, "z")
	if _, ok := b.Redo(); ok {
		t.Error("redo should be cleared by a new change")
	}
}
