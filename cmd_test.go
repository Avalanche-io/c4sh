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

// testEnv holds all paths and resources for a single test scenario.
type testEnv struct {
	dir       string            // temp root
	storePath string            // TreeStore directory
	store     *c4store.TreeStore // content store
	c4mPath   string            // path to test.c4m
}

// newTestEnv creates a temp directory with a TreeStore and a c4m file
// containing a small file tree:
//
//	hello.txt   ("hello world")
//	sub/
//	  nested.txt ("nested content")
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()

	storePath := filepath.Join(dir, "store")
	store, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}

	// Store content
	helloID, err := store.Put(strings.NewReader("hello world"))
	if err != nil {
		t.Fatalf("store.Put hello: %v", err)
	}
	nestedID, err := store.Put(strings.NewReader("nested content"))
	if err != nil {
		t.Fatalf("store.Put nested: %v", err)
	}

	// Build manifest
	m := c4m.NewManifest()
	m.AddEntry(&c4m.Entry{
		Name:      "hello.txt",
		Depth:     0,
		Mode:      0644,
		Size:      int64(len("hello world")),
		Timestamp: c4m.NullTimestamp(),
		C4ID:      helloID,
	})
	m.AddEntry(&c4m.Entry{
		Name:      "sub/",
		Depth:     0,
		Mode:      os.ModeDir | 0755,
		Size:      -1,
		Timestamp: c4m.NullTimestamp(),
	})
	m.AddEntry(&c4m.Entry{
		Name:      "nested.txt",
		Depth:     1,
		Mode:      0644,
		Size:      int64(len("nested content")),
		Timestamp: c4m.NullTimestamp(),
		C4ID:      nestedID,
	})
	m.SortEntries()

	c4mPath := filepath.Join(dir, "test.c4m")
	if err := saveManifest(c4mPath, m); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}

	return &testEnv{
		dir:       dir,
		storePath: storePath,
		store:     store,
		c4mPath:   c4mPath,
	}
}

// setStoreEnv sets C4_STORE so openStore() finds our test store.
func (te *testEnv) setStoreEnv(t *testing.T) {
	t.Helper()
	t.Setenv("C4_STORE", te.storePath)
}

// loadC4m loads and returns the manifest from the test c4m path.
func (te *testEnv) loadC4m(t *testing.T) *c4m.Manifest {
	t.Helper()
	m, err := loadManifest(te.c4mPath)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	return m
}

// manifestPaths returns all full paths from a manifest.
func manifestPaths(m *c4m.Manifest) []string {
	var names []string
	for _, e := range m.Entries {
		names = append(names, entryFullPath(m, e))
	}
	return names
}

// --------------------------------------------------------------------------
// cp: real → c4m
// --------------------------------------------------------------------------

func TestCpRealToC4m(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	store, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}
	_ = store

	t.Setenv("C4_STORE", storePath)

	// Create a real directory tree to capture.
	srcDir := filepath.Join(dir, "src")
	os.MkdirAll(filepath.Join(srcDir, "docs"), 0755)
	os.WriteFile(filepath.Join(srcDir, "readme.txt"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(srcDir, "docs", "guide.txt"), []byte("guide content"), 0644)

	// Capture into a c4m file.
	c4mPath := filepath.Join(dir, "captured.c4m")
	cpRealToC4m(srcDir, c4mPath+":")

	// Verify c4m was created.
	m, err := loadManifest(c4mPath)
	if err != nil {
		t.Fatalf("loadManifest after capture: %v", err)
	}

	names := manifestPaths(m)
	wantNames := map[string]bool{
		"readme.txt":      true,
		"docs":            true,
		"docs/guide.txt":  true,
	}
	for _, n := range names {
		delete(wantNames, n)
	}
	if len(wantNames) > 0 {
		t.Errorf("missing entries: %v (got %v)", wantNames, names)
	}

	// Verify content was stored — every file entry should have a non-nil C4ID
	// and the store should have that object.
	for _, e := range m.Entries {
		if e.IsDir() {
			continue
		}
		if e.C4ID.IsNil() {
			t.Errorf("entry %s has nil C4ID", e.Name)
			continue
		}
		if !store.Has(e.C4ID) {
			t.Errorf("store missing content for %s (%s)", e.Name, e.C4ID)
		}
	}
}

