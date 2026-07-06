// cowrite is a vim-flavored terminal markdown editor built for co-writing:
// run it in one pane, a terminal agent in another, and edit the same file
// together. See README.md.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/henrypoydar/cowrite/internal/app"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: cowrite <file>")
		os.Exit(2)
	}
	m, err := app.New(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "cowrite:", err)
		os.Exit(1)
	}
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "cowrite:", err)
		os.Exit(1)
	}
}
