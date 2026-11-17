package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Avalanche-io/c4/c4m"
	"github.com/Avalanche-io/c4/scan"
	c4store "github.com/Avalanche-io/c4/store"
)

// A capture describes the tree it was taken from: c4sh cp <dir>/ x.c4m:
// writes exactly what c4 id <dir>/ writes for it, links included, so every
// folder's size and C4 ID counts the entries the c4m lists. c4 id is the
// pinned core's scanner and encoder.

// buildLinkTree makes x, a/file, a/ln -> file and top -> a under root.
func buildLinkTree(t *testing.T, root string) {
	t.Helper()
	os.MkdirAll(filepath.Join(root, "a"), 0755)
	os.WriteFile(filepath.Join(root, "x"), []byte("x\n"), 0644)
	os.WriteFile(filepath.Join(root, "a", "file"), []byte("afile\n"), 0644)
	if err := os.Symlink("file", filepath.Join(root, "a", "ln")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink("a", filepath.Join(root, "top")); err != nil {
		t.Fatal(err)
	}
	pinTimes(t, root)
}

// scanText is what c4 id <root>/ writes: the library's full scan, encoded.
func scanText(t *testing.T, root string) string {
	t.Helper()
	m, err := scan.Dir(root, scan.WithMode(scan.ModeFull))
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := c4m.NewEncoder(&b).Encode(m); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestCaptureOfATreeWithLinksEqualsAScanOfIt(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	store, _ := c4store.NewTreeStore(storePath)
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	root := filepath.Join(dir, "t")
	buildLinkTree(t, root)
	got := captureTree(t, root, filepath.Join(dir, "x.c4m"))
	want := scanText(t, root)
	if got != want {
		t.Fatalf("capture differs from c4 id of the same tree:\ncapture:\n%s\nc4 id:\n%s", got, want)
	}
	for _, name := range []string{" top -> a ", " ln -> file "} {
		if !strings.Contains(got, name) {
			t.Errorf("capture has no %q line:\n%s", strings.TrimSpace(name), got)
		}
	}

	// Content goes to the store as c4 id -s stores it: a link to a file
	// stores its target's bytes.
	m, err := c4m.NewDecoder(strings.NewReader(got)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range m.Entries {
		if e.Name == "ln" && !store.Has(e.C4ID) {
			t.Errorf("a/ln's content (%s) is not in the store", e.C4ID)
		}
	}
}

func TestMultiDestinationCaptureDerivesFolderIDs(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	c4store.NewTreeStore(storePath)
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	root := filepath.Join(dir, "project")
	buildEditTree(t, root)
	c4mPath := filepath.Join(dir, "x.c4m")
	cpMultiDest(root+"/", []string{c4mPath + ":", filepath.Join(dir, "copy")})

	got, err := loadManifest(c4mPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := scan.Dir(root, scan.WithMode(scan.ModeFull))
	if err != nil {
		t.Fatal(err)
	}
	wantDirs := make(map[string]*c4m.Entry)
	for _, e := range want.Entries {
		if e.IsDir() {
			wantDirs[want.EntryPath(e)] = e
		}
	}
	seen := 0
	for _, e := range got.Entries {
		if !e.IsDir() {
			continue
		}
		seen++
		p := got.EntryPath(e)
		if e.C4ID.IsNil() {
			t.Errorf("%s: folder C4 ID is null", p)
			continue
		}
		if w := wantDirs[p]; w == nil || w.C4ID != e.C4ID || w.Size != e.Size {
			t.Errorf("%s: %d %s, want c4 id's %v", p, e.Size, e.C4ID, w)
		}
	}
	if seen == 0 {
		t.Fatal("the capture has no folders")
	}
}