func TestCpRealToC4m_SingleFile(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	store, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}
	_ = store

	t.Setenv("C4_STORE", storePath)

	// Create a single file to capture.
	srcFile := filepath.Join(dir, "single.txt")
	os.WriteFile(srcFile, []byte("single file content"), 0644)

	c4mPath := filepath.Join(dir, "single.c4m")
	cpRealToC4m(srcFile, c4mPath+":")

	m, err := loadManifest(c4mPath)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}

	if len(m.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(m.Entries))
	}
	if m.Entries[0].Name != "single.txt" {
		t.Errorf("entry name = %q, want %q", m.Entries[0].Name, "single.txt")
	}
	if m.Entries[0].C4ID.IsNil() {
		t.Error("single file entry has nil C4ID")
	}
}

// --------------------------------------------------------------------------
// cp: c4m → real
// --------------------------------------------------------------------------

func TestCpC4mToReal(t *testing.T) {
	te := newTestEnv(t)
	te.setStoreEnv(t)

	outDir := filepath.Join(te.dir, "extracted")
	cpC4mToReal(te.c4mPath+":", outDir)

	// Verify files were created with correct content.
	data, err := os.ReadFile(filepath.Join(outDir, "hello.txt"))
	if err != nil {
		t.Fatalf("read hello.txt: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("hello.txt = %q, want %q", data, "hello world")
	}

	data, err = os.ReadFile(filepath.Join(outDir, "sub", "nested.txt"))
	if err != nil {
		t.Fatalf("read sub/nested.txt: %v", err)
	}
	if string(data) != "nested content" {
		t.Errorf("sub/nested.txt = %q, want %q", data, "nested content")
	}
}

func TestCpC4mToReal_SubtreeExtract(t *testing.T) {
	te := newTestEnv(t)
	te.setStoreEnv(t)

	outDir := filepath.Join(te.dir, "sub_extract")
	os.MkdirAll(outDir, 0755)
	cpC4mToReal(te.c4mPath+":sub/", outDir)

	// Only nested.txt should be extracted (children of sub/).
	data, err := os.ReadFile(filepath.Join(outDir, "nested.txt"))
	if err != nil {
		t.Fatalf("read nested.txt: %v", err)
	}
	if string(data) != "nested content" {
		t.Errorf("nested.txt = %q, want %q", data, "nested content")
	}

	// hello.txt should NOT be in the output.
	if _, err := os.Stat(filepath.Join(outDir, "hello.txt")); err == nil {
		t.Error("hello.txt should not exist in subtree extract")
	}
}

// --------------------------------------------------------------------------
// cp: c4m → c4m
// --------------------------------------------------------------------------

func TestCpC4mToC4m(t *testing.T) {
	te := newTestEnv(t)

	dstC4m := filepath.Join(te.dir, "dest.c4m")

	// Copy all entries from test.c4m into dest.c4m
	cpC4mToC4m(te.c4mPath+":", dstC4m+":")

	m, err := loadManifest(dstC4m)
	if err != nil {
		t.Fatalf("loadManifest dest: %v", err)
	}

	srcM := te.loadC4m(t)
	if len(m.Entries) != len(srcM.Entries) {
		t.Errorf("entry count = %d, want %d", len(m.Entries), len(srcM.Entries))
	}

	// Verify C4IDs were preserved.
	for i, e := range m.Entries {
		if i >= len(srcM.Entries) {
			break
		}
		if e.C4ID != srcM.Entries[i].C4ID {
			t.Errorf("entry %d C4ID mismatch: %s != %s", i, e.C4ID, srcM.Entries[i].C4ID)
		}
	}
}

func TestCpC4mToC4m_SubtreeCopy(t *testing.T) {
	te := newTestEnv(t)

	dstC4m := filepath.Join(te.dir, "dest.c4m")

	// Copy sub/ subtree into dest.c4m root.
	cpC4mToC4m(te.c4mPath+":sub/", dstC4m+":")

	m, err := loadManifest(dstC4m)
	if err != nil {
		t.Fatalf("loadManifest dest: %v", err)
	}

	// Should contain nested.txt at depth 0.
	if len(m.Entries) != 1 {
		t.Fatalf("expected 1 entry (nested.txt), got %d: %v", len(m.Entries), manifestPaths(m))
	}
	if m.Entries[0].Name != "nested.txt" {
		t.Errorf("entry name = %q, want %q", m.Entries[0].Name, "nested.txt")
	}
	if m.Entries[0].Depth != 0 {
		t.Errorf("depth = %d, want 0", m.Entries[0].Depth)
	}
}

// --------------------------------------------------------------------------
// mv
// --------------------------------------------------------------------------

func TestMv_RenameFile(t *testing.T) {
	te := newTestEnv(t)

	// Rename hello.txt to greetings.txt.
	// Set C4_CONTEXT so resolveContextPath works.
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")

	m := te.loadC4m(t)
	origID := findEntryByName(m, "hello.txt").C4ID

	runMvInternal(t, te.c4mPath, "hello.txt", "greetings.txt")

	m = te.loadC4m(t)
	if findEntryByName(m, "hello.txt") != nil {
		t.Error("hello.txt should not exist after mv")
	}
	e := findEntryByName(m, "greetings.txt")
	if e == nil {
		t.Fatal("greetings.txt should exist after mv")
	}
	if e.C4ID != origID {
		t.Errorf("C4ID changed during mv: %s -> %s", origID, e.C4ID)
	}
}

func TestMv_MoveIntoDirectory(t *testing.T) {
	te := newTestEnv(t)

	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")

	// Move hello.txt into sub/
	runMvInternal(t, te.c4mPath, "hello.txt", "sub/")

	m := te.loadC4m(t)
	// hello.txt at depth 0 should not exist.
	for _, e := range m.Entries {
		if e.Name == "hello.txt" && e.Depth == 0 {
			t.Error("hello.txt should not exist at root (depth 0) after mv into sub/")
		}
	}
	// Should exist as sub/hello.txt
	if e := m.GetEntry("sub/hello.txt"); e == nil {
		t.Error("sub/hello.txt should exist after mv into sub/")
	}
}

// runMvInternal performs an mv operation without os.Exit.
// It calls the production mvEntry function directly.
func runMvInternal(t *testing.T, c4mPath, src, dst string) {
	t.Helper()

	srcC4m, srcSub := resolveContextPath(src)
	dstC4m, dstSub := resolveContextPath(dst)

	if srcC4m == "" {
		t.Fatalf("mv: could not resolve source %q", src)
	}
	if dstC4m == "" {
		dstC4m = srcC4m
		dstSub = dst
	}
	if srcC4m != dstC4m {
		t.Fatal("mv: cannot move between different c4m files")
	}

	m, err := loadManifest(srcC4m)
	if err != nil {
		t.Fatalf("mv loadManifest: %v", err)
	}

	if err := mvEntry(m, srcSub, dstSub); err != nil {
		t.Fatalf("mv: %v", err)
	}

	if err := saveManifest(srcC4m, m); err != nil {
		t.Fatalf("mv saveManifest: %v", err)
	}
}

// --------------------------------------------------------------------------
// rm
// --------------------------------------------------------------------------

func TestRm_RemoveFile(t *testing.T) {
	te := newTestEnv(t)

	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")

	runRmInternal(t, te.c4mPath, false, "hello.txt")

	m := te.loadC4m(t)
	if findEntryByName(m, "hello.txt") != nil {
		t.Error("hello.txt should not exist after rm")
	}
	// sub/ and nested.txt should still be present.
	if findEntryByName(m, "sub/") == nil {
		t.Error("sub/ should still exist after rm hello.txt")
	}
}

func TestRm_RemoveDirectoryRecursive(t *testing.T) {
	te := newTestEnv(t)

	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")

	runRmInternal(t, te.c4mPath, true, "sub/")

	m := te.loadC4m(t)
	if findEntryByName(m, "sub/") != nil {
		t.Error("sub/ should not exist after rm -r")
	}
	if findEntryByName(m, "nested.txt") != nil {
		t.Error("nested.txt should not exist after rm -r sub/")
	}
	// hello.txt should remain.
	if findEntryByName(m, "hello.txt") == nil {
		t.Error("hello.txt should still exist after rm -r sub/")
	}
}

// runRmInternal performs rm without os.Exit.
// It calls the production rmEntries function directly.
func runRmInternal(t *testing.T, c4mPath string, recursive bool, paths ...string) {
	t.Helper()

	m, err := loadManifest(c4mPath)
	if err != nil {
		t.Fatalf("rm loadManifest: %v", err)
	}

	modified, errs := rmEntries(m, paths, recursive, false)
	if errs > 0 {
		t.Fatalf("rm: %d errors", errs)
	}

	if modified {
		if err := saveManifest(c4mPath, m); err != nil {
			t.Fatalf("rm saveManifest: %v", err)
		}
	}
}

// --------------------------------------------------------------------------
// mkdir
// --------------------------------------------------------------------------

func TestMkdir_CreateDirectory(t *testing.T) {
	te := newTestEnv(t)

	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")

	m := te.loadC4m(t)
	mkdirOne(m, "newdir", new(int))
	m.SortEntries()
	if err := saveManifest(te.c4mPath, m); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}

	m = te.loadC4m(t)
	e := findEntryByName(m, "newdir/")
	if e == nil {
		t.Fatal("newdir/ should exist after mkdir")
	}
	if !e.IsDir() {
		t.Error("newdir/ should be a directory")
	}
}

