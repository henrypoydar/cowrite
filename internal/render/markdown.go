package render

import "strings"

// Markdown decoration: a Style per rune, computed line by line. This is
// decoration, not preview — the source stays visible; structure is styled
// in place. The lexer is deliberately lightweight: headings, blockquotes,
// list markers, fenced and inline code, strong and emphasis, plus muted
// frontmatter and %% comments %%. No nesting.

type Style uint8

const (
	SText    Style = iota
	SHeading       // heading text after the #s
	SMarker        // structural punctuation: #, >, bullets, fences, delimiters
	SStrong        // **bold**
	SEmph          // *italic* or _italic_
	SCode          // `inline` and fenced block contents
	SQuote         // blockquote text
	SComment       // frontmatter and %% comments %% — metadata, not prose
	SLink          // a link's readable part: [label], a bare URL, or <autolink>
)

// Decorate computes a style for every rune of every line.
func Decorate(l Lines) [][]Style {
	out := make([][]Style, l.LineCount())
	fmEnd := frontmatterEnd(l)
	inFence := false
	inComment := false
	for i := range l.LineCount() {
		line := l.Line(i)
		st := make([]Style, len(line))
		out[i] = st

		if i <= fmEnd {
			fill(st, SComment)
			continue
		}
		if !inComment && isFence(line) {
			fill(st, SMarker)
			inFence = !inFence
			continue
		}
		if inFence {
			fill(st, SCode)
			continue
		}
		mask := commentMask(line, &inComment)
		decorateLine(line, st)
		for j, commented := range mask {
			if commented {
				st[j] = SComment
			}
		}
	}
	return out
}

// decorateLine styles one ordinary content line: heading, blockquote, or
// list marker + inline spans.
func decorateLine(line []rune, st []Style) {
	if n := headingLevel(line); n > 0 {
		for j := range n {
			st[j] = SMarker
		}
		for j := n; j < len(line); j++ {
			st[j] = SHeading
		}
		return
	}
	if len(line) > 0 && line[0] == '>' {
		st[0] = SMarker
		for j := 1; j < len(line); j++ {
			st[j] = SQuote
		}
		return
	}
	if n := listMarker(line); n > 0 {
		for j := range n {
			st[j] = SMarker
		}
	}
	inline(line, st)
}

// frontmatterEnd returns the last line of a YAML frontmatter block —
// opened by "---" as the document's first line and closed by another —
// or -1. An unclosed opener is not frontmatter (else the whole document
// would mute while the block is being typed).
func frontmatterEnd(l Lines) int {
	if l.LineCount() == 0 || !isFmDelim(l.Line(0)) {
		return -1
	}
	for i := 1; i < l.LineCount(); i++ {
		if isFmDelim(l.Line(i)) {
			return i
		}
	}
	return -1
}

func isFmDelim(line []rune) bool {
	s := string(line)
	return s == "---" || strings.TrimRight(s, " ") == "---"
}

// commentMask marks the runes of line inside %% comments %%, Obsidian
// style, carrying open comments across lines.
func commentMask(line []rune, open *bool) []bool {
	mask := make([]bool, len(line))
	i := 0
	for i < len(line) {
		delim := i+1 < len(line) && line[i] == '%' && line[i+1] == '%'
		if delim {
			mask[i], mask[i+1] = true, true
			*open = !*open
			i += 2
			continue
		}
		mask[i] = *open
		i++
	}
	return mask
}

func fill(st []Style, s Style) {
	for i := range st {
		st[i] = s
	}
}

func isFence(line []rune) bool {
	return len(line) >= 3 && line[0] == '`' && line[1] == '`' && line[2] == '`'
}

// headingLevel returns the number of leading #s when the line is a heading
// (1-6 #s followed by a space), else 0.
func headingLevel(line []rune) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n >= 1 && n <= 6 && n < len(line) && line[n] == ' ' {
		return n
	}
	return 0
}

// listMarker returns the rune length of a leading list marker (indent +
// bullet or ordinal + space), else 0.
func listMarker(line []rune) int {
	i := 0
	for i < len(line) && line[i] == ' ' {
		i++
	}
	if i+1 < len(line) && (line[i] == '-' || line[i] == '*' || line[i] == '+') && line[i+1] == ' ' {
		return i + 2
	}
	j := i
	for j < len(line) && line[j] >= '0' && line[j] <= '9' {
		j++
	}
	if j > i && j+1 < len(line) && line[j] == '.' && line[j+1] == ' ' {
		return j + 2
	}
	return 0
}

