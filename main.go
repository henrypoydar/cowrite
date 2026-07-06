// cowrite is a vim-flavored terminal markdown editor built for co-writing:
// run it in one pane, a terminal agent in another, and edit the same file
// together. See README.md.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/henrypoydar/cowrite/internal/app"
)

// version is stamped by goreleaser at release time.
var version = "dev"

const usage = `usage: cowrite [--with <command>] <file>

  --with <command>   also launch <command> in a tmux split beside the
                     editor ({file} expands to the file path)
  --version          print the version`

func main() {
	path, withCmd, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}

	if withCmd != "" {
		if err := splitWith(withCmd, path); err != nil {
			fmt.Fprintln(os.Stderr, "cowrite:", err)
			os.Exit(1)
		}
	}

	m, err := app.New(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cowrite:", err)
		os.Exit(1)
	}
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "cowrite:", err)
		os.Exit(1)
	}
}

// parseArgs accepts the file path and flags in any order.
func parseArgs(args []string) (path, with string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--version":
			fmt.Println("cowrite", version)
			os.Exit(0)
		case a == "--with":
			i++
			if i >= len(args) {
				return "", "", fmt.Errorf("cowrite: --with requires a command")
			}
			with = args[i]
		case strings.HasPrefix(a, "--with="):
			with = strings.TrimPrefix(a, "--with=")
			if with == "" {
				return "", "", fmt.Errorf("cowrite: --with requires a command")
			}
		case strings.HasPrefix(a, "-"):
			return "", "", fmt.Errorf("cowrite: unknown flag %s", a)
		case path != "":
			return "", "", fmt.Errorf("cowrite: expected one file, got %q and %q", path, a)
		default:
			path = a
		}
	}
	if path == "" {
		return "", "", fmt.Errorf("cowrite: no file given")
	}
	return path, with, nil
}

// splitWith opens the co-writer's command in a tmux split beside the
// editor, keeping focus here. cowrite stays agent-agnostic: the command is
// whatever the user says it is.
func splitWith(command, path string) error {
	if os.Getenv("TMUX") == "" {
		return fmt.Errorf("--with needs a tmux session (Ghostty/iTerm splits aren't scriptable); run cowrite inside tmux or open the split yourself")
	}
	command = strings.ReplaceAll(command, "{file}", path)
	out, err := exec.Command("tmux", "split-window", "-d", "-h", command).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux split failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