func TestMkdir_CreateNestedWithParents(t *testing.T) {
	te := newTestEnv(t)

	m := te.loadC4m(t)
	if !mkdirAll(m, "a/b/c") {
		t.Fatal("mkdirAll returned false, expected true")
	}
	m.SortEntries()
	if err := saveManifest(te.c4mPath, m); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}

	m = te.loadC4m(t)
	for _, name := range []string{"a/", "b/", "c/"} {
		if findEntryByName(m, name) == nil {
			t.Errorf("%s should exist after mkdir -p a/b/c", name)
		}
	}

	// Verify the depth hierarchy: a/ at 0, b/ at 1, c/ at 2.
	aEntry := m.GetEntry("a/")
	if aEntry == nil || aEntry.Depth != 0 {
		t.Errorf("a/ depth = %v, want 0", aEntry)
	}
	bEntry := m.GetEntry("a/b/")
	if bEntry == nil || bEntry.Depth != 1 {
		t.Errorf("a/b/ depth = %v, want 1", bEntry)
	}
	cEntry := m.GetEntry("a/b/c/")
	if cEntry == nil || cEntry.Depth != 2 {
		t.Errorf("a/b/c/ depth = %v, want 2", cEntry)
	}
}

func TestMkdir_AlreadyExists(t *testing.T) {
	te := newTestEnv(t)

	m := te.loadC4m(t)
	errs := 0
	// sub/ already exists
	modified := mkdirOne(m, "sub", &errs)
	if modified {
		t.Error("mkdirOne should return false for existing directory")
	}
	if errs == 0 {
		t.Error("mkdirOne should increment errs for existing directory")
	}
}

