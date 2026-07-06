package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/henrypoydar/cowrite/internal/buffer"
	"github.com/henrypoydar/cowrite/internal/render"
	"github.com/henrypoydar/cowrite/internal/vim"
)

var (
	// No background fills: accent colors on the terminal's own background,
	// so the bar reads like chrome, not a stripe. ANSI palette throughout.
	faintStyle = lipgloss.NewStyle().Faint(true)
	fileStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	insertMode = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	visualMode = lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)
	promptMode = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	coMsgStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)
	tildeStyle = lipgloss.NewStyle().Faint(true)
	mergeBg    = lipgloss.AdaptiveColor{Light: "254", Dark: "236"}

	mdStyles = map[render.Style]lipgloss.Style{
		render.SText:    lipgloss.NewStyle(),
		render.SHeading: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")),
		render.SMarker:  lipgloss.NewStyle().Faint(true),
		render.SStrong:  lipgloss.NewStyle().Bold(true),
		render.SEmph:    lipgloss.NewStyle().Italic(true),
		render.SCode:    lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		render.SQuote:   lipgloss.NewStyle().Italic(true).Faint(true),
	}
)

func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	deco := render.Decorate(m.buf)
	cursorRow, cursorCol := m.layout.PosToRow(m.cursor)

	pad := strings.Repeat(" ", m.pad)
	var b strings.Builder
	for i := range m.viewRows() {
		r := m.top + i
		if r >= len(m.layout.Rows) {
			b.WriteString(pad)
			b.WriteString(tildeStyle.Render("~"))
			b.WriteByte('\n')
			continue
		}
		row := m.layout.Rows[r]
		text := m.buf.Line(row.Line)[row.Start:row.End]
		styles := deco[row.Line][row.Start:row.End]
		selFrom, selTo := m.selectionInRow(row)
		cc := -1
		if r == cursorRow {
			cc = cursorCol
		}
		b.WriteString(pad)
		b.WriteString(renderRow(text, styles, selFrom, selTo, cc,
			m.eng.Mode() == vim.ModeInsert, m.hlLines[row.Line]))
		b.WriteByte('\n')
	}
	b.WriteString(faintStyle.Render(strings.Repeat("─", m.width)))
	b.WriteByte('\n')
	b.WriteString(m.statusLine())
	b.WriteByte('\n')
	b.WriteString(m.messageLine())
	return b.String()
}

// renderRow paints one display row, grouping runs of runes that share the
// same (style, selected) attributes. The cursor is a reverse-video block
// in normal mode — inverting whatever it lands on so it stays visible
// inside a selection — and an underline in insert mode, the terminal
// convention for "text goes here".
func renderRow(text []rune, styles []render.Style, selFrom, selTo, cursorCol int, insertCursor, merged bool) string {
	base := func(i int) lipgloss.Style {
		s := mdStyles[styles[i]]
		if merged {
			s = s.Background(mergeBg)
		}
		return s
	}
	var b strings.Builder
	flush := func(from, to int) {
		for from < to {
			run := from + 1
			for run < to && styles[run] == styles[from] {
				run++
			}
			b.WriteString(base(from).Render(string(text[from:run])))
			from = run
		}
	}
	styled := func(i int) lipgloss.Style {
		s := base(i)
		if i == cursorCol && insertCursor {
			return s.Underline(true)
		}
		sel := i >= selFrom && i < selTo
		if sel != (i == cursorCol) { // selection or cursor, not both
			s = s.Reverse(true)
		}
		return s
	}

	i := 0
	for i < len(text) {
		if (i >= selFrom && i < selTo) || i == cursorCol {
			b.WriteString(styled(i).Render(string(text[i])))
			i++
			continue
		}
		to := len(text)
		if i < selFrom {
			to = min(to, selFrom)
		}
		if i < cursorCol {
			to = min(to, cursorCol)
		}
		flush(i, to)
		i = to
	}
	if cursorCol >= len(text) && cursorCol >= 0 {
		eol := lipgloss.NewStyle().Reverse(true)
		if insertCursor {
			eol = lipgloss.NewStyle().Underline(true)
		}
		b.WriteString(eol.Render(" "))
	}
	return b.String()
}

// selectionInRow returns the selected rune span [from,to) within the row,
// or (-1,-1) when nothing in the row is selected.
func (m *Model) selectionInRow(row render.Row) (int, int) {
	start, end, ok := m.selectionSpan()
	if !ok {
		return -1, -1
	}
	if row.Line < start.Line || row.Line > end.Line {
		return -1, -1
	}
	from, to := 0, row.End-row.Start
	if !m.visual.linewise {
		if row.Line == start.Line {
			from = max(0, start.Col-row.Start)
		}
		if row.Line == end.Line {
			to = min(to, end.Col+1-row.Start)
		}
	}
	if from >= to && !(m.visual.linewise && row.Start == row.End) {
		return -1, -1
	}
	return from, to
}

// selectionSpan orders the visual anchor and cursor. Both ends inclusive.
func (m *Model) selectionSpan() (buffer.Pos, buffer.Pos, bool) {
	if !m.visual.active {
		return buffer.Pos{}, buffer.Pos{}, false
	}
	a, c := m.visual.anchor, m.cursor
	if c.Before(a) {
		a, c = c, a
	}
	return a, c, true
}

func (m *Model) statusLine() string {
	mode := m.eng.Mode().String()
	if m.visual.active && m.visual.linewise {
		mode = "V-LINE"
	}
	var modeStyle lipgloss.Style
	switch m.eng.Mode() {
	case vim.ModeInsert:
		modeStyle = insertMode
	case vim.ModeVisual, vim.ModeVisualLine:
		modeStyle = visualMode
	case vim.ModeCommand, vim.ModeSearch:
		modeStyle = promptMode
	default:
		modeStyle = faintStyle
	}

	dirty := ""
	if m.buf.Dirty() {
		dirty = faintStyle.Render(" [+]")
	}
	left := " " + modeStyle.Render(mode) + "  " + fileStyle.Render(filepath.Base(m.path)) + dirty
	right := faintStyle.Render(fmt.Sprintf("%dw  %d:%d ", m.wordCount(), m.cursor.Line+1, m.cursor.Col+1))
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) wordCount() int {
	n := 0
	for i := range m.buf.LineCount() {
		inWord := false
		for _, r := range m.buf.Line(i) {
			if r == ' ' || r == '\t' {
				inWord = false
			} else if !inWord {
				inWord = true
				n++
			}
		}
	}
	return n
}

func (m *Model) messageLine() string {
	switch m.eng.Mode() {
	case vim.ModeCommand:
		return ":" + m.eng.Cmdline()
	case vim.ModeSearch:
		return "/" + m.eng.Cmdline()
	}
	if m.msgCo {
		return coMsgStyle.Render(m.msg)
	}
	return m.msg
}
