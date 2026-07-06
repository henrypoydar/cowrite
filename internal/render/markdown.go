package render

// Markdown decoration: a Style per rune, computed line by line. This is
// decoration, not preview — the source stays visible; structure is styled
// in place. The lexer is deliberately lightweight: headings, blockquotes,
// list markers, fenced and inline code, strong and emphasis. No nesting.

type Style uint8

const (
	SText    Style = iota
	SHeading       // heading text after the #s
	SMarker        // structural punctuation: #, >, bullets, fences, delimiters
	SStrong        // **bold**
	SEmph          // *italic* or _italic_
	SCode          // `inline` and fenced block contents
	SQuote         // blockquote text
)

// Decorate computes a style for every rune of every line.
func Decorate(l Lines) [][]Style {
	out := make([][]Style, l.LineCount())
	inFence := false
	for i := range l.LineCount() {
		line := l.Line(i)
		st := make([]Style, len(line))
		out[i] = st

		if isFence(line) {
			fill(st, SMarker)
			inFence = !inFence
			continue
		}
		if inFence {
			fill(st, SCode)
			continue
		}
		if n := headingLevel(line); n > 0 {
			for j := range n {
				st[j] = SMarker
			}
			for j := n; j < len(line); j++ {
				st[j] = SHeading
			}
			continue
		}
		if len(line) > 0 && line[0] == '>' {
			st[0] = SMarker
			for j := 1; j < len(line); j++ {
				st[j] = SQuote
			}
			continue
		}
		if n := listMarker(line); n > 0 {
			for j := range n {
				st[j] = SMarker
			}
		}
		inline(line, st)
	}
	return out
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
