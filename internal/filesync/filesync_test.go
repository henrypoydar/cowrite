package filesync

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.md")
	e := NewEngine(path)
	if err := e.Save([]string{"hello", "world"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello\nworld\n" {
		t.Errorf("file = %q", data)
	}
	content, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(SplitLines(content), []string{"hello", "world"}) {
		t.Errorf("round trip = %v", SplitLines(content))
	}
}

func TestLoadMissingFile(t *testing.T) {
	content, err := Load(filepath.Join(t.TempDir(), "nope.md"))
	if err != nil || content != "" {
		t.Errorf("Load(missing) = %q, %v", content, err)
	}
}

func TestWatchReportsExternalWriteOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.md")
	e := NewEngine(path)
	if err := e.Save([]string{"start"}); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	defer close(done)
	ch, err := e.Watch(done)
	if err != nil {
		t.Fatal(err)
	}

	// our own save must not come back as a change
	if err := e.Save([]string{"start", "ours"}); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-ch:
		t.Fatalf("own write reported as external change: %+v", c)
	case <-time.After(200 * time.Millisecond):
	}

	// an external write must
	if err := os.WriteFile(path, []byte("start\nours\nagent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-ch:
		if c.Err != nil {
			t.Fatal(c.Err)
		}
		if !slices.Equal(c.Lines, []string{"start", "ours", "agent"}) {
			t.Errorf("change = %v", c.Lines)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("external write never reported")
	}
}
