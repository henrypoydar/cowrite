package filesync

import (
	"slices"
	"sort"
)

// Hunk replaces base lines [Start,End) with Lines. Start==End is a pure
// insertion before line Start.
type Hunk struct {
	Start, End int
	Lines      []string
}

// Diff computes line-level hunks turning a into b, via LCS. Documents are
// prose-sized; quadratic DP is fine, with a wholesale-replace fallback as a
// backstop for pathological inputs.
func Diff(a, b []string) []Hunk {
	n, m := len(a), len(b)
	if n*m > 25_000_000 {
		if slices.Equal(a, b) {
			return nil
		}
		return []Hunk{{Start: 0, End: n, Lines: slices.Clone(b)}}
	}

	// dp[i][j] = LCS length of a[i:] and b[j:]
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}

	var hunks []Hunk
	i, j := 0, 0
	si, sj := 0, 0
	inDiff := false
	flush := func(ei, ej int) {
		if inDiff {
			hunks = append(hunks, Hunk{Start: si, End: ei, Lines: slices.Clone(b[sj:ej])})
			inDiff = false
		}
	}
	for i < n && j < m {
		if a[i] == b[j] {
			flush(i, j)
			i++
			j++
			continue
		}
		if !inDiff {
			si, sj = i, j
			inDiff = true
		}
		if dp[i+1][j] >= dp[i][j+1] {
			i++
		} else {
			j++
		}
	}
	if (i < n || j < m) && !inDiff {
		si, sj = i, j
		inDiff = true
	}
	flush(n, m)
	return hunks
}

// Merge3 merges two divergent edits of base. Regions changed by only one
// side apply cleanly. Where both sides changed the same region, theirs
// wins — on disk, the other writer wrote last. Pure insertions at the same
// point keep both, theirs first.
func Merge3(base, ours, theirs []string) []string {
	type side struct {
		h      Hunk
		theirs bool
	}
	var all []side
	for _, h := range Diff(base, ours) {
		all = append(all, side{h: h})
	}
	for _, h := range Diff(base, theirs) {
		all = append(all, side{h: h, theirs: true})
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].h.Start != all[j].h.Start {
			return all[i].h.Start < all[j].h.Start
		}
		return all[i].theirs && !all[j].theirs
	})

	out := make([]string, 0, max(len(ours), len(theirs)))
	pos := 0
	for k := 0; k < len(all); {
		// gather a cluster of hunks whose base ranges overlap
		cluster := []side{all[k]}
		start, end := all[k].h.Start, all[k].h.End
		k++
		for k < len(all) && all[k].h.Start < end {
			end = max(end, all[k].h.End)
			cluster = append(cluster, all[k])
			k++
		}

		hasOurs, hasTheirs := false, false
		for _, s := range cluster {
			if s.theirs {
				hasTheirs = true
			} else {
				hasOurs = true
			}
		}
		var apply []Hunk
		for _, s := range cluster {
			if !(hasOurs && hasTheirs) || s.theirs { // conflict: theirs only
				apply = append(apply, s.h)
			}
		}

		out = append(out, base[pos:start]...)
		p := start
		for _, h := range apply {
			out = append(out, base[p:h.Start]...)
			out = append(out, h.Lines...)
			p = h.End
		}
		out = append(out, base[p:end]...)
		pos = end
	}
	out = append(out, base[pos:]...)
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}

// AdjustLine maps a line index in the pre-merge buffer to the merged
// buffer, so the cursor stays on its text as agent hunks land around it.
// A cursor inside a rewritten region lands at the region's start.
func AdjustLine(ours, merged []string, line int) int {
	delta := 0
	for _, h := range Diff(ours, merged) {
		if h.End <= line {
			delta += len(h.Lines) - (h.End - h.Start)
		} else if h.Start <= line {
			return h.Start + delta
		} else {
			break
		}
	}
	return line + delta
}
