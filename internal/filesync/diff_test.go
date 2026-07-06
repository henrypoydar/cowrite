package filesync

import (
	"slices"
	"testing"
)

func TestDiff(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want []Hunk
	}{
		{"equal", []string{"a", "b"}, []string{"a", "b"}, nil},
		{"insert", []string{"a", "c"}, []string{"a", "b", "c"},
			[]Hunk{{Start: 1, End: 1, Lines: []string{"b"}}}},
		{"delete", []string{"a", "b", "c"}, []string{"a", "c"},
			[]Hunk{{Start: 1, End: 2, Lines: []string{}}}},
		{"replace", []string{"a", "b", "c"}, []string{"a", "X", "c"},
			[]Hunk{{Start: 1, End: 2, Lines: []string{"X"}}}},
		{"append", []string{"a"}, []string{"a", "b"},
			[]Hunk{{Start: 1, End: 1, Lines: []string{"b"}}}},
		{"prepend", []string{"b"}, []string{"a", "b"},
			[]Hunk{{Start: 0, End: 0, Lines: []string{"a"}}}},
		{"everything", []string{"a"}, []string{"x", "y"},
			[]Hunk{{Start: 0, End: 1, Lines: []string{"x", "y"}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Diff(c.a, c.b)
			if len(got) != len(c.want) {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
			for i := range got {
				if got[i].Start != c.want[i].Start || got[i].End != c.want[i].End ||
					!slices.Equal(got[i].Lines, c.want[i].Lines) {
					t.Errorf("hunk %d = %+v, want %+v", i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestDiffRoundTrip(t *testing.T) {
	a := []string{"one", "two", "three", "four", "five"}
	b := []string{"zero", "one", "three", "3.5", "five", "six"}
	got := a
	// applying hunks back-to-front leaves earlier offsets valid
	hunks := Diff(a, b)
	for i := len(hunks) - 1; i >= 0; i-- {
		h := hunks[i]
		got = slices.Concat(got[:h.Start], h.Lines, got[h.End:])
	}
	if !slices.Equal(got, b) {
		t.Errorf("round trip = %v, want %v", got, b)
	}
}

func TestMerge3(t *testing.T) {
	base := []string{"title", "", "para one", "", "para two"}
	cases := []struct {
		name         string
		ours, theirs []string
		want         []string
	}{
		{"theirs only",
			base,
			[]string{"title", "", "para one", "", "para two", "", "para three"},
			[]string{"title", "", "para one", "", "para two", "", "para three"}},
		{"ours only",
			[]string{"TITLE", "", "para one", "", "para two"},
			base,
			[]string{"TITLE", "", "para one", "", "para two"}},
		{"disjoint edits combine",
			[]string{"TITLE", "", "para one", "", "para two"},
			[]string{"title", "", "para one", "", "para two", "para two b"},
			[]string{"TITLE", "", "para one", "", "para two", "para two b"}},
		{"overlap: theirs wins",
			[]string{"title", "", "para one OURS", "", "para two"},
			[]string{"title", "", "para one THEIRS", "", "para two"},
			[]string{"title", "", "para one THEIRS", "", "para two"}},
		{"same-point insertions keep both, theirs first",
			[]string{"title", "", "para one", "ours new", "", "para two"},
			[]string{"title", "", "para one", "theirs new", "", "para two"},
			[]string{"title", "", "para one", "theirs new", "ours new", "", "para two"}},
		{"no changes",
			base, base, base},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Merge3(base, c.ours, c.theirs)
			if !slices.Equal(got, c.want) {
				t.Errorf("got  %v\nwant %v", got, c.want)
			}
		})
	}
}

func TestMerge3EmptyResult(t *testing.T) {
	base := []string{"a"}
	got := Merge3(base, []string{"a"}, []string{""})
	if !slices.Equal(got, []string{""}) {
		t.Errorf("got %v", got)
	}
}

func TestAdjustLine(t *testing.T) {
	ours := []string{"a", "b", "c", "d"}
	merged := []string{"x", "y", "a", "b", "c", "d"} // 2 lines inserted above
	if got := AdjustLine(ours, merged, 2); got != 4 {
		t.Errorf("insert above: got %d, want 4", got)
	}

	merged = []string{"a", "d"} // b, c deleted
	if got := AdjustLine(ours, merged, 3); got != 1 {
		t.Errorf("delete above: got %d, want 1", got)
	}
	// cursor inside the rewritten region lands at its start
	if got := AdjustLine(ours, merged, 2); got != 1 {
		t.Errorf("inside rewrite: got %d, want 1", got)
	}

	merged = []string{"a", "b", "c", "d", "e"} // append below cursor
	if got := AdjustLine(ours, merged, 1); got != 1 {
		t.Errorf("append below: got %d, want 1", got)
	}
}
