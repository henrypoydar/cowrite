---
name: verify
description: Build and drive cowrite end-to-end in an isolated tmux session — launch the TUI, type through the vim engine, simulate an agent's external write, and observe the live merge. Use before committing changes to editor, sync, or rendering behavior.
---

# Verifying cowrite

cowrite is a TUI; its surface is the terminal. Drive the real binary in an
isolated tmux server and capture panes as evidence. Unit tests exist but
they are CI's job — verification here means watching the editor do the
thing.

## Build

```sh
mise exec -- go build -o cowrite .
```

## Drive

Use a dedicated tmux server (`-L cwv`) so nothing shares state with the
user's sessions, and a scratch file outside the repo:

```sh
tmux -L cwv new-session -d -s main -x 64 -y 12 "$PWD/cowrite /path/to/scratch/draft.md"
tmux -L cwv send-keys -t main -l "iHello world"   # -l = literal keystrokes
tmux -L cwv send-keys -t main Escape              # named keys separately
tmux -L cwv capture-pane -t main -p               # evidence
tmux -L cwv kill-server                           # cleanup
```

Gotchas:
- A `;` ending a send-keys argument is tmux's command separator — the key
  is silently dropped. Escape it: `send-keys -l 'g\;'`.
- Autosave debounce is 400ms — `sleep 0.8` after edits before checking disk.
- The watcher coalesces events for 30ms; `sleep 0.4` after an external
  write before capturing the merged screen.
- `send-keys -l ":q"` then `send-keys Enter` to quit; confirm with
  `tmux -L cwv ls` erroring "no server running".

## Flows worth driving

1. **Autosave**: type, Escape, wait, `cat` the file — content on disk with
   no `:w`.
2. **Live merge (the product)**: with the editor mid-INSERT on a partial
   line, overwrite the file externally (`printf ... > file`), wait, capture
   — the agent's lines appear, the partial line survives, typing continues
   on the correct line, and the merged doc autosaves back out.
3. **Undo granularity**: after a merge, `u` steps back the user's insert
   session and the agent's whole turn as separate single steps; `C-r`
   (send-keys `C-r`) restores.
4. **Soft wrap reflow**: `tmux -L cwv resize-window -t main -x 28`, capture.
5. **Cold-start race** (mitigated, see DESIGN.md): on a fresh empty file
   the first keystroke saves eagerly — `cat` the file ~150ms after typing
   begins and it already holds the first character, so an agent that
   reads-then-appends merges disjointly. A blind full-file overwrite still
   takes conflicting lines; that's disk-wins policy, expected.
6. **Decoration**: open a fixture with a heading, emphasis, inline code, a
   list, a quote, and a fence; `capture-pane -e` shows the raw SGR codes
   (heading bold+cyan, markers faint, code yellow).
7. **Visual mode**: `vip` then capture with `-e` — the paragraph renders
   reverse-video except the cursor cell, which inverts back to stay
   visible; status bar reads V-LINE for linewise selections.
8. **Merge visibility**: after an external write, the message line shows
   "co-writer: +N -M lines (g; to jump)", merged lines carry a background
   tint (`48;5;236` dark / `48;5;254` light) fading after ~3s, and
   `g\;` jumps the cursor to the merge site.
9. **Centering**: at 110 cols the text column caps at 80 and pads left by
   15; narrow panes use full width.
