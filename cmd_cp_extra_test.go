package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Avalanche-io/c4"
	"github.com/Avalanche-io/c4/c4m"
	c4store "github.com/Avalanche-io/c4/store"
)

// --------------------------------------------------------------------------
// expandHome
// --------------------------------------------------------------------------

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{"tilde prefix", "~/Documents", filepath.Join(home, "Documents")},
		{"normal path", "/usr/bin", "/usr/bin"},
		{"relative path", "foo/bar", "foo/bar"},
		{"empty", "", ""},
		{"just tilde slash", "~/", home},
		{"tilde only", "~", "~"}, // Only ~/... is expanded
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandHome(tt.path)
			if got != tt.want {
				t.Errorf("expandHome(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// storeRoot
// --------------------------------------------------------------------------

func TestStoreRoot_WithTreeStore(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	store, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}
	root := storeRoot(store)
	if root != storePath {
		t.Errorf("storeRoot = %q, want %q", root, storePath)
	}
}

func TestStoreRoot_WithNonRooter(t *testing.T) {
	// Create a mock store that doesn't implement rooter
	ms := &mockStoreNoRoot{}
	root := storeRoot(ms)
	if root != "" {
		t.Errorf("storeRoot non-rooter = %q, want empty", root)
	}
}

// mockStoreNoRoot implements c4store.Store but not the rooter interface.
type mockStoreNoRoot struct{}

func (m *mockStoreNoRoot) Put(r io.Reader) (c4.ID, error) { return c4.ID{}, nil }
func (m *mockStoreNoRoot) Open(id c4.ID) (io.ReadCloser, error) {
	return nil, os.ErrNotExist
}
func (m *mockStoreNoRoot) Has(id c4.ID) bool             { return false }
func (m *mockStoreNoRoot) Remove(id c4.ID) error         { return nil }
func (m *mockStoreNoRoot) Create(id c4.ID) (io.WriteCloser, error) {
	return nil, os.ErrNotExist
}

// --------------------------------------------------------------------------
// ensureParentDirs
// --------------------------------------------------------------------------

func TestEnsureParentDirs(t *testing.T) {
	t.Run("single level", func(t *testing.T) {
		m := c4m.NewManifest()
		ensureParentDirs(m, "src/")

		found := false
		for _, e := range m.Entries {
			if e.Name == "src/" && e.Depth == 0 {
				found = true
			}
		}
		if !found {
			t.Error("expected src/ at depth 0")
		}
	})

	t.Run("nested path", func(t *testing.T) {
		m := c4m.NewManifest()
		ensureParentDirs(m, "a/b/c/")

		wantDirs := map[string]int{"a/": 0, "b/": 1, "c/": 2}
		for _, e := range m.Entries {
			if wantDepth, ok := wantDirs[e.Name]; ok {
				if e.Depth != wantDepth {
					t.Errorf("%s depth = %d, want %d", e.Name, e.Depth, wantDepth)
				}
				delete(wantDirs, e.Name)
			}
		}
		if len(wantDirs) > 0 {
			t.Errorf("missing dirs: %v", wantDirs)
		}
	})

	t.Run("already existing parent", func(t *testing.T) {
		m := c4m.NewManifest()
		m.AddEntry(&c4m.Entry{
			Name:  "src/",
			Depth: 0,
			Mode:  os.ModeDir | 0755,
			Size:  -1,
		})
		m.SortEntries()

		countBefore := len(m.Entries)
		ensureParentDirs(m, "src/lib/")

		// Should add lib/ but not re-add src/
		if len(m.Entries) != countBefore+1 {
			t.Errorf("expected %d entries, got %d", countBefore+1, len(m.Entries))
		}
	})
}

// --------------------------------------------------------------------------
// doExtractEntry
// --------------------------------------------------------------------------

func TestDoExtractEntry_RegularFile(t *testing.T) {
	te := newTestEnv(t)

	m := te.loadC4m(t)
	e := findEntry(m, "hello.txt")
	if e == nil {
		t.Fatal("hello.txt not found")
	}

	dstPath := filepath.Join(te.dir, "out", "hello.txt")
	if err := doExtractEntry(te.store, e, dstPath); err != nil {
		t.Fatalf("doExtractEntry: %v", err)
	}

	data, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("content = %q, want %q", data, "hello world")
	}
}

