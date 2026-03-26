package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Avalanche-io/c4/c4m"
	c4store "github.com/Avalanche-io/c4/store"
)

// --------------------------------------------------------------------------
// catFromC4mTo
// --------------------------------------------------------------------------

func TestCatFromC4mTo_FileContent(t *testing.T) {
	te := newTestEnv(t)
	te.setStoreEnv(t)

	var buf bytes.Buffer
	if err := catFromC4mTo(&buf, te.c4mPath, "hello.txt"); err != nil {
		t.Fatalf("catFromC4mTo: %v", err)
	}
	if buf.String() != "hello world" {
		t.Errorf("content = %q, want %q", buf.String(), "hello world")
	}
}

func TestCatFromC4mTo_NestedFile(t *testing.T) {
	te := newTestEnv(t)
	te.setStoreEnv(t)

	var buf bytes.Buffer
	if err := catFromC4mTo(&buf, te.c4mPath, "sub/nested.txt"); err != nil {
		t.Fatalf("catFromC4mTo: %v", err)
	}
	if buf.String() != "nested content" {
		t.Errorf("content = %q, want %q", buf.String(), "nested content")
	}
}

func TestCatFromC4mTo_NotFound(t *testing.T) {
	te := newTestEnv(t)
	te.setStoreEnv(t)

	var buf bytes.Buffer
	err := catFromC4mTo(&buf, te.c4mPath, "missing.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want 'not found'", err)
	}
}

func TestCatFromC4mTo_Directory(t *testing.T) {
	te := newTestEnv(t)
	te.setStoreEnv(t)

	var buf bytes.Buffer
	err := catFromC4mTo(&buf, te.c4mPath, "sub/")
	if err == nil {
		t.Fatal("expected error for directory")
	}
	if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("error = %q, want 'is a directory'", err)
	}
}

func TestCatFromC4mTo_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	_, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}
	t.Setenv("C4_STORE", storePath)

	m := c4m.NewManifest()
	m.AddEntry(&c4m.Entry{
		Name:      "empty.txt",
		Depth:     0,
		Mode:      0644,
		Size:      0,
		Timestamp: c4m.NullTimestamp(),
		// C4ID is zero/nil — represents empty file
	})
	m.SortEntries()

	c4mPath := filepath.Join(dir, "test.c4m")
	if err := saveManifest(c4mPath, m); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}

	var buf bytes.Buffer
	if err := catFromC4mTo(&buf, c4mPath, "empty.txt"); err != nil {
		t.Fatalf("catFromC4mTo empty: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty output, got %q", buf.String())
	}
}

func TestCatFromC4mTo_NullIDNonZeroSize(t *testing.T) {
	dir := t.TempDir()
	m := c4m.NewManifest()
	m.AddEntry(&c4m.Entry{
		Name:      "broken.txt",
		Depth:     0,
		Mode:      0644,
		Size:      100, // non-zero size but nil C4ID
		Timestamp: c4m.NullTimestamp(),
	})
	m.SortEntries()

	c4mPath := filepath.Join(dir, "test.c4m")
	if err := saveManifest(c4mPath, m); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}

	var buf bytes.Buffer
	err := catFromC4mTo(&buf, c4mPath, "broken.txt")
	if err == nil {
		t.Fatal("expected error for null C4 ID with non-zero size")
	}
	if !strings.Contains(err.Error(), "null C4 ID") {
		t.Errorf("error = %q, want 'null C4 ID'", err)
	}
}

func TestCatFromC4mTo_BadManifestPath(t *testing.T) {
	var buf bytes.Buffer
	err := catFromC4mTo(&buf, "/nonexistent/path.c4m", "file.txt")
	if err == nil {
		t.Fatal("expected error for bad manifest path")
	}
}

func TestCatFromC4mTo_NoStore(t *testing.T) {
	te := newTestEnv(t)
	// Don't set C4_STORE — no store configured
	t.Setenv("C4_STORE", "")
	// Also unset HOME-based config
	t.Setenv("HOME", t.TempDir())

	var buf bytes.Buffer
	err := catFromC4mTo(&buf, te.c4mPath, "hello.txt")
	if err == nil {
		t.Fatal("expected error when no store configured")
	}
}

// --------------------------------------------------------------------------
// hasC4mArg
// --------------------------------------------------------------------------

