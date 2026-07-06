// Package filesync makes the file on disk a shared workspace: atomic
// autosaves, a watcher that reports external writes (suppressing our own),
// and a three-way merge for folding those writes into a live buffer.
package filesync

import (
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	gosync "sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// coalesce is how long the watcher waits after the last event before
// reading — agents often produce several events per logical write.
const coalesce = 30 * time.Millisecond

// Engine tracks one file's sync state. base is the content at last sync in
// either direction: what buffer and disk last agreed on.
type Engine struct {
	path string

	mu        gosync.Mutex
	base      []string
	lastWrite [32]byte
	haveWrite bool
}

func NewEngine(path string) *Engine {
	return &Engine{path: path}
}

// Load reads the file, returning "" (and no error) if it doesn't exist yet.
func Load(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return string(data), err
}

// SplitLines converts file content to buffer lines, dropping the trailing
// newline Save adds.
func SplitLines(content string) []string {
	return strings.Split(strings.TrimSuffix(content, "\n"), "\n")
}

func (e *Engine) Base() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.base
}

func (e *Engine) SetBase(lines []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.base = slices.Clone(lines)
}

// Save writes lines atomically (temp file + rename) and records the write
// so the watcher can tell our writes from external ones.
func (e *Engine) Save(lines []string) error {
	data := []byte(strings.Join(lines, "\n") + "\n")

	mode := fs.FileMode(0o644)
	if info, err := os.Stat(e.path); err == nil {
		mode = info.Mode().Perm()
	}

	dir := filepath.Dir(e.path)
	tmp, err := os.CreateTemp(dir, ".cowrite-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}

	// Record before the rename lands so the watcher can never see the new
	// file before we know its hash.
	e.mu.Lock()
	e.lastWrite = sha256.Sum256(data)
	e.haveWrite = true
	e.base = slices.Clone(lines)
	e.mu.Unlock()

	return os.Rename(tmp.Name(), e.path)
}

// Change is an external modification to the file on disk.
type Change struct {
	Lines []string
	Err   error
}

// Watch reports external changes until done closes. It watches the parent
// directory: atomic saves (ours and many agents') replace the file by
// rename, which breaks a watch on the file itself.
func (e *Engine) Watch(done <-chan struct{}) (<-chan Change, error) {
	abs, err := filepath.Abs(e.path)
	if err != nil {
		return nil, err
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := w.Add(filepath.Dir(abs)); err != nil {
		w.Close()
		return nil, err
	}

	ch := make(chan Change, 1)
	go func() {
		defer w.Close()
		var timerC <-chan time.Time
		for {
			select {
			case <-done:
				return
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				ch <- Change{Err: err}
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if filepath.Clean(ev.Name) != abs {
					continue
				}
				if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
					continue
				}
				timerC = time.After(coalesce)
			case <-timerC:
				timerC = nil
				data, err := os.ReadFile(abs)
				if err != nil {
					continue // mid-rename; the next event retries
				}
				e.mu.Lock()
				own := e.haveWrite && sha256.Sum256(data) == e.lastWrite
				e.mu.Unlock()
				if own {
					continue
				}
				ch <- Change{Lines: SplitLines(string(data))}
			}
		}
	}()
	return ch, nil
}