// --------------------------------------------------------------------------
// ls helpers
// --------------------------------------------------------------------------

func TestEntriesAtPath_Root(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	entries := entriesAtPath(m, "")
	if len(entries) != 2 {
		t.Fatalf("root entries = %d, want 2 (hello.txt, sub/)", len(entries))
	}
}

func TestEntriesAtPath_Subdirectory(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	entries := entriesAtPath(m, "sub/")
	if len(entries) != 1 {
		t.Fatalf("sub/ entries = %d, want 1 (nested.txt)", len(entries))
	}
	if entries[0].Name != "nested.txt" {
		t.Errorf("sub/ child = %q, want %q", entries[0].Name, "nested.txt")
	}
}

// --------------------------------------------------------------------------
// cat helpers
// --------------------------------------------------------------------------

func TestCatFromC4m_ReadsContent(t *testing.T) {
	te := newTestEnv(t)
	te.setStoreEnv(t)

	m := te.loadC4m(t)
	e := findEntry(m, "hello.txt")
	if e == nil {
		t.Fatal("hello.txt not found")
	}

	// Read content from store directly (catFromC4m calls os.Exit, so test the pipe).
	rc, err := te.store.Open(e.C4ID)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "hello world" {
		t.Errorf("content = %q, want %q", data, "hello world")
	}
}

func TestCatFromC4m_NestedFile(t *testing.T) {
	te := newTestEnv(t)
	te.setStoreEnv(t)

	m := te.loadC4m(t)
	e := findEntry(m, "sub/nested.txt")
	if e == nil {
		t.Fatal("sub/nested.txt not found")
	}

	rc, err := te.store.Open(e.C4ID)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "nested content" {
		t.Errorf("content = %q, want %q", data, "nested content")
	}
}

// --------------------------------------------------------------------------
// pool + ingest round-trip
// --------------------------------------------------------------------------

