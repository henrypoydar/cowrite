package render

import (
	"strings"
	"testing"

	"github.com/henrypoydar/cowrite/internal/buffer"
)

// deco renders the style of each rune as a letter for easy comparison:
// t=text, H=heading, m=marker, S=strong, E=emph, C=code, Q=quote.
func deco(t *testing.T, doc string, line int) string {
	t.Helper()
	styles := Decorate(buffer.New(doc))
	letters := map[Style]byte{
		SText: 't', SHeading: 'H', SMarker: 'm', SStrong: 'S',
		SEmph: 'E', SCode: 'C', SQuote: 'Q',
	}
	var b strings.Builder
	for _, s := range styles[line] {
		b.WriteByte(letters[s])
	}
	return b.String()
}

func TestDecorate(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		line int
		want string
	}{
		{"heading", "## Title\n", 0, "mmHHHHHH"},
		{"not a heading without space", "#tag\n", 0, "tttt"},
		{"blockquote", "> quoted\n", 0, "mQQQQQQQ"},
		{"bullet list", "- item\n", 0, "mmtttt"},
		{"ordered list", "12. item\n", 0, "mmmmtttt"},
		{"strong", "a **bb** c\n", 0, "ttmmSSmmtt"},
		{"emphasis", "a *b* c\n", 0, "ttmEmtt"},
		{"underscore emphasis", "_hi_\n", 0, "mEEm"},
		{"inline code", "x `y` z\n", 0, "ttmCmtt"},
		{"unclosed emphasis stays text", "3 * 4\n", 0, "ttttt"},
		{"emphasis inside list", "- *hi*\n", 0, "mmmEEm"},
		{"fence line", "```go\ncode here\n```\n", 0, "mmmmm"},
		{"fenced content", "```go\ncode here\n```\n", 1, "CCCCCCCCC"},
		{"after fence closes", "```\nx\n```\nplain\n", 3, "ttttt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := deco(t, c.doc, c.line); got != c.want {
				t.Errorf("got  %s\nwant %s", got, c.want)
			}
		})
	}
}