func TestDoExtractEntry_EmptyFile(t *testing.T) {
	te := newTestEnv(t)

	e := &c4m.Entry{
		Name:      "empty.txt",
		Mode:      0644,
		Size:      0,
		Depth:     0,
		Timestamp: c4m.NullTimestamp(),
		// C4ID is nil
	}

	dstPath := filepath.Join(te.dir, "out", "empty.txt")
	if err := doExtractEntry(te.store, e, dstPath); err != nil {
		t.Fatalf("doExtractEntry empty: %v", err)
	}

	info, err := os.Stat(dstPath)
	if err != nil {
		t.Fatalf("stat empty: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("empty file size = %d, want 0", info.Size())
	}
}

func TestDoExtractEntry_WithPermissions(t *testing.T) {
	te := newTestEnv(t)

	m := te.loadC4m(t)
	e := findEntry(m, "hello.txt")
	if e == nil {
		t.Fatal("hello.txt not found")
	}
	e.Mode = 0755 // executable

	dstPath := filepath.Join(te.dir, "out", "executable.txt")
	if err := doExtractEntry(te.store, e, dstPath); err != nil {
		t.Fatalf("doExtractEntry: %v", err)
	}

	info, err := os.Stat(dstPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0100 == 0 {
		t.Error("expected executable bit to be set")
	}
}

func TestDoExtractEntry_MissingContent(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	store, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}

	// Store content in a separate store to get a valid non-nil C4 ID,
	// then use it in a store that doesn't have the content.
	otherStorePath := filepath.Join(dir, "other")
	otherStore, err := c4store.NewTreeStore(otherStorePath)
	if err != nil {
		t.Fatalf("NewTreeStore other: %v", err)
	}
	realID, err := otherStore.Put(strings.NewReader("content that won't be in main store"))
	if err != nil {
		t.Fatalf("store.Put: %v", err)
	}

	e := &c4m.Entry{
		Name:      "missing.txt",
		Mode:      0644,
		Size:      100,
		Depth:     0,
		Timestamp: c4m.NullTimestamp(),
		C4ID:      realID,
	}

	dstPath := filepath.Join(dir, "out", "missing.txt")
	err = doExtractEntry(store, e, dstPath)
	if err == nil {
		t.Fatal("expected error for missing content")
	}
	if !strings.Contains(err.Error(), "content not found") {
		t.Errorf("error = %q, want 'content not found'", err)
	}
}

func TestDoExtractEntry_CreateFails(t *testing.T) {
	te := newTestEnv(t)

	m := te.loadC4m(t)
	e := findEntry(m, "hello.txt")
	if e == nil {
		t.Fatal("hello.txt not found")
	}

	// Create a read-only directory so os.Create fails
	roDir := filepath.Join(te.dir, "readonly")
	os.MkdirAll(roDir, 0755)
	os.Chmod(roDir, 0555)
	t.Cleanup(func() { os.Chmod(roDir, 0755) })

	dstPath := filepath.Join(roDir, "file.txt")
	err := doExtractEntry(te.store, e, dstPath)
	if err == nil {
		t.Fatal("expected error when create fails")
	}
}

func TestDoExtractEntry_EmptyFileCreateFails(t *testing.T) {
	te := newTestEnv(t)

	e := &c4m.Entry{
		Name:      "empty.txt",
		Mode:      0644,
		Size:      0,
		Depth:     0,
		Timestamp: c4m.NullTimestamp(),
		// C4ID is nil
	}

	// Create a read-only directory so os.Create fails
	roDir := filepath.Join(te.dir, "readonly_empty")
	os.MkdirAll(roDir, 0755)
	os.Chmod(roDir, 0555)
	t.Cleanup(func() { os.Chmod(roDir, 0755) })

	dstPath := filepath.Join(roDir, "empty.txt")
	err := doExtractEntry(te.store, e, dstPath)
	if err == nil {
		t.Fatal("expected error when create fails for empty file")
	}
}

func TestDoExtractEntry_WithTimestamp(t *testing.T) {
	te := newTestEnv(t)

	m := te.loadC4m(t)
	e := findEntry(m, "hello.txt")
	if e == nil {
		t.Fatal("hello.txt not found")
	}
	ts := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	e.Timestamp = ts

	dstPath := filepath.Join(te.dir, "out", "timestamped.txt")
	if err := doExtractEntry(te.store, e, dstPath); err != nil {
		t.Fatalf("doExtractEntry: %v", err)
	}

	info, err := os.Stat(dstPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// Timestamp should be set
	if !info.ModTime().Equal(ts) {
		t.Errorf("modtime = %v, want %v", info.ModTime(), ts)
	}
}

func TestDoExtractEntry_CreatesParentDirs(t *testing.T) {
	te := newTestEnv(t)

	m := te.loadC4m(t)
	e := findEntry(m, "hello.txt")
	if e == nil {
		t.Fatal("hello.txt not found")
	}

	// Put it in a deeply nested path
	dstPath := filepath.Join(te.dir, "deep", "nested", "path", "hello.txt")
	if err := doExtractEntry(te.store, e, dstPath); err != nil {
		t.Fatalf("doExtractEntry: %v", err)
	}

	if _, err := os.Stat(dstPath); err != nil {
		t.Errorf("file should exist at deep path: %v", err)
	}
}

// --------------------------------------------------------------------------
// extractEntry (wrapper that prints to stderr)
// --------------------------------------------------------------------------

func TestExtractEntry_Success(t *testing.T) {
	te := newTestEnv(t)

	m := te.loadC4m(t)
	e := findEntry(m, "hello.txt")
	if e == nil {
		t.Fatal("hello.txt not found")
	}

	dstPath := filepath.Join(te.dir, "extract_wrapper", "hello.txt")
	// Should not panic, should extract successfully
	extractEntry(te.store, e, dstPath)

	data, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("content = %q, want %q", data, "hello world")
	}
}

func TestExtractEntry_Error(t *testing.T) {
	te := newTestEnv(t)

	// Use a real C4 ID not in this store
	otherStorePath := filepath.Join(te.dir, "other")
	otherStore, err := c4store.NewTreeStore(otherStorePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}
	realID, err := otherStore.Put(strings.NewReader("not in te.store"))
	if err != nil {
		t.Fatalf("store.Put: %v", err)
	}

	e := &c4m.Entry{
		Name:      "missing.txt",
		Mode:      0644,
		Size:      15,
		Depth:     0,
		Timestamp: c4m.NullTimestamp(),
		C4ID:      realID,
	}

	dstPath := filepath.Join(te.dir, "extract_err", "missing.txt")
	// Should not panic, should print error to stderr
	extractEntry(te.store, e, dstPath)
}

// --------------------------------------------------------------------------
// storeFile
// --------------------------------------------------------------------------

func TestStoreFile(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	store, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}

	// Create a file to store
	srcFile := filepath.Join(dir, "input.txt")
	content := "test content for storeFile"
	os.WriteFile(srcFile, []byte(content), 0644)

	id, err := storeFile(store, srcFile)
	if err != nil {
		t.Fatalf("storeFile: %v", err)
	}
	if id.IsNil() {
		t.Fatal("storeFile returned nil ID")
	}

	// Verify content is in store
	if !store.Has(id) {
		t.Error("store should have the stored content")
	}

	// Verify reading back
	rc, err := store.Open(id)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != content {
		t.Errorf("stored content = %q, want %q", data, content)
	}
}

