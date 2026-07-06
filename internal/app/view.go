package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/henrypoydar/cowrite/internal/vim"
)

var (
	cursorStyle = lipgloss.NewStyle().Reverse(true)
	statusStyle = lipgloss.NewStyle().Reverse(true)
	tildeStyle  = lipgloss.NewStyle().Faint(true)
)

func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	cursorRow, cursorCol := m.layout.PosToRow(m.cursor)

	var b strings.Builder
	for i := range m.viewRows() {
		r := m.top + i
		if r >= len(m.layout.Rows) {
			b.WriteString(tildeStyle.Render("~"))
			b.WriteByte('\n')
			continue
		}
		row := m.layout.Rows[r]
		text := m.buf.Line(row.Line)[row.Start:row.End]
		if r == cursorRow {
			b.WriteString(withCursor(text, cursorCol))
		} else {
			b.WriteString(string(text))
		}
		b.WriteByte('\n')
	}
	b.WriteString(m.statusLine())
	b.WriteByte('\n')
	b.WriteString(m.messageLine())
	return b.String()
}

func withCursor(text []rune, col int) string {
	if col >= len(text) {
		return string(text) + cursorStyle.Render(" ")
	}
	return string(text[:col]) + cursorStyle.Render(string(text[col])) + string(text[col+1:])
}

func (m *Model) statusLine() string {
	name := filepath.Base(m.path)
	if m.buf.Dirty() {
		name += " [+]"
	}
	left := fmt.Sprintf(" %s  %s", m.eng.Mode(), name)
	right := fmt.Sprintf("%d:%d ", m.cursor.Line+1, m.cursor.Col+1)
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return statusStyle.Render(left + strings.Repeat(" ", gap) + right)
}

func (m *Model) messageLine() string {
	if m.eng.Mode() == vim.ModeCommand {
		return ":" + m.eng.Cmdline()
	}
	return m.msg
}
