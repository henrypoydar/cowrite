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
		SEmph: 'E', SCode: 'C', SQuote: 'Q', SComment: 'x', SLink: 'L',
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
		// links
		{"inline link", "[go](http://x)\n", 0, "mLLmmmmmmmmmmm"},
		{"image link", "![alt](y)\n", 0, "mmLLLmmmm"},
		{"bare url", "see http://x.com\n", 0, "ttttLLLLLLLLLLLL"},
		{"bare url drops trailing dot", "go http://x.\n", 0, "tttLLLLLLLLt"},
		{"angle autolink", "<https://x>\n", 0, "mLLLLLLLLLm"},
		{"link label not emphasized", "[a_b](u)\n", 0, "mLLLmmmm"},
		{"link inside list", "- [a](b)\n", 0, "mmmLmmmm"},
		{"unclosed emphasis stays text", "3 * 4\n", 0, "ttttt"},
		{"emphasis inside list", "- *hi*\n", 0, "mmmEEm"},
		{"fence line", "```go\ncode here\n```\n", 0, "mmmmm"},
		{"fenced content", "```go\ncode here\n```\n", 1, "CCCCCCCCC"},
		{"after fence closes", "```\nx\n```\nplain\n", 3, "ttttt"},
		// frontmatter
		{"frontmatter delimiter", "---\ntitle: Post\n---\nbody\n", 0, "xxx"},
		{"frontmatter content", "---\ntitle: Post\n---\nbody\n", 1, "xxxxxxxxxxx"},
		{"after frontmatter", "---\ntitle: Post\n---\nbody\n", 3, "tttt"},
		{"unclosed frontmatter is not frontmatter", "---\ntitle: Post\n", 1, "ttttttttttt"},
		{"--- mid-document is not frontmatter", "body\n---\nmore\n", 1, "ttt"},
		// %% comments %%
		{"inline comment", "a %%hidden%% b\n", 0, "ttxxxxxxxxxxtt"},
		{"comment spans lines, opener", "pre %% note\nstill note\n%% post\n", 0, "ttttxxxxxxx"},
		{"comment spans lines, middle", "pre %% note\nstill note\n%% post\n", 1, "xxxxxxxxxx"},
		{"comment spans lines, closer", "pre %% note\nstill note\n%% post\n", 2, "xxttttt"},
		{"comment overrides heading", "%%\n# hidden\n%%\n", 1, "xxxxxxxx"},
		{"percent inside fence is code", "```\n%% x\n```\nafter\n", 3, "ttttt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := deco(t, c.doc, c.line); got != c.want {
				t.Errorf("got  %s\nwant %s", got, c.want)
			}
		})
	}
}
