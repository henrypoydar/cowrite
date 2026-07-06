# cowrite

A vim-flavored markdown editor for your terminal, built for writing *with* an AI agent instead of waiting on one.

Run cowrite in one split pane and Claude Code (or Codex, or any terminal agent) in the other. Point them both at the same file. As the agent writes, its changes appear in your buffer in real time; as you type, your words are saved to disk so the agent always sees your latest thinking. You're both editing the same document, together.

There's no integration to configure and no API to wire up. cowrite never talks to an agent directly — the file on disk is the shared workspace, which means it works with any agent that can edit files, today and whatever ships next year.

## Why

Terminal agents are already good co-writers. The workflow around them is not. Today, co-writing a document means a clumsy loop: write, save, switch panes, prompt the agent, switch back, reload the buffer, squint at what changed. Every handoff has friction, and the friction discourages exactly the kind of rapid back-and-forth that makes co-writing good.

cowrite removes the loop. You write a passage, ask the agent to take a turn, and watch the draft grow in your own buffer — then jump back in without touching a save or reload command.

## How it works

cowrite is deliberately not much more than a focused markdown editor. The co-writing behavior comes from three things:

1. **Continuous save.** Your edits are written to disk automatically, moments after you stop typing. The file always reflects what's in your buffer, so an agent reading it never sees a stale draft.
2. **Live reload.** cowrite watches the file for external writes. When your co-writer saves, cowrite diffs the change into your buffer immediately — preserving your cursor position and scroll — instead of asking you to reload.
3. **Turn-based by convention.** Nothing enforces turns, but co-writing naturally alternates: you write, then the agent writes. If you both edit the same region at the same moment, the last write wins. In practice, collisions are rare — and everything is on disk, so use git if you want a safety net.

The editing itself should feel familiar:

- **Vim bindings** — modal editing with the core you actually use: motions, operators, visual mode, registers. cowrite is not a full vim clone and doesn't try to be.
- **Markdown decoration** — headings, emphasis, lists, blockquotes, and code fences styled in place. Decoration, not preview: you're always looking at the source.
- **Soft wrap** — prose wraps at word boundaries and motions operate on visual lines, so long paragraphs are pleasant to navigate.
- **Minimal chrome** — your text, a modeline, and nothing else.

### Recommended setup

Split panes in your terminal (Ghostty, tmux, iTerm2 — whatever you use):

```
┌──────────────────────────┬──────────────────────────┐
│  cowrite draft.md        │  claude                  │
│                          │                          │
│  # The Pitch             │  > Read draft.md and     │
│  Every startup deck…     │    write the next        │
│                          │    section in my voice.  │
└──────────────────────────┴──────────────────────────┘
```

Write your opening in the left pane. Ask your agent for the next passage in the right pane. Watch it land in your buffer. Repeat.

## Installing

```sh
brew install henrypoydar/tap/cowrite
```

Or build from source — see [Contributing](#contributing).

Then open any markdown file:

```sh
cowrite draft.md
```

## Contributing

cowrite has a small, deliberate scope: it's an editor for co-writing, not a vim replacement or a general-purpose IDE. Features are judged by whether they make writing alongside an agent better. Bug fixes and polish are always welcome; new features are worth an issue first.

To work on it locally:

```sh
git clone https://github.com/henrypoydar/cowrite.git
cd cowrite
mise install     # toolchain versions are pinned in the repo
```

Run the tests before opening a PR. Keep changes small and focused, and match the style of the surrounding code.

## License

MIT