func TestStoreFile_NonexistentFile(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	store, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}

	_, err = storeFile(store, "/nonexistent/file.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}


// --------------------------------------------------------------------------
// cpMultiDest
// --------------------------------------------------------------------------

func TestCpMultiDest_RealAndC4m(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	_, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}
	t.Setenv("C4_STORE", storePath)

	// Create source directory
	srcDir := filepath.Join(dir, "src")
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0755)
	os.WriteFile(filepath.Join(srcDir, "root.txt"), []byte("root file"), 0644)
	os.WriteFile(filepath.Join(srcDir, "sub", "child.txt"), []byte("child file"), 0644)

	// Set up destinations: one real, one c4m
	realDest := filepath.Join(dir, "realdest")
	c4mDest := filepath.Join(dir, "dest.c4m")

	cpMultiDest(srcDir, []string{realDest, c4mDest + ":"})

	// Verify real destination
	data, err := os.ReadFile(filepath.Join(realDest, "root.txt"))
	if err != nil {
		t.Fatalf("read real dest root.txt: %v", err)
	}
	if string(data) != "root file" {
		t.Errorf("real root.txt = %q, want %q", data, "root file")
	}
	data, err = os.ReadFile(filepath.Join(realDest, "sub", "child.txt"))
	if err != nil {
		t.Fatalf("read real dest sub/child.txt: %v", err)
	}
	if string(data) != "child file" {
		t.Errorf("real child.txt = %q, want %q", data, "child file")
	}

	// Verify c4m destination
	m, err := loadManifest(c4mDest)
	if err != nil {
		t.Fatalf("loadManifest dest: %v", err)
	}
	paths := manifestPaths(m)
	wantPaths := map[string]bool{"root.txt": true, "sub": true, "sub/child.txt": true}
	for _, p := range paths {
		delete(wantPaths, p)
	}
	if len(wantPaths) > 0 {
		t.Errorf("missing paths in c4m: %v (got %v)", wantPaths, paths)
	}

	// Verify c4m entries have C4IDs for files
	for _, e := range m.Entries {
		if e.IsDir() {
			continue
		}
		if e.C4ID.IsNil() {
			t.Errorf("file entry %s has nil C4ID", e.Name)
		}
	}
}

