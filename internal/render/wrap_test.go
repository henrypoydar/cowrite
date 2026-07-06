package render

import (
	"testing"

	"github.com/henrypoydar/cowrite/internal/buffer"
)

func TestWrap(t *testing.T) {
	b := buffer.New("one two three four\n\nshort\n")
	ly := Wrap(b, 10)
	want := []Row{
		{Line: 0, Start: 0, End: 8},  // "one two "
		{Line: 0, Start: 8, End: 18}, // "three four" — exactly width, fits
		{Line: 1, Start: 0, End: 0},  // empty line
		{Line: 2, Start: 0, End: 5},  // "short"
	}
	if len(ly.Rows) != len(want) {
		t.Fatalf("rows = %+v, want %d rows", ly.Rows, len(want))
	}
	for i, w := range want {
		if ly.Rows[i] != w {
			t.Errorf("row %d = %+v, want %+v", i, ly.Rows[i], w)
		}
	}
}

func TestWrapHardBreak(t *testing.T) {
	b := buffer.New("abcdefghijkl\n")
	ly := Wrap(b, 5)
	want := []Row{{Line: 0, Start: 0, End: 5}, {Line: 0, Start: 5, End: 10}, {Line: 0, Start: 10, End: 12}}
	if len(ly.Rows) != 3 {
		t.Fatalf("rows = %+v", ly.Rows)
	}
	for i, w := range want {
		if ly.Rows[i] != w {
			t.Errorf("row %d = %+v, want %+v", i, ly.Rows[i], w)
		}
	}
}

func TestPosRowRoundTrip(t *testing.T) {
	b := buffer.New("one two three four\nnext\n")
	ly := Wrap(b, 10)

	r, c := ly.PosToRow(buffer.Pos{Line: 0, Col: 9})
	if r != 1 || c != 1 {
		t.Errorf("PosToRow(0,9) = %d,%d want 1,1", r, c)
	}
	// end-of-line cursor sits one past the last rune of the final row
	r, c = ly.PosToRow(buffer.Pos{Line: 0, Col: 18})
	if r != 1 || c != 10 {
		t.Errorf("PosToRow(0,18) = %d,%d want 1,10", r, c)
	}

	// moving down a display row keeps the goal column
	p := ly.RowToPos(1, 3)
	if p != (buffer.Pos{Line: 0, Col: 11}) {
		t.Errorf("RowToPos(1,3) = %v", p)
	}
	// column clamps into short rows (and row index clamps into range)
	p = ly.RowToPos(3, 9)
	if p != (buffer.Pos{Line: 1, Col: 4}) {
		t.Errorf("RowToPos(3,9) = %v", p)
	}
	// non-final rows clamp before the wrap point
	p = ly.RowToPos(0, 20)
	if p != (buffer.Pos{Line: 0, Col: 7}) {
		t.Errorf("RowToPos(0,20) = %v", p)
	}
}
