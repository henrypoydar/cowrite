package vim

import (
	"testing"

	"github.com/henrypoydar/cowrite/internal/buffer"
)

// keys turns a string into engine input; \x1b, \r, \b map to specials.
func keys(s string) []Key {
	var out []Key
	for _, r := range s {
		switch r {
		case '\x1b':
			out = append(out, Key{Special: KeyEsc})
		case '\r':
			out = append(out, Key{Special: KeyEnter})
		case '\b':
			out = append(out, Key{Special: KeyBackspace})
		default:
			out = append(out, Key{Rune: r})
		}
	}
	return out
}

func feed(e *Engine, s string) []Cmd {
	var out []Cmd
	for _, k := range keys(s) {
		out = append(out, e.Feed(k)...)
	}
	return out
}

func TestEngineSequences(t *testing.T) {
	cases := []struct {
		in   string
		want []Cmd
	}{
		{"j", []Cmd{{Kind: CmdMove, Motion: Motion{Kind: MotionDown, Count: 1}}}},
		{"3w", []Cmd{{Kind: CmdMove, Motion: Motion{Kind: MotionWordForward, Count: 3}}}},
		{"10j", []Cmd{{Kind: CmdMove, Motion: Motion{Kind: MotionDown, Count: 10}}}},
		{"dw", []Cmd{{Kind: CmdDelete, Motion: Motion{Kind: MotionWordForward, Count: 1}}}},
		{"d2w", []Cmd{{Kind: CmdDelete, Motion: Motion{Kind: MotionWordForward, Count: 2}}}},
		{"2d3w", []Cmd{{Kind: CmdDelete, Motion: Motion{Kind: MotionWordForward, Count: 6}}}},
		{"dd", []Cmd{{Kind: CmdDelete, Motion: Motion{Kind: MotionLine, Count: 1}}}},
		{"3dd", []Cmd{{Kind: CmdDelete, Motion: Motion{Kind: MotionLine, Count: 3}}}},
		// cw acts like ce — the vim quirk: change stops at word end
		{"cw", []Cmd{{Kind: CmdChange, Motion: Motion{Kind: MotionWordEnd, Count: 1}}}},
		{"yy", []Cmd{{Kind: CmdYank, Motion: Motion{Kind: MotionLine, Count: 1}}}},
		{"x", []Cmd{{Kind: CmdDelete, Motion: Motion{Kind: MotionRight, Count: 1}}}},
		{"3x", []Cmd{{Kind: CmdDelete, Motion: Motion{Kind: MotionRight, Count: 3}}}},
		{"gg", []Cmd{{Kind: CmdMove, Motion: Motion{Kind: MotionFileStart}}}},
		{"G", []Cmd{{Kind: CmdMove, Motion: Motion{Kind: MotionFileEnd}}}},
		{"5G", []Cmd{{Kind: CmdMove, Motion: Motion{Kind: MotionFileEnd, Count: 5}}}},
		{"fx", []Cmd{{Kind: CmdMove, Motion: Motion{Kind: MotionFind, Count: 1, Char: 'x'}}}},
		{"dta", []Cmd{{Kind: CmdDelete, Motion: Motion{Kind: MotionTill, Count: 1, Char: 'a'}}}},
		{"u", []Cmd{{Kind: CmdUndo}}},
		{"p", []Cmd{{Kind: CmdPaste}}},
		{"P", []Cmd{{Kind: CmdPaste, Before: true}}},
		{"0", []Cmd{{Kind: CmdMove, Motion: Motion{Kind: MotionLineStart}}}},
		{"$", []Cmd{{Kind: CmdMove, Motion: Motion{Kind: MotionLineEnd, Count: 1}}}},
		// count containing a 0 digit: 10x
		{"10x", []Cmd{{Kind: CmdDelete, Motion: Motion{Kind: MotionRight, Count: 10}}}},
		// escape cancels a pending operator
		{"d\x1bw", []Cmd{{Kind: CmdMove, Motion: Motion{Kind: MotionWordForward, Count: 1}}}},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := feed(New(), c.in)
			if len(got) != len(c.want) {
				t.Fatalf("got %d cmds %+v, want %d", len(got), got, len(c.want))
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("cmd %d = %+v, want %+v", i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestEngineInsertMode(t *testing.T) {
	e := New()
	got := feed(e, "ihi\r!\x1b")
	want := []Cmd{
		{Kind: CmdEnterInsert, At: AtCursor},
		{Kind: CmdInsertText, Text: "h"},
		{Kind: CmdInsertText, Text: "i"},
		{Kind: CmdNewline},
		{Kind: CmdInsertText, Text: "!"},
		{Kind: CmdExitInsert},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("cmd %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if e.Mode() != ModeNormal {
		t.Errorf("mode = %v, want normal", e.Mode())
	}
}

func TestEngineInsertVariants(t *testing.T) {
	for in, at := range map[string]InsertAt{
		"i": AtCursor, "a": AtAfter, "I": AtLineStart,
		"A": AtLineEnd, "o": AtLineBelow, "O": AtLineAbove,
	} {
		e := New()
		got := feed(e, in)
		if len(got) != 1 || got[0].Kind != CmdEnterInsert || got[0].At != at {
			t.Errorf("%q: got %+v, want EnterInsert at %v", in, got, at)
		}
		if e.Mode() != ModeInsert {
			t.Errorf("%q: mode = %v, want insert", in, e.Mode())
		}
	}
}

func TestEngineCommandLine(t *testing.T) {
	e := New()
	got := feed(e, ":wq\r")
	if len(got) != 1 || got[0].Kind != CmdEx || got[0].Text != "wq" {
		t.Fatalf("got %+v, want Ex(wq)", got)
	}
	if e.Mode() != ModeNormal {
		t.Errorf("mode after ex = %v", e.Mode())
	}

	// esc cancels
	e = New()
	if got := feed(e, ":q\x1b"); len(got) != 0 {
		t.Errorf("cancelled cmdline produced %+v", got)
	}
	// backspace past empty cancels
	e = New()
	feed(e, ":w\b\b")
	if e.Mode() != ModeNormal {
		t.Errorf("mode = %v, want normal after backspacing out", e.Mode())
	}
}

func TestChangeEntersInsert(t *testing.T) {
	e := New()
	got := feed(e, "cw")
	if len(got) != 1 || got[0].Kind != CmdChange {
		t.Fatalf("got %+v", got)
	}
	if e.Mode() != ModeInsert {
		t.Errorf("cw should leave engine in insert, got %v", e.Mode())
	}
}

func lines(ss ...string) Lines { return buffer.New(joinLines(ss)) }

func joinLines(ss []string) string {
	out := ""
	for _, s := range ss {
		out += s + "\n"
	}
	return out
}

func TestResolveWordMotions(t *testing.T) {
	l := lines("one two, three", "", "  four")
	cases := []struct {
		name string
		m    Motion
		cur  buffer.Pos
		want buffer.Pos
	}{
		{"w to next word", Motion{Kind: MotionWordForward}, buffer.Pos{Line: 0, Col: 0}, buffer.Pos{Line: 0, Col: 4}},
		{"w stops at punct", Motion{Kind: MotionWordForward}, buffer.Pos{Line: 0, Col: 4}, buffer.Pos{Line: 0, Col: 7}},
		{"w from punct", Motion{Kind: MotionWordForward}, buffer.Pos{Line: 0, Col: 7}, buffer.Pos{Line: 0, Col: 9}},
		{"w onto empty line", Motion{Kind: MotionWordForward}, buffer.Pos{Line: 0, Col: 9}, buffer.Pos{Line: 1, Col: 0}},
		{"w across empty line", Motion{Kind: MotionWordForward}, buffer.Pos{Line: 1, Col: 0}, buffer.Pos{Line: 2, Col: 2}},
		{"2w", Motion{Kind: MotionWordForward, Count: 2}, buffer.Pos{Line: 0, Col: 0}, buffer.Pos{Line: 0, Col: 7}},
		{"b to word start", Motion{Kind: MotionWordBack}, buffer.Pos{Line: 0, Col: 6}, buffer.Pos{Line: 0, Col: 4}},
		{"b over punct", Motion{Kind: MotionWordBack}, buffer.Pos{Line: 0, Col: 9}, buffer.Pos{Line: 0, Col: 7}},
		{"b across lines", Motion{Kind: MotionWordBack}, buffer.Pos{Line: 2, Col: 2}, buffer.Pos{Line: 1, Col: 0}},
		{"e to word end", Motion{Kind: MotionWordEnd}, buffer.Pos{Line: 0, Col: 0}, buffer.Pos{Line: 0, Col: 2}},
		{"e from word end", Motion{Kind: MotionWordEnd}, buffer.Pos{Line: 0, Col: 2}, buffer.Pos{Line: 0, Col: 6}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Resolve(c.m, l, c.cur)
			if got.Pos != c.want {
				t.Errorf("got %v, want %v", got.Pos, c.want)
			}
		})
	}
}

func TestResolveFindAndLines(t *testing.T) {
	l := lines("abcabc", "second", "third")

	got := Resolve(Motion{Kind: MotionFind, Char: 'c'}, l, buffer.Pos{})
	if got.Pos != (buffer.Pos{Line: 0, Col: 2}) || !got.Inclusive {
		t.Errorf("fc: %+v", got)
	}
	got = Resolve(Motion{Kind: MotionFind, Char: 'c', Count: 2}, l, buffer.Pos{})
	if got.Pos != (buffer.Pos{Line: 0, Col: 5}) {
		t.Errorf("2fc: %+v", got)
	}
	got = Resolve(Motion{Kind: MotionTill, Char: 'c'}, l, buffer.Pos{})
	if got.Pos != (buffer.Pos{Line: 0, Col: 1}) {
		t.Errorf("tc: %+v", got)
	}
	got = Resolve(Motion{Kind: MotionFind, Char: 'z'}, l, buffer.Pos{Line: 0, Col: 3})
	if got.Pos != (buffer.Pos{Line: 0, Col: 3}) {
		t.Errorf("fz miss should not move: %+v", got)
	}

	got = Resolve(Motion{Kind: MotionFileEnd}, l, buffer.Pos{})
	if got.Pos.Line != 2 || !got.Linewise {
		t.Errorf("G: %+v", got)
	}
	got = Resolve(Motion{Kind: MotionFileEnd, Count: 2}, l, buffer.Pos{})
	if got.Pos.Line != 1 {
		t.Errorf("2G: %+v", got)
	}
	got = Resolve(Motion{Kind: MotionLine, Count: 2}, l, buffer.Pos{Line: 1})
	if got.Pos.Line != 2 || !got.Linewise {
		t.Errorf("2dd target: %+v", got)
	}
	got = Resolve(Motion{Kind: MotionLineEnd, Count: 1}, l, buffer.Pos{Line: 1, Col: 2})
	if got.Pos != (buffer.Pos{Line: 1, Col: 6}) {
		t.Errorf("$: %+v", got)
	}
}