func TestCpMultiDest_TwoRealDests(t *testing.T) {
	dir := t.TempDir()

	// Create source
	srcDir := filepath.Join(dir, "src")
	os.MkdirAll(srcDir, 0755)
	os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("content"), 0644)

	dest1 := filepath.Join(dir, "dest1")
	dest2 := filepath.Join(dir, "dest2")

	cpMultiDest(srcDir, []string{dest1, dest2})

	for _, d := range []string{dest1, dest2} {
		data, err := os.ReadFile(filepath.Join(d, "file.txt"))
		if err != nil {
			t.Errorf("read %s/file.txt: %v", d, err)
			continue
		}
		if string(data) != "content" {
			t.Errorf("%s/file.txt = %q, want %q", d, data, "content")
		}
	}
}

func TestCpMultiDest_TwoC4mDests(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	_, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}
	t.Setenv("C4_STORE", storePath)

	// Create source
	srcDir := filepath.Join(dir, "src")
	os.MkdirAll(srcDir, 0755)
	os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("data"), 0644)

	c4m1 := filepath.Join(dir, "a.c4m")
	c4m2 := filepath.Join(dir, "b.c4m")

	cpMultiDest(srcDir, []string{c4m1 + ":", c4m2 + ":"})

	// Both manifests should have the file
	for _, c4mPath := range []string{c4m1, c4m2} {
		m, err := loadManifest(c4mPath)
		if err != nil {
			t.Fatalf("loadManifest %s: %v", c4mPath, err)
		}
		found := false
		for _, e := range m.Entries {
			if e.Name == "data.txt" {
				found = true
				if e.C4ID.IsNil() {
					t.Errorf("%s: data.txt has nil C4ID", c4mPath)
				}
			}
		}
		if !found {
			t.Errorf("%s: data.txt not found", c4mPath)
		}
	}
}