func TestPoolAndIngest(t *testing.T) {
	te := newTestEnv(t)
	te.setStoreEnv(t)

	// Pool: bundle the c4m + store objects.
	bundleDir := filepath.Join(te.dir, "bundle")
	poolInternal(t, te.c4mPath, bundleDir, te.store)

	// Verify bundle structure.
	if _, err := os.Stat(filepath.Join(bundleDir, "test.c4m")); err != nil {
		t.Error("bundle should contain test.c4m")
	}
	if _, err := os.Stat(filepath.Join(bundleDir, "store")); err != nil {
		t.Error("bundle should contain store/")
	}
	if _, err := os.Stat(filepath.Join(bundleDir, "extract.sh")); err != nil {
		t.Error("bundle should contain extract.sh")
	}

	// Verify pool store has the referenced objects.
	poolStore, err := c4store.NewTreeStore(filepath.Join(bundleDir, "store"))
	if err != nil {
		t.Fatalf("open pool store: %v", err)
	}
	m := te.loadC4m(t)
	for _, e := range m.Entries {
		if e.C4ID.IsNil() {
			continue
		}
		if !poolStore.Has(e.C4ID) {
			t.Errorf("pool store missing %s (%s)", e.Name, e.C4ID)
		}
	}

	// Ingest into a new store.
	newStorePath := filepath.Join(te.dir, "newstore")
	newStore, err := c4store.NewTreeStore(newStorePath)
	if err != nil {
		t.Fatalf("NewTreeStore newstore: %v", err)
	}

	ingestInternal(t, bundleDir, newStore)

	// Verify all content is in the new store.
	for _, e := range m.Entries {
		if e.C4ID.IsNil() {
			continue
		}
		if !newStore.Has(e.C4ID) {
			t.Errorf("new store missing %s (%s)", e.Name, e.C4ID)
		}
	}

	// Verify content is readable and correct.
	helloEntry := findEntry(m, "hello.txt")
	rc, err := newStore.Open(helloEntry.C4ID)
	if err != nil {
		t.Fatalf("newStore.Open hello: %v", err)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	if string(data) != "hello world" {
		t.Errorf("ingested hello = %q, want %q", data, "hello world")
	}
}

// poolInternal replicates runPool logic without os.Exit.
func poolInternal(t *testing.T, c4mPath, outDir string, srcStore *c4store.TreeStore) {
	t.Helper()

	m, err := loadManifest(c4mPath)
	if err != nil {
		t.Fatalf("pool loadManifest: %v", err)
	}

	poolStorePath := filepath.Join(outDir, "store")
	dst, err := c4store.NewTreeStore(poolStorePath)
	if err != nil {
		t.Fatalf("pool NewTreeStore: %v", err)
	}

	for _, e := range m.Entries {
		if e.C4ID.IsNil() || dst.Has(e.C4ID) {
			continue
		}
		rc, err := srcStore.Open(e.C4ID)
		if err != nil {
			t.Fatalf("pool src.Open %s: %v", e.C4ID, err)
		}
		if _, err := dst.Put(rc); err != nil {
			rc.Close()
			t.Fatalf("pool dst.Put %s: %v", e.C4ID, err)
		}
		rc.Close()
	}

	c4mDest := filepath.Join(outDir, filepath.Base(c4mPath))
	if err := copyFile(c4mPath, c4mDest); err != nil {
		t.Fatalf("pool copyFile: %v", err)
	}

	if err := writeExtractScript(outDir, filepath.Base(c4mPath), m); err != nil {
		t.Fatalf("pool writeExtractScript: %v", err)
	}
}

// ingestInternal replicates runIngest logic without os.Exit.
func ingestInternal(t *testing.T, bundleDir string, localStore *c4store.TreeStore) {
	t.Helper()

	poolStorePath := filepath.Join(bundleDir, "store")
	poolStore, err := c4store.NewTreeStore(poolStorePath)
	if err != nil {
		t.Fatalf("ingest open pool store: %v", err)
	}

	walkTreeStore(poolStorePath, func(id c4ID) {
		if localStore.Has(id) {
			return
		}
		rc, err := poolStore.Open(id)
		if err != nil {
			t.Errorf("ingest read %s: %v", id, err)
			return
		}
		defer rc.Close()
		if _, err := localStore.Put(rc); err != nil {
			t.Errorf("ingest store %s: %v", id, err)
		}
	})
}

// --------------------------------------------------------------------------
// cp round-trip: real → c4m → real
// --------------------------------------------------------------------------

func TestCpRoundTrip(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	_, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}
	t.Setenv("C4_STORE", storePath)

	// Create source directory with varied content.
	srcDir := filepath.Join(dir, "src")
	os.MkdirAll(filepath.Join(srcDir, "a", "b"), 0755)
	os.WriteFile(filepath.Join(srcDir, "root.txt"), []byte("root"), 0644)
	os.WriteFile(filepath.Join(srcDir, "a", "mid.txt"), []byte("middle"), 0644)
	os.WriteFile(filepath.Join(srcDir, "a", "b", "deep.txt"), []byte("deep"), 0644)

	// Capture.
	c4mPath := filepath.Join(dir, "roundtrip.c4m")
	cpRealToC4m(srcDir, c4mPath+":")

	// Extract.
	outDir := filepath.Join(dir, "out")
	cpC4mToReal(c4mPath+":", outDir)

	// Verify all files exist with correct content.
	checks := map[string]string{
		"root.txt":      "root",
		"a/mid.txt":     "middle",
		"a/b/deep.txt":  "deep",
	}
	for relPath, wantContent := range checks {
		data, err := os.ReadFile(filepath.Join(outDir, relPath))
		if err != nil {
			t.Errorf("read %s: %v", relPath, err)
			continue
		}
		if string(data) != wantContent {
			t.Errorf("%s = %q, want %q", relPath, data, wantContent)
		}
	}
}