// inline styles `code`, **strong**, and *emphasis* spans over runes still
// marked SText.
func inline(line []rune, st []Style) {
	i := 0
	for i < len(line) {
		if st[i] != SText {
			i++
			continue
		}
		if j := linkSpan(line, st, i); j != -1 {
			i = j
			continue
		}
		if j := urlSpan(line, st, i); j != -1 {
			i = j
			continue
		}
		switch line[i] {
		case '`':
			if j := findPlain(line, st, "`", i+1); j != -1 {
				st[i], st[j] = SMarker, SMarker
				for k := i + 1; k < j; k++ {
					st[k] = SCode
				}
				i = j + 1
				continue
			}
		case '*', '_':
			d := string(line[i])
			if i+1 < len(line) && line[i+1] == line[i] {
				if j := findPlain(line, st, d+d, i+2); j != -1 {
					st[i], st[i+1], st[j], st[j+1] = SMarker, SMarker, SMarker, SMarker
					for k := i + 2; k < j; k++ {
						st[k] = SStrong
					}
					i = j + 2
					continue
				}
			} else if j := findPlain(line, st, d, i+1); j != -1 && j > i+1 {
				st[i], st[j] = SMarker, SMarker
				for k := i + 1; k < j; k++ {
					st[k] = SEmph
				}
				i = j + 1
				continue
			}
		}
		i++
	}
}

// linkSpan styles an inline [label](url) — or image ![alt](url) — beginning
// at i. The label reads as the link; the brackets, parens, URL, and any image
// bang recede as markers. Returns the index just past ')', or -1.
func linkSpan(line []rune, st []Style, i int) int {
	if line[i] != '[' {
		return -1
	}
	rbrack := findPlain(line, st, "]", i+1)
	if rbrack == -1 || rbrack+1 >= len(line) || line[rbrack+1] != '(' {
		return -1
	}
	rparen := findPlain(line, st, ")", rbrack+2)
	if rparen == -1 {
		return -1
	}
	if i > 0 && line[i-1] == '!' && st[i-1] == SText {
		st[i-1] = SMarker // image bang
	}
	st[i], st[rbrack] = SMarker, SMarker // [ ]
	for k := i + 1; k < rbrack; k++ {    // label
		st[k] = SLink
	}
	for k := rbrack + 1; k <= rparen; k++ { // (url)
		st[k] = SMarker
	}
	return rparen + 1
}

// urlSpan styles a bare or angle-bracketed URL beginning at i. A bare URL
// (http/https scheme at a word boundary) runs to whitespace, dropping any
// trailing sentence punctuation; an <angle> URL runs to its '>'. Returns the
// index just past the URL, or -1.
func urlSpan(line []rune, st []Style, i int) int {
	if line[i] == '<' {
		if !hasScheme(line, i+1) {
			return -1
		}
		gt := findPlain(line, st, ">", i+1)
		if gt == -1 {
			return -1
		}
		st[i], st[gt] = SMarker, SMarker
		for k := i + 1; k < gt; k++ {
			st[k] = SLink
		}
		return gt + 1
	}
	if !hasScheme(line, i) || (i > 0 && !isURLBoundary(line[i-1])) {
		return -1
	}
	j := i
	for j < len(line) && st[j] == SText && !isURLStop(line[j]) {
		j++
	}
	for j > i+1 && isTrailPunct(line[j-1]) {
		j--
	}
	for k := i; k < j; k++ {
		st[k] = SLink
	}
	return j
}

func hasScheme(line []rune, i int) bool {
	for _, s := range [...]string{"https://", "http://"} {
		r := []rune(s)
		if i+len(r) > len(line) {
			continue
		}
		ok := true
		for k := range r {
			if line[i+k] != r[k] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func isURLBoundary(r rune) bool {
	switch r {
	case ' ', '\t', '(', '<', '"', '\'':
		return true
	}
	return false
}

func isURLStop(r rune) bool {
	switch r {
	case ' ', '\t', '<', '>', ')', ']', '"', '\'', '`':
		return true
	}
	return false
}

func isTrailPunct(r rune) bool {
	switch r {
	case '.', ',', ';', ':', '!', '?':
		return true
	}
	return false
}

// findPlain locates delim starting at or after from, over runes still
// styled SText. Returns the index of the delimiter's first rune, or -1.
func findPlain(line []rune, st []Style, delim string, from int) int {
	d := []rune(delim)
	for i := from; i+len(d) <= len(line); i++ {
		match := true
		for k, r := range d {
			if line[i+k] != r || st[i+k] != SText {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