func TestCpMultiDest_WithSubdirectory(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	_, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}
	t.Setenv("C4_STORE", storePath)

	// Create source with nested dirs
	srcDir := filepath.Join(dir, "src")
	os.MkdirAll(filepath.Join(srcDir, "a", "b"), 0755)
	os.WriteFile(filepath.Join(srcDir, "a", "file.txt"), []byte("in a"), 0644)
	os.WriteFile(filepath.Join(srcDir, "a", "b", "deep.txt"), []byte("deep"), 0644)

	c4mDest := filepath.Join(dir, "dest.c4m")
	cpMultiDest(srcDir, []string{c4mDest + ":"})

	m, loadErr := loadManifest(c4mDest)
	if loadErr != nil {
		t.Fatalf("loadManifest: %v", loadErr)
	}

	// Verify we have nested structure
	paths := manifestPaths(m)
	wantPaths := map[string]bool{"a": true, "a/file.txt": true, "a/b": true, "a/b/deep.txt": true}
	for _, p := range paths {
		delete(wantPaths, p)
	}
	if len(wantPaths) > 0 {
		t.Errorf("missing paths: %v (got %v)", wantPaths, paths)
	}
}


// --------------------------------------------------------------------------
// setFileMetadata
// --------------------------------------------------------------------------

func TestSetFileMetadata_ModeAndTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("x"), 0644)

	ts := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	e := &c4m.Entry{
		Mode:      0755,
		Timestamp: ts,
	}
	setFileMetadata(path, e)

	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0755 {
		t.Errorf("mode = %o, want 0755", info.Mode().Perm())
	}
	if !info.ModTime().Equal(ts) {
		t.Errorf("modtime = %v, want %v", info.ModTime(), ts)
	}
}

func TestSetFileMetadata_NullTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("x"), 0644)

	originalInfo, _ := os.Stat(path)
	originalMod := originalInfo.ModTime()

	e := &c4m.Entry{
		Mode:      0644,
		Timestamp: c4m.NullTimestamp(),
	}
	setFileMetadata(path, e)

	info, _ := os.Stat(path)
	// Timestamp should not change for null timestamp
	if !info.ModTime().Equal(originalMod) {
		t.Errorf("modtime changed for null timestamp")
	}
}

func TestSetFileMetadata_ZeroMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("x"), 0644)

	e := &c4m.Entry{
		Mode:      0, // zero mode should not be applied
		Timestamp: c4m.NullTimestamp(),
	}
	setFileMetadata(path, e)

	info, _ := os.Stat(path)
	// Mode should remain 0644
	if info.Mode().Perm() != 0644 {
		t.Errorf("mode should stay 0644 with zero mode entry, got %o", info.Mode().Perm())
	}
}


// --------------------------------------------------------------------------
// cpRealToC4m: capture into subdirectory
// --------------------------------------------------------------------------

func TestCpRealToC4m_IntoSubdir(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	_, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}
	t.Setenv("C4_STORE", storePath)

	// Create source file
	srcFile := filepath.Join(dir, "input.txt")
	os.WriteFile(srcFile, []byte("input content"), 0644)

	// Capture into c4m at a subpath
	c4mPath := filepath.Join(dir, "test.c4m")
	cpRealToC4m(srcFile, c4mPath+":assets/")

	m, err := loadManifest(c4mPath)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}

	// Should have assets/ dir and input.txt inside it
	assetsDir, _ := findEntryByPath(m, "assets/")
	if assetsDir == nil {
		t.Fatal("assets/ should exist")
	}
	e, _ := findEntryByPath(m, "assets/input.txt")
	if e == nil {
		t.Fatal("assets/input.txt should exist")
	}
}

// --------------------------------------------------------------------------
// cpC4mToReal: single file extraction
// --------------------------------------------------------------------------