// --------------------------------------------------------------------------
// entriesForSubtree
// --------------------------------------------------------------------------

func TestEntriesForSubtree_All(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	entries := entriesForSubtree(m, "")
	if len(entries) != len(m.Entries) {
		t.Errorf("entriesForSubtree('') = %d entries, want %d", len(entries), len(m.Entries))
	}
}

func TestEntriesForSubtree_SingleFile(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	entries := entriesForSubtree(m, "hello.txt")
	if len(entries) != 1 {
		t.Fatalf("entriesForSubtree('hello.txt') = %d entries, want 1", len(entries))
	}
	if entries[0].entry.Name != "hello.txt" {
		t.Errorf("entry name = %q, want %q", entries[0].entry.Name, "hello.txt")
	}
}

func TestEntriesForSubtree_Directory(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	entries := entriesForSubtree(m, "sub/")
	// Should return children of sub/ (nested.txt), not sub/ itself.
	if len(entries) != 1 {
		t.Fatalf("entriesForSubtree('sub/') = %d entries, want 1", len(entries))
	}
	if entries[0].entry.Name != "nested.txt" {
		t.Errorf("entry name = %q, want %q", entries[0].entry.Name, "nested.txt")
	}
}

func TestEntriesForSubtree_NotFound(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	entries := entriesForSubtree(m, "nonexistent.txt")
	if len(entries) != 0 {
		t.Errorf("entriesForSubtree('nonexistent.txt') = %d entries, want 0", len(entries))
	}
}

// --------------------------------------------------------------------------
// findEntryByPath
// --------------------------------------------------------------------------

func TestFindEntryByPath_Integration(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	cases := []struct {
		path    string
		wantNil bool
		name    string
	}{
		{"hello.txt", false, "hello.txt"},
		{"sub/", false, "sub/"},
		{"sub/nested.txt", false, "nested.txt"},
		{"nonexistent", true, ""},
		{"", true, ""},
	}

	for _, tc := range cases {
		e, _ := findEntryByPath(m, tc.path)
		if tc.wantNil {
			if e != nil {
				t.Errorf("findEntryByPath(%q) = %v, want nil", tc.path, e.Name)
			}
			continue
		}
		if e == nil {
			t.Errorf("findEntryByPath(%q) = nil, want %q", tc.path, tc.name)
			continue
		}
		if e.Name != tc.name {
			t.Errorf("findEntryByPath(%q).Name = %q, want %q", tc.path, e.Name, tc.name)
		}
	}
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// findEntryByName searches entries by bare name (not full path).
func findEntryByName(m *c4m.Manifest, name string) *c4m.Entry {
	for _, e := range m.Entries {
		if e.Name == name {
			return e
		}
	}
	return nil
}

// findEntryByPath is a test helper that wraps m.GetEntry for compatibility
// with tests that used the old two-return-value function. The second return
// value (index) is no longer meaningful and is always -1.
func findEntryByPath(m *c4m.Manifest, subPath string) (*c4m.Entry, int) {
	if subPath == "" {
		return nil, -1
	}
	e := m.GetEntry(subPath)
	if e == nil {
		return nil, -1
	}
	return e, -1
}

// collectDescendants is a test helper that wraps m.Descendants for
// compatibility with tests that used the old index-based function.
func collectDescendants(m *c4m.Manifest, idx int) []*c4m.Entry {
	if idx < 0 || idx >= len(m.Entries) {
		return nil
	}
	return m.Descendants(m.Entries[idx])
}

// c4ID is a type alias for use in walkTreeStore callback.
type c4ID = c4.ID