func TestHasC4mArg(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"c4m colon path", []string{"project.c4m:file.txt"}, true},
		{"bare c4m", []string{"project.c4m"}, true},
		{"plain path", []string{"file.txt"}, false},
		{"flag only", []string{"-n"}, false},
		{"flag and c4m", []string{"-n", "project.c4m:file.txt"}, true},
		{"empty", []string{}, false},
		{"flag resembling c4m", []string{"-l"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasC4mArg(tt.args)
			if got != tt.want {
				t.Errorf("hasC4mArg(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// copyToStdout (test via pipe replacement is hard; test the writer path)
// --------------------------------------------------------------------------

func TestCatFromC4mTo_ContentIntegrity(t *testing.T) {
	// Test with binary-like content
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	store, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}
	t.Setenv("C4_STORE", storePath)

	content := "line1\nline2\nline3\n\x00binary\xff"
	id, err := store.Put(strings.NewReader(content))
	if err != nil {
		t.Fatalf("store.Put: %v", err)
	}

	m := c4m.NewManifest()
	m.AddEntry(&c4m.Entry{
		Name:      "data.bin",
		Depth:     0,
		Mode:      0644,
		Size:      int64(len(content)),
		Timestamp: c4m.NullTimestamp(),
		C4ID:      id,
	})
	m.SortEntries()

	c4mPath := filepath.Join(dir, "test.c4m")
	if err := saveManifest(c4mPath, m); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}

	var buf bytes.Buffer
	if err := catFromC4mTo(&buf, c4mPath, "data.bin"); err != nil {
		t.Fatalf("catFromC4mTo: %v", err)
	}
	if buf.String() != content {
		t.Errorf("content mismatch: got %d bytes, want %d bytes", buf.Len(), len(content))
	}
}

// --------------------------------------------------------------------------
// catFromC4mTo: write error
// --------------------------------------------------------------------------

type errWriter struct{}

func (e *errWriter) Write(p []byte) (n int, err error) {
	return 0, fmt.Errorf("simulated write error")
}

func TestCatFromC4mTo_WriteError(t *testing.T) {
	te := newTestEnv(t)
	te.setStoreEnv(t)

	err := catFromC4mTo(&errWriter{}, te.c4mPath, "hello.txt")
	if err == nil {
		t.Fatal("expected error for write failure")
	}
	if !strings.Contains(err.Error(), "write error") {
		t.Errorf("error = %q, want 'write error'", err)
	}
}

// --------------------------------------------------------------------------
// catFromC4m (wrapper using osExit)
// --------------------------------------------------------------------------

func TestCatFromC4m_WritesToStdout(t *testing.T) {
	te := newTestEnv(t)
	te.setStoreEnv(t)

	// Redirect stdout to capture output
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = origStdout })

	catFromC4m(te.c4mPath, "hello.txt")

	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	os.Stdout = origStdout

	if buf.String() != "hello world" {
		t.Errorf("catFromC4m output = %q, want %q", buf.String(), "hello world")
	}
}

func TestCatFromC4m_ErrorCallsExit(t *testing.T) {
	te := newTestEnv(t)
	te.setStoreEnv(t)

	withTestExit(t)
	code := catchExit(func() { catFromC4m(te.c4mPath, "nonexistent.txt") })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

// --------------------------------------------------------------------------
// Content not in store
// --------------------------------------------------------------------------

func TestCatFromC4mTo_ContentMissing(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	store, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}
	t.Setenv("C4_STORE", storePath)

	// Store content, get its ID, then use a DIFFERENT store (empty)
	id, err := store.Put(strings.NewReader("will be missing"))
	if err != nil {
		t.Fatalf("store.Put: %v", err)
	}

	emptyStorePath := filepath.Join(dir, "emptystore")
	_, err = c4store.NewTreeStore(emptyStorePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}
	t.Setenv("C4_STORE", emptyStorePath)

	m := c4m.NewManifest()
	m.AddEntry(&c4m.Entry{
		Name:      "missing.txt",
		Depth:     0,
		Mode:      0644,
		Size:      15,
		Timestamp: c4m.NullTimestamp(),
		C4ID:      id,
	})
	m.SortEntries()

	c4mPath := filepath.Join(dir, "test.c4m")
	if err := saveManifest(c4mPath, m); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}

	var buf bytes.Buffer
	err = catFromC4mTo(&buf, c4mPath, "missing.txt")
	if err == nil {
		t.Fatal("expected error for missing content")
	}
	if !strings.Contains(err.Error(), "content not in store") {
		t.Errorf("error = %q, want 'content not in store'", err)
	}
}
