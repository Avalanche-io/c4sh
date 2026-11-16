package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Avalanche-io/c4"
	"github.com/Avalanche-io/c4/c4m"
	c4store "github.com/Avalanche-io/c4/store"
)

// A bundle carries its c4m, so a folder whose listing the c4m holds inline
// travels with it: the folder's C4 ID is not a store object the bundle
// lacks. Only content that is in neither the store nor the c4m is missing.

// runPoolCapturing runs c4sh pool and returns its exit code and stderr.
func runPoolCapturing(t *testing.T, args ...string) (int, string) {
	t.Helper()
	withTestExit(t)
	r, w, _ := os.Pipe()
	origErr, origOut := os.Stderr, os.Stdout
	os.Stderr, os.Stdout = w, w
	code := catchExit(func() { runPool(args) })
	w.Close()
	os.Stderr, os.Stdout = origErr, origOut
	out, _ := io.ReadAll(r)
	return code, string(out)
}

// README L210: c4sh pool delivery.c4m ./bundle/, for a captured tree with
// folders, nested folders and an empty folder.
func TestPoolOfACapturedTreeIsComplete(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	c4store.NewTreeStore(storePath)
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	root := filepath.Join(dir, "project")
	buildEditTree(t, root)
	c4mPath := filepath.Join(dir, "delivery.c4m")
	captureTree(t, root, c4mPath)

	code, out := runPoolCapturing(t, c4mPath, filepath.Join(dir, "bundle"))
	if code != -1 {
		t.Fatalf("pool: exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "0 missing") {
		t.Errorf("pool output %q, want 0 missing", out)
	}
}

func TestPoolNamesAMissingFileAndExits1(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	store, _ := c4store.NewTreeStore(storePath)
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	root := filepath.Join(dir, "project")
	buildEditTree(t, root)
	c4mPath := filepath.Join(dir, "delivery.c4m")
	captureTree(t, root, c4mPath)
	id := c4.Identify(strings.NewReader("comp\n"))
	if err := store.Remove(id); err != nil {
		t.Fatal(err)
	}

	code, out := runPoolCapturing(t, c4mPath, filepath.Join(dir, "bundle"))
	if code != 1 {
		t.Errorf("pool: exit %d, want 1", code)
	}
	if !strings.Contains(out, "1 missing") {
		t.Errorf("pool output %q, want 1 missing", out)
	}
	if !strings.Contains(out, "shots/010/comp.nk") || !strings.Contains(out, id.String()) {
		t.Errorf("pool output %q does not name shots/010/comp.nk and its C4 ID", out)
	}
}

// What a bundle needs from the store: file content, a range's ID list
// unless the c4m carries it inline, and a folder's listing only when the
// c4m doesn't list the folder's children.
func TestPoolNeedsOnlyWhatTheC4mDoesNotCarry(t *testing.T) {
	ts := editTime
	fileID := c4.Identify(strings.NewReader("f"))
	listID := c4.Identify(strings.NewReader("a range ID list"))
	inlineListID := c4.Identify(strings.NewReader("an inline range ID list"))
	collapsedID := c4.Identify(strings.NewReader("a listing stored elsewhere"))
	emptyID := c4.Identify(strings.NewReader(""))

	m := c4m.NewManifest()
	listed := &c4m.Entry{Name: "listed/", Mode: os.ModeDir | 0755, Size: 1, Timestamp: ts, C4ID: c4.Identify(strings.NewReader("listed"))}
	file := &c4m.Entry{Name: "f.txt", Depth: 1, Mode: 0644, Size: 1, Timestamp: ts, C4ID: fileID}
	empty := &c4m.Entry{Name: "empty/", Mode: os.ModeDir | 0755, Size: 0, Timestamp: ts, C4ID: emptyID}
	collapsed := &c4m.Entry{Name: "collapsed/", Mode: os.ModeDir | 0755, Size: -1, Timestamp: ts, C4ID: collapsedID}
	seq := &c4m.Entry{Name: "frame.[1-3].exr", Mode: 0644, Size: 3, Timestamp: ts, C4ID: listID, IsSequence: true}
	inlineSeq := &c4m.Entry{Name: "other.[1-3].exr", Mode: 0644, Size: 3, Timestamp: ts, C4ID: inlineListID, IsSequence: true}
	for _, e := range []*c4m.Entry{seq, inlineSeq, listed, file, empty, collapsed} {
		m.AddEntry(e)
	}
	m.RangeData = map[c4.ID]string{inlineListID: "inline"}
	m.SortEntries()

	for _, tc := range []struct {
		e    *c4m.Entry
		want bool
	}{
		{file, true},
		{seq, true},
		{collapsed, true},
		{listed, false},
		{empty, false},
		{inlineSeq, false},
	} {
		if got := poolNeedsObject(m, tc.e); got != tc.want {
			t.Errorf("%s: needs a store object = %v, want %v", m.EntryPath(tc.e), got, tc.want)
		}
	}
}

// An empty folder and an empty file share a C4 ID (no bytes). The folder
// needs nothing, and must not stop the file's object being bundled.
func TestPoolBundlesAnEmptyFileAfterAnEmptyFolder(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	c4store.NewTreeStore(storePath)
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")

	root := filepath.Join(dir, "project")
	os.MkdirAll(filepath.Join(root, "a-empty"), 0755)
	os.MkdirAll(filepath.Join(root, "z"), 0755)
	os.WriteFile(filepath.Join(root, "z", "e.txt"), nil, 0644)
	pinTimes(t, root)
	c4mPath := filepath.Join(dir, "p.c4m")
	captureTree(t, root, c4mPath)

	bundle := filepath.Join(dir, "bundle")
	code, out := runPoolCapturing(t, c4mPath, bundle)
	if code != -1 {
		t.Fatalf("pool: exit %d:\n%s", code, out)
	}
	b, _ := c4store.NewTreeStore(filepath.Join(bundle, "store"))
	if !b.Has(emptyID) {
		t.Error("the empty file's object is not in the bundle")
	}
}
