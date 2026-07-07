# cowrite — design

The README states the product; this file states how it's built. See the
[build order](#build-order) for where the implementation currently stands.

## Stack

Go, with Bubble Tea (TUI event loop), Lipgloss (styling), and fsnotify (file
watching). Rationale: the hard parts of cowrite — a modal vim engine and a
soft-wrap renderer — are hand-rolled in any language, so the stack decision
comes down to the surrounding ecosystem and iteration speed. Bubble Tea's
Elm-style `state + message → new state` loop is the same shape as a modal
editor's key handling, Go ships a single static binary (the whole install
story), and performance headroom is irrelevant for prose-sized files. Rust
(Ratatui/ropey/tree-sitter) was the runner-up: better editor internals, but
headroom this product deliberately never uses.

## Architecture

```
cowrite/
├── main.go              CLI entry: open/create file, start the tea.Program
└── internal/
    ├── buffer/          text buffer: lines, edit ops, undo, dirty state
    ├── vim/             modal engine: keypress → intent
    ├── render/          soft wrap layout, logical↔display position mapping
    ├── filesync/        autosave, file watcher, diff3 merge  ← the product
    └── app/             Bubble Tea model gluing it all together
```

**buffer** — a slice of rune-lines with one mutation primitive, `Replace`,
plus undo/redo. Every change flows through `Replace` — keystrokes, undo, and
merged agent edits alike — so undo history sees everything. Changes are
recorded in groups: an insert-mode session is one undoable group, and so is a
merged agent turn (`u` rolls back the agent's whole passage).

**vim** — a pure state machine: `(mode, pending, keypress) → commands`. No
I/O, no buffer access; motions resolve against a `Lines` interface. Entirely
table-driven-testable. v1 scope: normal/insert/command-line modes, counts,
operators `d c y`, motions `h j k l w b e 0 $ f t gg G`, paste, undo/redo.
`j`/`k` move by display line (vim's `gj`/`gk`) — the right default for prose.

**render** — computes a word-boundary soft-wrap layout: logical lines →
display rows, with position mapping in both directions (needed for
display-line motions and the viewport). Stateless; recomputed per edit,
which is fine at prose scale. Markdown decoration will live here too.

**filesync** — the reason cowrite exists:

- *Autosave*: debounced ~400ms after the last edit; writes are atomic
  (temp file + rename) so the agent never reads a half-written file.
- *Self-write suppression*: we record the hash of every write; watcher
  events whose content matches are ours, and are dropped.
- *Watching*: fsnotify on the parent directory (renames swap inodes, so
  watching the file itself breaks after the first atomic save). Event
  bursts are coalesced before reading.
- *Merge*: we keep `base`, the file content at last sync. On an external
  change, diff `base → disk` and `base → buffer` (LCS line diff), then
  three-way merge. Regions only the agent touched apply cleanly around
  your cursor; regions you both touched resolve **disk-wins** (the agent
  wrote last). Insertions at the same point keep both, agent's first.
  After merging, `base` becomes the disk content and any surviving local
  edits autosave back out.

All of it runs inside Bubble Tea's message loop — the watcher goroutine and
debounce timer deliver messages; the update function is the only writer to
model state, so there's no locking.

## Decisions

- **Disk wins on conflicting hunks.** The alternative (buffer wins) protects
  the human but silently discards the agent's work, which felt worse. With
  ~400ms autosave the conflict window is tiny either way.
- **`:q` saves and quits.** In a continuous-save editor, refusing to quit
  because of unsaved changes is noise; the file is already current or about
  to be. `:q!` quits without the final save.
- **The cold-start race, mitigated.** On a brand-new empty file there is
  no base yet, so a user's first words and an agent write both claimed
  line 0 and disk-wins dropped the user's text (verified live). Fix: the
  very first edit of an empty buffer saves immediately, skipping the
  debounce — an agent that reads-then-appends now merges disjointly
  (re-verified live). An agent that blindly overwrites the whole file
  still takes line 0; that's the disk-wins policy, not the race.
- **Known edge — stale whole-file agent writes resurrect edits.** If the
  user deletes a line and the agent then writes the *entire* file from a
  version read before that deletion, the merge re-adds the line — on disk
  the agent wrote last, and disk wins (verified live; it presents as "dd
  doesn't stick"). cowrite cannot distinguish "agent deliberately restored
  this" from "agent had stale context". The working conventions: agents
  should re-read the file (or use targeted edits, like Claude Code's Edit
  tool) rather than whole-file writes from memory, and `u` undoes the
  agent's merge in one step when it happens.
- **No embedded agent pane.** Running the agent *inside* cowrite would
  mean building a terminal multiplexer (pty + VT100 emulation + key
  routing) and choosing an agent to integrate — surrendering both the
  small scope and the filesystem-is-the-protocol agnosticism. The terminal
  already does splits better. The friction that idea points at is real,
  though, and gets cheaper answers: `--with <command>` opens the agent in
  a tmux split (shipped, 0.2.0); a `:co <prompt>` one-shot ex command
  routed through the existing watcher/merge is a possible later
  experiment, with the caveat that one-shot prompts lose the
  conversational session that makes a live agent pane valuable.
- **`ctrl+[` is decoded by hand.** Ghostty (following the fixterms spec)
  encodes `ctrl+[` as `CSI 91;5u` rather than a bare ESC byte, and Bubble
  Tea v1 can't parse CSI-u keys — it surfaces the sequence as an unexported
  unknown-CSI message. `app.csiEscape` matches that message by reflection
  and decodes the fixterms and xterm-modifyOtherKeys escape encodings, so
  `ctrl+[` leaves insert mode everywhere vim muscle memory expects it to.
- **Cursor width is rune-based** for now; grapheme clusters and east-asian
  widths via `rivo/uniseg` are a known future fix, not a v1 blocker.
- **Trailing newline**: files are stored with one; the buffer strips it on
  load and restores it on save.

## Build order

1. ~~**Skeleton** — open a file, render plain text, `hjkl`, `:q`.~~
2. ~~**Editing** — insert mode, edits, `:w`, undo.~~
3. ~~**Soft wrap** — display-line layout and motions.~~
4. ~~**Co-writing, clean case** — autosave + watcher + reload-when-clean.~~
5. ~~**Co-writing, real case** — diff3 merge into a dirty buffer.~~
6. ~~**Decoration** — markdown styling pass (headings, emphasis, lists,
   blockquotes, code fences).~~
7. ~~**Vim depth** — visual mode (`v`/`V`), text objects (`iw`/`aw`/
   `ip`/`ap`), paragraph motions (`{`/`}`), `J`, `.` repeat.~~ Named
   registers deliberately deferred; the unnamed register covers prose
   workflows.
8. ~~**Writing-session polish** — `/` search with `n`/`N` wrap-around
   (plain substring, case-sensitive, per-line), insert-mode arrow keys,
   `^`, live word count in the status bar.~~
9. ~~**Merge visibility & comfort** — merged lines get a background tint
   fading after 3s, the message line reports "co-writer: +N -M lines",
   `g;` jumps to the merge site; the text column caps at 80ch and centers
   in wider terminals; insert-mode cursor renders as an underline; a
   panic flushes the buffer to `<file>.crash` before dying.~~
10. ~~**Ship** — goreleaser, Homebrew tap.~~ v0.1.0 released;
    `brew install henrypoydar/tap/cowrite`.
11. ~~**Launcher** — `cowrite <file> --with <command>` opens the agent in
    a tmux split (`{file}` placeholder, focus stays on the editor);
    `--version`.~~ v0.2.0.

## Testing

The vim engine and the merge are pure functions and carry the test weight
(table-driven). The app model is driven directly in tests — Bubble Tea
models are just `Update(msg)`/`View()`, no pty needed — including a real
watcher round-trip against a temp file. Rendering is trusted to eyeballs.