func TestCpC4mToReal_SingleFile(t *testing.T) {
	te := newTestEnv(t)
	te.setStoreEnv(t)

	outDir := filepath.Join(te.dir, "outdir")
	os.MkdirAll(outDir, 0755)

	// Extract a single file (not a directory)
	cpC4mToReal(te.c4mPath+":hello.txt", outDir)

	// Should be placed inside outDir since outDir is an existing directory
	data, err := os.ReadFile(filepath.Join(outDir, "hello.txt"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("content = %q, want %q", data, "hello world")
	}
}

func TestCpC4mToReal_SingleFileToNewPath(t *testing.T) {
	te := newTestEnv(t)
	te.setStoreEnv(t)

	// Extract to a non-existing path (file destination)
	outPath := filepath.Join(te.dir, "renamed.txt")
	cpC4mToReal(te.c4mPath+":hello.txt", outPath)

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("content = %q, want %q", data, "hello world")
	}
}

// --------------------------------------------------------------------------
// cpC4mToC4m: copy into subdirectory
// --------------------------------------------------------------------------

func TestCpC4mToC4m_IntoSubdir(t *testing.T) {
	te := newTestEnv(t)

	dstC4m := filepath.Join(te.dir, "dest.c4m")

	// Copy into a subdirectory in dest
	cpC4mToC4m(te.c4mPath+":", dstC4m+":imported/")

	m, err := loadManifest(dstC4m)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}

	// Should have imported/ dir
	importDir, _ := findEntryByPath(m, "imported/")
	if importDir == nil {
		t.Fatal("imported/ should exist")
	}

	// And hello.txt under it
	e, _ := findEntryByPath(m, "imported/hello.txt")
	if e == nil {
		t.Fatal("imported/hello.txt should exist")
	}
}

func TestCpC4mToC4m_SameFileCopy(t *testing.T) {
	te := newTestEnv(t)

	// Copy sub/ content to a new path within the same c4m
	cpC4mToC4m(te.c4mPath+":sub/", te.c4mPath+":backup/")

	m := te.loadC4m(t)

	// backup/ should exist with nested.txt
	backupDir, _ := findEntryByPath(m, "backup/")
	if backupDir == nil {
		t.Fatal("backup/ should exist")
	}
	e, _ := findEntryByPath(m, "backup/nested.txt")
	if e == nil {
		t.Fatal("backup/nested.txt should exist")
	}
	// Original should still exist
	origE, _ := findEntryByPath(m, "sub/nested.txt")
	if origE == nil {
		t.Fatal("sub/nested.txt should still exist")
	}
}

// --------------------------------------------------------------------------
// entriesForSubtree: edge cases
// --------------------------------------------------------------------------

func TestEntriesForSubtree_DirectoryWithoutTrailingSlash(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	// "sub" without trailing slash should still find the directory
	entries := entriesForSubtree(m, "sub")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (nested.txt), got %d", len(entries))
	}
}

// --------------------------------------------------------------------------
// relative path computation (was reconstructRelPath, now inline in entriesForSubtree)
// --------------------------------------------------------------------------

func TestRelativePathComputation(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	// entriesForSubtree computes relative paths internally; verify via its output.
	entries := entriesForSubtree(m, "sub/")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].relPath != "nested.txt" {
		t.Errorf("relPath = %q, want %q", entries[0].relPath, "nested.txt")
	}
}

// --------------------------------------------------------------------------
// cpMultiDest: directory timestamps
// --------------------------------------------------------------------------

func TestCpMultiDest_DirTimestamps(t *testing.T) {
	dir := t.TempDir()

	// Create source with a subdirectory
	srcDir := filepath.Join(dir, "src")
	os.MkdirAll(filepath.Join(srcDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(srcDir, "subdir", "file.txt"), []byte("content"), 0644)

	ts := time.Date(2024, 3, 15, 10, 0, 0, 0, time.UTC)
	os.Chtimes(filepath.Join(srcDir, "subdir"), ts, ts)

	realDest := filepath.Join(dir, "dest")
	cpMultiDest(srcDir, []string{realDest})

	// Check that directory timestamp was preserved
	info, err := os.Stat(filepath.Join(realDest, "subdir"))
	if err != nil {
		t.Fatalf("stat dest subdir: %v", err)
	}
	if !info.ModTime().Equal(ts) {
		t.Errorf("dir modtime = %v, want %v", info.ModTime(), ts)
	}
}
