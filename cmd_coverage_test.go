package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Avalanche-io/c4/c4m"
	c4store "github.com/Avalanche-io/c4/store"

	"github.com/Avalanche-io/c4sh/internal/ctx"
)

// ===========================================================================
// mvEntry
// ===========================================================================

func TestMvEntry_RenameFile(t *testing.T) {
	m := buildSimpleManifest()

	if err := mvEntry(m, "hello.txt", "greetings.txt"); err != nil {
		t.Fatalf("mvEntry: %v", err)
	}

	if e, _ := findEntryByPath(m, "hello.txt"); e != nil {
		t.Error("hello.txt should not exist after rename")
	}
	if e, _ := findEntryByPath(m, "greetings.txt"); e == nil {
		t.Error("greetings.txt should exist after rename")
	}
}

func TestMvEntry_MoveFileIntoDir(t *testing.T) {
	m := buildSimpleManifest()

	if err := mvEntry(m, "hello.txt", "sub/"); err != nil {
		t.Fatalf("mvEntry: %v", err)
	}

	if e, _ := findEntryByPath(m, "hello.txt"); e != nil {
		t.Error("hello.txt should not exist at root after move")
	}
	e, _ := findEntryByPath(m, "sub/hello.txt")
	if e == nil {
		t.Fatal("sub/hello.txt should exist after move into sub/")
	}
	if e.Depth != 1 {
		t.Errorf("moved entry depth = %d, want 1", e.Depth)
	}
}

func TestMvEntry_MoveDirDescendantsUpdateDepth(t *testing.T) {
	// Create manifest: a/ -> child.txt at depth 1, then move a/ under sub/
	m := c4m.NewManifest()
	m.AddEntry(&c4m.Entry{
		Name: "a/", Depth: 0, Mode: os.ModeDir | 0755, Size: -1,
		Timestamp: c4m.NullTimestamp(),
	})
	m.AddEntry(&c4m.Entry{
		Name: "child.txt", Depth: 1, Mode: 0644, Size: 10,
		Timestamp: c4m.NullTimestamp(),
	})
	m.AddEntry(&c4m.Entry{
		Name: "sub/", Depth: 0, Mode: os.ModeDir | 0755, Size: -1,
		Timestamp: c4m.NullTimestamp(),
	})
	m.SortEntries()

	if err := mvEntry(m, "a/", "sub/"); err != nil {
		t.Fatalf("mvEntry dir: %v", err)
	}

	aEntry, _ := findEntryByPath(m, "sub/a/")
	if aEntry == nil {
		t.Fatal("sub/a/ should exist after move")
	}
	if aEntry.Depth != 1 {
		t.Errorf("a/ depth = %d, want 1", aEntry.Depth)
	}

	childEntry, _ := findEntryByPath(m, "sub/a/child.txt")
	if childEntry == nil {
		t.Fatal("sub/a/child.txt should exist after move")
	}
	if childEntry.Depth != 2 {
		t.Errorf("child.txt depth = %d, want 2", childEntry.Depth)
	}
}

func TestMvEntry_MoveToNonexistentParent(t *testing.T) {
	m := buildSimpleManifest()

	err := mvEntry(m, "hello.txt", "nonexistent/renamed.txt")
	if err == nil {
		t.Fatal("expected error moving to nonexistent parent")
	}
	if !strings.Contains(err.Error(), "parent directory does not exist") {
		t.Errorf("error = %q, want parent directory message", err)
	}
}

func TestMvEntry_SourceNotFound(t *testing.T) {
	m := buildSimpleManifest()

	err := mvEntry(m, "nosuchfile.txt", "renamed.txt")
	if err == nil {
		t.Fatal("expected error for missing source")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want 'not found'", err)
	}
}

func TestMvEntry_DestAlreadyExists(t *testing.T) {
	m := buildSimpleManifest()

	// Try to move hello.txt to a name that already exists
	// First add another file
	m.AddEntry(&c4m.Entry{
		Name: "other.txt", Depth: 0, Mode: 0644, Size: 5,
		Timestamp: c4m.NullTimestamp(),
	})
	m.SortEntries()

	err := mvEntry(m, "hello.txt", "other.txt")
	if err == nil {
		t.Fatal("expected error when destination already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want 'already exists'", err)
	}
}

func TestMvEntry_MoveToRoot(t *testing.T) {
	m := buildSimpleManifest()

	err := mvEntry(m, "hello.txt", "")
	if err == nil {
		t.Fatal("expected error moving to c4m root")
	}
	if !strings.Contains(err.Error(), "cannot move to c4m root") {
		t.Errorf("error = %q, want 'cannot move to c4m root'", err)
	}
}

func TestMvEntry_RenameDir(t *testing.T) {
	m := buildSimpleManifest()

	if err := mvEntry(m, "sub/", "renamed/"); err != nil {
		t.Fatalf("mvEntry rename dir: %v", err)
	}

	if e, _ := findEntryByPath(m, "sub/"); e != nil {
		t.Error("sub/ should not exist after rename")
	}
	if e, _ := findEntryByPath(m, "renamed/"); e == nil {
		t.Error("renamed/ should exist after rename")
	}
	// Descendants should be under renamed/
	if e, _ := findEntryByPath(m, "renamed/nested.txt"); e == nil {
		t.Error("renamed/nested.txt should exist after dir rename")
	}
}

// ===========================================================================
// rmEntries
// ===========================================================================

func TestRmEntries_RemoveFile(t *testing.T) {
	m := buildSimpleManifest()
	modified, errs := rmEntries(m, []string{"hello.txt"}, false, false)
	if !modified {
		t.Error("expected modified=true")
	}
	if errs != 0 {
		t.Errorf("errs = %d, want 0", errs)
	}
	if e, _ := findEntryByPath(m, "hello.txt"); e != nil {
		t.Error("hello.txt should not exist after rm")
	}
	// sub/ and nested.txt should remain
	if e, _ := findEntryByPath(m, "sub/"); e == nil {
		t.Error("sub/ should still exist")
	}
}

func TestRmEntries_RemoveDirRecursive(t *testing.T) {
	m := buildSimpleManifest()
	modified, errs := rmEntries(m, []string{"sub/"}, true, false)
	if !modified {
		t.Error("expected modified=true")
	}
	if errs != 0 {
		t.Errorf("errs = %d, want 0", errs)
	}
	if e, _ := findEntryByPath(m, "sub/"); e != nil {
		t.Error("sub/ should not exist after rm -r")
	}
	if e, _ := findEntryByPath(m, "sub/nested.txt"); e != nil {
		t.Error("sub/nested.txt should not exist after rm -r sub/")
	}
	// hello.txt should remain
	if e, _ := findEntryByPath(m, "hello.txt"); e == nil {
		t.Error("hello.txt should still exist")
	}
}

func TestRmEntries_DirWithoutRecursive(t *testing.T) {
	m := buildSimpleManifest()
	modified, errs := rmEntries(m, []string{"sub/"}, false, false)
	if modified {
		t.Error("expected modified=false (should fail)")
	}
	if errs != 1 {
		t.Errorf("errs = %d, want 1", errs)
	}
	// sub/ should still exist
	if e, _ := findEntryByPath(m, "sub/"); e == nil {
		t.Error("sub/ should still exist after failed rm")
	}
}

func TestRmEntries_ForceMissing(t *testing.T) {
	m := buildSimpleManifest()
	modified, errs := rmEntries(m, []string{"nosuchfile.txt"}, false, true)
	if modified {
		t.Error("expected modified=false for missing entry with -f")
	}
	if errs != 0 {
		t.Errorf("errs = %d, want 0 (force should suppress)", errs)
	}
}

func TestRmEntries_MissingWithoutForce(t *testing.T) {
	m := buildSimpleManifest()
	modified, errs := rmEntries(m, []string{"nosuchfile.txt"}, false, false)
	if modified {
		t.Error("expected modified=false for missing entry")
	}
	if errs != 1 {
		t.Errorf("errs = %d, want 1", errs)
	}
}

func TestRmEntries_RemoveLastEntry(t *testing.T) {
	m := c4m.NewManifest()
	m.AddEntry(&c4m.Entry{
		Name: "only.txt", Depth: 0, Mode: 0644, Size: 5,
		Timestamp: c4m.NullTimestamp(),
	})
	m.SortEntries()

	modified, errs := rmEntries(m, []string{"only.txt"}, false, false)
	if !modified {
		t.Error("expected modified=true")
	}
	if errs != 0 {
		t.Errorf("errs = %d, want 0", errs)
	}
	if len(m.Entries) != 0 {
		t.Errorf("entries remaining = %d, want 0", len(m.Entries))
	}
}

func TestRmEntries_MultipleFiles(t *testing.T) {
	m := buildSimpleManifest()
	// Add another file
	m.AddEntry(&c4m.Entry{
		Name: "extra.txt", Depth: 0, Mode: 0644, Size: 5,
		Timestamp: c4m.NullTimestamp(),
	})
	m.SortEntries()

	modified, errs := rmEntries(m, []string{"hello.txt", "extra.txt"}, false, false)
	if !modified {
		t.Error("expected modified=true")
	}
	if errs != 0 {
		t.Errorf("errs = %d, want 0", errs)
	}
	if e, _ := findEntryByPath(m, "hello.txt"); e != nil {
		t.Error("hello.txt should be removed")
	}
	if e, _ := findEntryByPath(m, "extra.txt"); e != nil {
		t.Error("extra.txt should be removed")
	}
}

func TestRmEntries_DirWithoutSlash(t *testing.T) {
	// Test that rm finds directory even without trailing slash
	m := buildSimpleManifest()
	modified, errs := rmEntries(m, []string{"sub"}, true, false)
	if !modified {
		t.Error("expected modified=true")
	}
	if errs != 0 {
		t.Errorf("errs = %d, want 0", errs)
	}
	if e, _ := findEntryByPath(m, "sub/"); e != nil {
		t.Error("sub/ should not exist after rm -r sub")
	}
}

// ===========================================================================
// mkdirOne / mkdirAll
// ===========================================================================

func TestMkdirOne_AtRoot(t *testing.T) {
	m := buildSimpleManifest()
	errs := 0
	modified := mkdirOne(m, "newdir", &errs)
	if !modified {
		t.Error("expected modified=true")
	}
	if errs != 0 {
		t.Errorf("errs = %d, want 0", errs)
	}
	m.SortEntries()
	e, _ := findEntryByPath(m, "newdir/")
	if e == nil {
		t.Fatal("newdir/ should exist")
	}
	if e.Depth != 0 {
		t.Errorf("depth = %d, want 0", e.Depth)
	}
}

func TestMkdirOne_Nested(t *testing.T) {
	m := buildSimpleManifest()
	errs := 0
	modified := mkdirOne(m, "sub/child", &errs)
	if !modified {
		t.Error("expected modified=true")
	}
	if errs != 0 {
		t.Errorf("errs = %d, want 0", errs)
	}
	m.SortEntries()
	e, _ := findEntryByPath(m, "sub/child/")
	if e == nil {
		t.Fatal("sub/child/ should exist")
	}
	if e.Depth != 1 {
		t.Errorf("depth = %d, want 1", e.Depth)
	}
}

func TestMkdirOne_AlreadyExists(t *testing.T) {
	m := buildSimpleManifest()
	errs := 0
	modified := mkdirOne(m, "sub", &errs)
	if modified {
		t.Error("expected modified=false for existing dir")
	}
	if errs != 1 {
		t.Errorf("errs = %d, want 1", errs)
	}
}

func TestMkdirOne_MissingParent(t *testing.T) {
	m := buildSimpleManifest()
	errs := 0
	modified := mkdirOne(m, "nonexistent/child", &errs)
	if modified {
		t.Error("expected modified=false for missing parent")
	}
	if errs != 1 {
		t.Errorf("errs = %d, want 1", errs)
	}
}

func TestMkdirAll_CreateParents(t *testing.T) {
	m := buildSimpleManifest()
	modified := mkdirAll(m, "x/y/z")
	if !modified {
		t.Error("expected modified=true")
	}
	m.SortEntries()

	for _, tc := range []struct {
		path  string
		depth int
	}{
		{"x/", 0},
		{"x/y/", 1},
		{"x/y/z/", 2},
	} {
		e, _ := findEntryByPath(m, tc.path)
		if e == nil {
			t.Errorf("%s should exist", tc.path)
			continue
		}
		if e.Depth != tc.depth {
			t.Errorf("%s depth = %d, want %d", tc.path, e.Depth, tc.depth)
		}
	}
}

func TestMkdirAll_PartiallyExisting(t *testing.T) {
	m := buildSimpleManifest()

	// sub/ already exists; create sub/deep
	modified := mkdirAll(m, "sub/deep")
	if !modified {
		t.Error("expected modified=true")
	}
	m.SortEntries()

	e, _ := findEntryByPath(m, "sub/deep/")
	if e == nil {
		t.Fatal("sub/deep/ should exist")
	}
	if e.Depth != 1 {
		t.Errorf("depth = %d, want 1", e.Depth)
	}
}

func TestMkdirAll_AllExist(t *testing.T) {
	m := buildSimpleManifest()

	// sub/ already exists
	modified := mkdirAll(m, "sub")
	if modified {
		t.Error("expected modified=false when all exist")
	}
}

// ===========================================================================
// splitDirName
// ===========================================================================

func TestSplitDirName(t *testing.T) {
	tests := []struct {
		input      string
		wantParent string
		wantName   string
	}{
		{"foo", "", "foo"},
		{"a/b", "a", "b"},
		{"a/b/c", "a/b", "c"},
		{"x/y/z/w", "x/y/z", "w"},
	}
	for _, tt := range tests {
		parent, name := splitDirName(tt.input)
		if parent != tt.wantParent || name != tt.wantName {
			t.Errorf("splitDirName(%q) = (%q, %q), want (%q, %q)",
				tt.input, parent, name, tt.wantParent, tt.wantName)
		}
	}
}

// ===========================================================================
// enterContextCmds
// ===========================================================================

func TestEnterContextCmds_BasicC4m(t *testing.T) {
	te := newTestEnv(t)

	cmds, err := enterContextCmds(te.c4mPath, "")
	if err != nil {
		t.Fatalf("enterContextCmds: %v", err)
	}
	if !strings.Contains(cmds, "export C4_CONTEXT=") {
		t.Error("should contain C4_CONTEXT export")
	}
	if !strings.Contains(cmds, "export C4_CWD=") {
		t.Error("should contain C4_CWD export")
	}
	// CWD should be empty for root
	if !strings.Contains(cmds, `C4_CWD=""`) {
		t.Errorf("CWD should be empty for root; cmds = %q", cmds)
	}
}

func TestEnterContextCmds_WithSubpath(t *testing.T) {
	te := newTestEnv(t)

	cmds, err := enterContextCmds(te.c4mPath, "sub")
	if err != nil {
		t.Fatalf("enterContextCmds: %v", err)
	}
	if !strings.Contains(cmds, "sub/") {
		t.Errorf("should contain sub/ subpath; cmds = %q", cmds)
	}
}

func TestEnterContextCmds_NonexistentC4m(t *testing.T) {
	_, err := enterContextCmds("/nonexistent/path.c4m", "")
	if err == nil {
		t.Fatal("expected error for nonexistent c4m")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want 'not found'", err)
	}
}

func TestEnterContextCmds_NonexistentSubpath(t *testing.T) {
	te := newTestEnv(t)

	_, err := enterContextCmds(te.c4mPath, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent subpath")
	}
	if !strings.Contains(err.Error(), "no such directory") {
		t.Errorf("error = %q, want 'no such directory'", err)
	}
}

// ===========================================================================
// navigateWithinCmds
// ===========================================================================

func TestNavigateWithinCmds_ToSubdir(t *testing.T) {
	te := newTestEnv(t)
	cur := &ctx.Context{C4mPath: te.c4mPath, CWD: ""}

	cmds, err := navigateWithinCmds(cur, "sub")
	if err != nil {
		t.Fatalf("navigateWithinCmds: %v", err)
	}
	if !strings.Contains(cmds, "C4_CWD=") {
		t.Error("should contain C4_CWD update")
	}
	if !strings.Contains(cmds, "sub/") {
		t.Errorf("should set CWD to sub/; cmds = %q", cmds)
	}
}

func TestNavigateWithinCmds_DotDotToRoot(t *testing.T) {
	te := newTestEnv(t)
	cur := &ctx.Context{C4mPath: te.c4mPath, CWD: "sub/"}

	cmds, err := navigateWithinCmds(cur, "..")
	if err != nil {
		t.Fatalf("navigateWithinCmds: %v", err)
	}
	if !strings.Contains(cmds, `C4_CWD=""`) {
		t.Errorf("should set CWD to empty (root); cmds = %q", cmds)
	}
}

func TestNavigateWithinCmds_Nonexistent(t *testing.T) {
	te := newTestEnv(t)
	cur := &ctx.Context{C4mPath: te.c4mPath, CWD: ""}

	_, err := navigateWithinCmds(cur, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent dir")
	}
	if !strings.Contains(err.Error(), "no such directory") {
		t.Errorf("error = %q, want 'no such directory'", err)
	}
}

func TestNavigateWithinCmds_AbsPath(t *testing.T) {
	te := newTestEnv(t)
	cur := &ctx.Context{C4mPath: te.c4mPath, CWD: "sub/"}

	cmds, err := navigateWithinCmds(cur, "/tmp")
	if err != nil {
		t.Fatalf("navigateWithinCmds abs: %v", err)
	}
	if !strings.Contains(cmds, "unset C4_CONTEXT C4_CWD") {
		t.Error("abs path should unset context")
	}
	if !strings.Contains(cmds, "builtin cd") {
		t.Error("abs path should issue builtin cd")
	}
}

// ===========================================================================
// exitContextCmds
// ===========================================================================

func TestExitContextCmds_NoRealPath(t *testing.T) {
	cmds := exitContextCmds("")
	if !strings.Contains(cmds, "unset C4_CONTEXT C4_CWD") {
		t.Errorf("should unset context vars; cmds = %q", cmds)
	}
	if strings.Contains(cmds, "builtin cd") {
		t.Error("should not have cd when no realPath")
	}
}

func TestExitContextCmds_WithRealPath(t *testing.T) {
	cmds := exitContextCmds("/home/user")
	if !strings.Contains(cmds, "unset C4_CONTEXT C4_CWD") {
		t.Error("should unset context vars")
	}
	if !strings.Contains(cmds, `builtin cd "/home/user"`) {
		t.Errorf("should cd to realPath; cmds = %q", cmds)
	}
}

// ===========================================================================
// pool: poolManifest
// ===========================================================================

func TestPoolManifest_AllPresent(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	outDir := filepath.Join(te.dir, "pool_out")
	res, err := poolManifest(m, te.store, te.c4mPath, outDir)
	if err != nil {
		t.Fatalf("poolManifest: %v", err)
	}
	if res.Missing != 0 {
		t.Errorf("missing = %d, want 0", res.Missing)
	}
	if res.Copied != 2 { // hello.txt + nested.txt
		t.Errorf("copied = %d, want 2", res.Copied)
	}

	// Verify pool store
	poolStore, err := c4store.NewTreeStore(filepath.Join(outDir, "store"))
	if err != nil {
		t.Fatalf("open pool store: %v", err)
	}
	for _, e := range m.Entries {
		if e.C4ID.IsNil() {
			continue
		}
		if !poolStore.Has(e.C4ID) {
			t.Errorf("pool store missing %s", e.C4ID)
		}
	}

	// Verify extract.sh exists
	if _, err := os.Stat(filepath.Join(outDir, "extract.sh")); err != nil {
		t.Error("extract.sh should exist")
	}
	// Verify c4m copy exists
	if _, err := os.Stat(filepath.Join(outDir, "test.c4m")); err != nil {
		t.Error("c4m copy should exist in pool")
	}
}

func TestPoolManifest_MissingObjects(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	// Use an empty store as source
	emptyStorePath := filepath.Join(te.dir, "emptystore")
	emptyStore, err := c4store.NewTreeStore(emptyStorePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}

	outDir := filepath.Join(te.dir, "pool_missing")
	res, err := poolManifest(m, emptyStore, te.c4mPath, outDir)
	if err != nil {
		t.Fatalf("poolManifest: %v", err)
	}
	if res.Missing != 2 {
		t.Errorf("missing = %d, want 2", res.Missing)
	}
	if res.Copied != 0 {
		t.Errorf("copied = %d, want 0", res.Copied)
	}
}

func TestPoolManifest_EmptyManifest(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	store, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}

	m := c4m.NewManifest()
	c4mPath := filepath.Join(dir, "empty.c4m")
	if err := saveManifest(c4mPath, m); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}

	outDir := filepath.Join(dir, "pool_empty")
	res, err := poolManifest(m, store, c4mPath, outDir)
	if err != nil {
		t.Fatalf("poolManifest: %v", err)
	}
	if res.Copied != 0 || res.Missing != 0 || res.Skipped != 0 {
		t.Errorf("expected all zeros, got copied=%d missing=%d skipped=%d",
			res.Copied, res.Missing, res.Skipped)
	}
}

func TestPoolManifest_ExtractScriptShellEscape(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	store, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}

	id, err := store.Put(strings.NewReader("tricky content"))
	if err != nil {
		t.Fatalf("store.Put: %v", err)
	}

	m := c4m.NewManifest()
	m.AddEntry(&c4m.Entry{
		Name: "file'name.txt", Depth: 0, Mode: 0644, Size: 14,
		Timestamp: c4m.NullTimestamp(), C4ID: id,
	})
	m.SortEntries()

	c4mPath := filepath.Join(dir, "tricky.c4m")
	if err := saveManifest(c4mPath, m); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}

	outDir := filepath.Join(dir, "pool_tricky")
	_, err = poolManifest(m, store, c4mPath, outDir)
	if err != nil {
		t.Fatalf("poolManifest: %v", err)
	}

	// Read extract.sh and verify the tricky name is properly escaped
	data, err := os.ReadFile(filepath.Join(outDir, "extract.sh"))
	if err != nil {
		t.Fatalf("read extract.sh: %v", err)
	}
	script := string(data)
	// The single quote in "file'name.txt" should be escaped as '\''
	if !strings.Contains(script, `'\''`) {
		t.Errorf("extract.sh should contain escaped single quote; script:\n%s", script)
	}
}

// ===========================================================================
// pool: buildPath
// ===========================================================================

func TestBuildPath_Nested(t *testing.T) {
	tests := []struct {
		name     string
		stack    []string
		entry    *c4m.Entry
		wantPath string
	}{
		{
			"root file",
			nil,
			&c4m.Entry{Name: "file.txt", Depth: 0},
			"file.txt",
		},
		{
			"nested file",
			[]string{"src"},
			&c4m.Entry{Name: "main.go", Depth: 1},
			"src/main.go",
		},
		{
			"deep nested",
			[]string{"src", "internal"},
			&c4m.Entry{Name: "core.go", Depth: 2},
			"src/internal/core.go",
		},
		{
			"root dir",
			nil,
			&c4m.Entry{Name: "docs/", Depth: 0},
			"docs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPath(tt.stack, tt.entry)
			if got != tt.wantPath {
				t.Errorf("buildPath = %q, want %q", got, tt.wantPath)
			}
		})
	}
}

// ===========================================================================
// pool: entryName
// ===========================================================================

func TestEntryName(t *testing.T) {
	m := buildSimpleManifest()
	e, _ := findEntryByPath(m, "hello.txt")
	if e == nil {
		t.Fatal("hello.txt not found")
	}
	name := entryName(m, e)
	if name != "hello.txt" {
		t.Errorf("entryName = %q, want %q", name, "hello.txt")
	}
}

// ===========================================================================
// pool: copyFile
// ===========================================================================

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "subdir", "dst.txt")

	content := "file content for copy test"
	if err := os.WriteFile(src, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != content {
		t.Errorf("content = %q, want %q", data, content)
	}
}

func TestCopyFile_SourceNotFound(t *testing.T) {
	dir := t.TempDir()
	err := copyFile(filepath.Join(dir, "nope"), filepath.Join(dir, "dst"))
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

// ===========================================================================
// ingest: walkTreeStore
// ===========================================================================

func TestWalkTreeStore(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	store, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}

	// Store some objects
	id1, err := store.Put(strings.NewReader("content one"))
	if err != nil {
		t.Fatalf("store.Put: %v", err)
	}
	id2, err := store.Put(strings.NewReader("content two"))
	if err != nil {
		t.Fatalf("store.Put: %v", err)
	}

	var foundIDs []string
	walkTreeStore(storePath, func(id c4ID) {
		foundIDs = append(foundIDs, id.String())
	})

	if len(foundIDs) != 2 {
		t.Fatalf("found %d IDs, want 2", len(foundIDs))
	}

	idSet := map[string]bool{id1.String(): true, id2.String(): true}
	for _, id := range foundIDs {
		if !idSet[id] {
			t.Errorf("unexpected ID: %s", id)
		}
	}
}

func TestWalkTreeStore_EmptyStore(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	_, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}

	var count int
	walkTreeStore(storePath, func(id c4ID) {
		count++
	})
	if count != 0 {
		t.Errorf("found %d IDs in empty store, want 0", count)
	}
}

// ===========================================================================
// rsync: isRemotePath
// ===========================================================================

func TestIsRemotePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"ssh style", "host:/path", true},
		{"user@host", "user@host:/path", true},
		{"c4m internal", "project.c4m:subdir/", false},
		{"bare path", "/usr/local/bin", false},
		{"no colon", "justpath", false},
		{"windows drive C", "C:\\Users", false},
		{"windows drive D", "D:\\data", false},
		{"colon only", ":", true},
		{"hostname no path", "server:", true},
		{"multi-word host", "my-server:/data", true},
		{"IP address", "192.168.1.1:/data", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRemotePath(tt.path)
			if got != tt.want {
				t.Errorf("isRemotePath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// ===========================================================================
// rsync: splitRemote
// ===========================================================================

func TestSplitRemote(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantHost  string
		wantPath  string
	}{
		{"standard", "host:/path", "host:", "/path"},
		{"user@host", "user@host:/path/to/file", "user@host:", "/path/to/file"},
		{"no colon", "justpath", "", "justpath"},
		{"empty path", "host:", "host:", ""},
		{"colon only", ":", ":", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, path := splitRemote(tt.input)
			if host != tt.wantHost || path != tt.wantPath {
				t.Errorf("splitRemote(%q) = (%q, %q), want (%q, %q)",
					tt.input, host, path, tt.wantHost, tt.wantPath)
			}
		})
	}
}

// ===========================================================================
// rsync: remoteStorePath
// ===========================================================================

func TestRemoteStorePath(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("C4_REMOTE_STORE", "")
		got := remoteStorePath("host:")
		if got != "host:.c4/store" {
			t.Errorf("remoteStorePath = %q, want %q", got, "host:.c4/store")
		}
	})

	t.Run("override", func(t *testing.T) {
		t.Setenv("C4_REMOTE_STORE", "/custom/store")
		got := remoteStorePath("host:")
		if got != "host:/custom/store" {
			t.Errorf("remoteStorePath = %q, want %q", got, "host:/custom/store")
		}
	})

	t.Run("user@host", func(t *testing.T) {
		t.Setenv("C4_REMOTE_STORE", "")
		got := remoteStorePath("user@host:")
		if got != "user@host:.c4/store" {
			t.Errorf("remoteStorePath = %q, want %q", got, "user@host:.c4/store")
		}
	})
}

// ===========================================================================
// rsync: ensureTrailingSlash
// ===========================================================================

func TestEnsureTrailingSlash(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/path", "/path/"},
		{"/path/", "/path/"},
		{"", "/"},
		{"host:.c4/store", "host:.c4/store/"},
		{"host:.c4/store/", "host:.c4/store/"},
	}

	for _, tt := range tests {
		got := ensureTrailingSlash(tt.input)
		if got != tt.want {
			t.Errorf("ensureTrailingSlash(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ===========================================================================
// shellinit: detectShell
// ===========================================================================

func TestDetectShell_Flags(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--bash"}, "bash"},
		{[]string{"--zsh"}, "zsh"},
		{[]string{"--foo", "--bash"}, "bash"},
		{[]string{"--zsh", "--bash"}, "zsh"}, // first wins
	}

	for _, tt := range tests {
		got := detectShell(tt.args)
		if got != tt.want {
			t.Errorf("detectShell(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestDetectShell_EnvVar(t *testing.T) {
	tests := []struct {
		shell string
		want  string
	}{
		{"/bin/bash", "bash"},
		{"/bin/zsh", "zsh"},
		{"/usr/local/bin/bash", "bash"},
		{"/usr/local/bin/zsh", "zsh"},
		{"/bin/fish", "fish"},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			t.Setenv("SHELL", tt.shell)
			got := detectShell(nil)
			if got != tt.want {
				t.Errorf("detectShell(nil) with SHELL=%q = %q, want %q",
					tt.shell, got, tt.want)
			}
		})
	}
}

// ===========================================================================
// shellinit: shellInitScript
// ===========================================================================

func TestShellInitScript_Bash(t *testing.T) {
	script, err := shellInitScript("bash")
	if err != nil {
		t.Fatalf("shellInitScript(bash): %v", err)
	}
	if script == "" {
		t.Fatal("bash script should not be empty")
	}
	// Verify it contains expected function names
	for _, fn := range []string{"function cd", "function ls", "function cat", "__c4sh_context", "__c4sh_complete"} {
		if !strings.Contains(script, fn) {
			t.Errorf("bash script missing %q", fn)
		}
	}
}

func TestShellInitScript_Zsh(t *testing.T) {
	script, err := shellInitScript("zsh")
	if err != nil {
		t.Fatalf("shellInitScript(zsh): %v", err)
	}
	if script == "" {
		t.Fatal("zsh script should not be empty")
	}
	for _, fn := range []string{"function cd", "function ls", "__c4sh_context", "__c4sh_complete", "compdef"} {
		if !strings.Contains(script, fn) {
			t.Errorf("zsh script missing %q", fn)
		}
	}
}

func TestShellInitScript_Unsupported(t *testing.T) {
	_, err := shellInitScript("fish")
	if err == nil {
		t.Fatal("expected error for unsupported shell")
	}
	if !strings.Contains(err.Error(), "unsupported shell") {
		t.Errorf("error = %q, want 'unsupported shell'", err)
	}
}

// ===========================================================================
// main.go: isC4mPath
// ===========================================================================

func TestIsC4mPath(t *testing.T) {
	// Create a temp file to test extension-free resolution
	dir := t.TempDir()
	c4mFile := filepath.Join(dir, "project.c4m")
	if err := os.WriteFile(c4mFile, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"colon path", "test.c4m:src/", true},
		{"c4m extension", "test.c4m", true},
		{"bare colon", "host:path", true},
		{"plain path no file", "somefile.txt", false},
		{"extension-free with file", filepath.Join(dir, "project"), true},
		{"extension-free no file", filepath.Join(dir, "nope"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isC4mPath(tt.path)
			if got != tt.want {
				t.Errorf("isC4mPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// ===========================================================================
// main.go: isInC4mContext
// ===========================================================================

func TestIsInC4mContext(t *testing.T) {
	t.Run("in context", func(t *testing.T) {
		t.Setenv("C4_CONTEXT", "/path/to/test.c4m")
		if !isInC4mContext() {
			t.Error("should return true when C4_CONTEXT is set")
		}
	})

	t.Run("not in context", func(t *testing.T) {
		t.Setenv("C4_CONTEXT", "")
		if isInC4mContext() {
			t.Error("should return false when C4_CONTEXT is empty")
		}
	})
}

// ===========================================================================
// writeExtractScript
// ===========================================================================

func TestWriteExtractScript_DirStructure(t *testing.T) {
	dir := t.TempDir()
	m := buildSimpleManifest()

	if err := writeExtractScript(dir, "test.c4m", m); err != nil {
		t.Fatalf("writeExtractScript: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "extract.sh"))
	if err != nil {
		t.Fatalf("read extract.sh: %v", err)
	}
	script := string(data)

	// Should start with shebang
	if !strings.HasPrefix(script, "#!/bin/sh") {
		t.Error("extract.sh should start with #!/bin/sh")
	}

	// Should contain mkdir for sub/
	if !strings.Contains(script, "mkdir -p") {
		t.Error("extract.sh should contain mkdir -p commands")
	}

	// Should reference store
	if !strings.Contains(script, "STORE=") {
		t.Error("extract.sh should reference STORE")
	}
}

// ===========================================================================
// Ingest round-trip with walkTreeStore
// ===========================================================================

func TestIngestRoundTrip(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	// Pool first
	bundleDir := filepath.Join(te.dir, "bundle")
	_, err := poolManifest(m, te.store, te.c4mPath, bundleDir)
	if err != nil {
		t.Fatalf("poolManifest: %v", err)
	}

	// Create a new empty store
	newStorePath := filepath.Join(te.dir, "newstore")
	newStore, err := c4store.NewTreeStore(newStorePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}

	// Ingest using walkTreeStore
	poolStorePath := filepath.Join(bundleDir, "store")
	poolStore, err := c4store.NewTreeStore(poolStorePath)
	if err != nil {
		t.Fatalf("open pool store: %v", err)
	}

	walkTreeStore(poolStorePath, func(id c4ID) {
		if newStore.Has(id) {
			return
		}
		rc, err := poolStore.Open(id)
		if err != nil {
			t.Errorf("pool open %s: %v", id, err)
			return
		}
		defer rc.Close()
		if _, err := newStore.Put(rc); err != nil {
			t.Errorf("newStore put %s: %v", id, err)
		}
	})

	// Verify all content transferred
	for _, e := range m.Entries {
		if e.C4ID.IsNil() {
			continue
		}
		if !newStore.Has(e.C4ID) {
			t.Errorf("new store missing %s", e.C4ID)
		}
	}
}

// ===========================================================================
// Ingest: skip already-present objects
// ===========================================================================

func TestIngest_SkipExisting(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	bundleDir := filepath.Join(te.dir, "bundle")
	_, err := poolManifest(m, te.store, te.c4mPath, bundleDir)
	if err != nil {
		t.Fatalf("poolManifest: %v", err)
	}

	// Pre-populate the target store with one object
	newStorePath := filepath.Join(te.dir, "newstore")
	newStore, err := c4store.NewTreeStore(newStorePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}
	newStore.Put(strings.NewReader("hello world")) // same as hello.txt

	poolStorePath := filepath.Join(bundleDir, "store")
	poolStore, err := c4store.NewTreeStore(poolStorePath)
	if err != nil {
		t.Fatalf("open pool store: %v", err)
	}

	var copied, skipped int
	walkTreeStore(poolStorePath, func(id c4ID) {
		if newStore.Has(id) {
			skipped++
			return
		}
		rc, err := poolStore.Open(id)
		if err != nil {
			t.Errorf("pool open %s: %v", id, err)
			return
		}
		defer rc.Close()
		if _, err := newStore.Put(rc); err != nil {
			t.Errorf("put: %v", err)
		}
		copied++
	})

	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	if copied != 1 {
		t.Errorf("copied = %d, want 1", copied)
	}
}

// ===========================================================================
// main.go: findSystemCommand
// ===========================================================================

func TestFindSystemCommand(t *testing.T) {
	// ls should exist on any Unix system
	path, err := findSystemCommand("ls")
	if err != nil {
		t.Fatalf("findSystemCommand(ls): %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path for ls")
	}

	// nonexistent command
	_, err = findSystemCommand("c4sh_nonexistent_command_xyz")
	if err == nil {
		t.Error("expected error for nonexistent command")
	}
}

// ===========================================================================
// main.go: usage (smoke test)
// ===========================================================================

func TestUsage_DoesNotPanic(t *testing.T) {
	// Just verify usage() doesn't panic when called.
	// It writes to stderr which is fine.
	usage()
}

// ===========================================================================
// saveManifest error path
// ===========================================================================

func TestSaveManifest_SyncAndRename(t *testing.T) {
	dir := t.TempDir()
	c4mPath := filepath.Join(dir, "test.c4m")
	m := buildSimpleManifest()

	if err := saveManifest(c4mPath, m); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(c4mPath); err != nil {
		t.Fatalf("c4m file not created: %v", err)
	}

	// Load it back and verify
	loaded, err := loadManifest(c4mPath)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if len(loaded.Entries) != 3 {
		t.Errorf("entry count = %d, want 3", len(loaded.Entries))
	}
}

// ===========================================================================
// poolManifest: duplicate C4 IDs (skipped)
// ===========================================================================

func TestPoolManifest_DuplicateIDs(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	store, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}

	id, err := store.Put(strings.NewReader("shared content"))
	if err != nil {
		t.Fatalf("store.Put: %v", err)
	}

	// Two entries with the same C4 ID
	m := c4m.NewManifest()
	m.AddEntry(&c4m.Entry{
		Name: "copy1.txt", Depth: 0, Mode: 0644, Size: 14,
		Timestamp: c4m.NullTimestamp(), C4ID: id,
	})
	m.AddEntry(&c4m.Entry{
		Name: "copy2.txt", Depth: 0, Mode: 0644, Size: 14,
		Timestamp: c4m.NullTimestamp(), C4ID: id,
	})
	m.SortEntries()

	c4mPath := filepath.Join(dir, "dups.c4m")
	if err := saveManifest(c4mPath, m); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}

	outDir := filepath.Join(dir, "pool_dups")
	res, err := poolManifest(m, store, c4mPath, outDir)
	if err != nil {
		t.Fatalf("poolManifest: %v", err)
	}
	if res.Copied != 1 {
		t.Errorf("copied = %d, want 1 (dedup)", res.Copied)
	}
	if res.Missing != 0 {
		t.Errorf("missing = %d, want 0", res.Missing)
	}
}

// ===========================================================================
// poolManifest: nil C4 IDs skipped
// ===========================================================================

func TestPoolManifest_NilIDsSkipped(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	store, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}

	m := c4m.NewManifest()
	// Directory entry (no C4 ID)
	m.AddEntry(&c4m.Entry{
		Name: "dir/", Depth: 0, Mode: os.ModeDir | 0755, Size: -1,
		Timestamp: c4m.NullTimestamp(),
	})
	// File with nil C4 ID
	m.AddEntry(&c4m.Entry{
		Name: "empty.txt", Depth: 0, Mode: 0644, Size: 0,
		Timestamp: c4m.NullTimestamp(),
	})
	m.SortEntries()

	c4mPath := filepath.Join(dir, "nilids.c4m")
	if err := saveManifest(c4mPath, m); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}

	outDir := filepath.Join(dir, "pool_nilids")
	res, err := poolManifest(m, store, c4mPath, outDir)
	if err != nil {
		t.Fatalf("poolManifest: %v", err)
	}
	if res.Copied != 0 {
		t.Errorf("copied = %d, want 0", res.Copied)
	}
	if res.Missing != 0 {
		t.Errorf("missing = %d, want 0", res.Missing)
	}
}

// ===========================================================================
// poolManifest: pool then pool again (skipped objects)
// ===========================================================================

func TestPoolManifest_AlreadyInDst(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	outDir := filepath.Join(te.dir, "pool_twice")

	// Pool once
	_, err := poolManifest(m, te.store, te.c4mPath, outDir)
	if err != nil {
		t.Fatalf("first poolManifest: %v", err)
	}

	// Pool again into the same dir - objects should be skipped
	res2, err := poolManifest(m, te.store, te.c4mPath, outDir)
	if err != nil {
		t.Fatalf("second poolManifest: %v", err)
	}
	if res2.Skipped != 2 { // hello.txt + nested.txt
		t.Errorf("skipped = %d, want 2", res2.Skipped)
	}
	if res2.Copied != 0 {
		t.Errorf("copied = %d, want 0", res2.Copied)
	}
}

// ===========================================================================
// writeExtractScript: file with nil C4 ID
// ===========================================================================

func TestWriteExtractScript_NilIDFiles(t *testing.T) {
	dir := t.TempDir()
	m := c4m.NewManifest()
	m.AddEntry(&c4m.Entry{
		Name: "empty.txt", Depth: 0, Mode: 0644, Size: 0,
		Timestamp: c4m.NullTimestamp(),
		// C4ID is nil
	})
	m.SortEntries()

	if err := writeExtractScript(dir, "test.c4m", m); err != nil {
		t.Fatalf("writeExtractScript: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "extract.sh"))
	if err != nil {
		t.Fatalf("read extract.sh: %v", err)
	}
	// Script should not have a cp command for nil ID files
	if strings.Contains(string(data), "find \"$STORE\"") {
		t.Error("extract.sh should not try to find nil ID objects")
	}
}

// ===========================================================================
// copyFile: MkdirAll branch
// ===========================================================================

func TestCopyFile_DeeplyNested(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "a", "b", "c", "d", "dst.txt")

	if err := os.WriteFile(src, []byte("deeply nested"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "deeply nested" {
		t.Errorf("content = %q, want %q", data, "deeply nested")
	}
}

// ===========================================================================
// openStore
// ===========================================================================

func TestOpenStore_WithC4Store(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	_, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}

	t.Setenv("C4_STORE", storePath)
	s, err := openStore()
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	if s == nil {
		t.Fatal("openStore returned nil")
	}
}

func TestOpenStore_NoStore(t *testing.T) {
	t.Setenv("C4_STORE", "")
	t.Setenv("HOME", t.TempDir())
	_, err := openStore()
	if err == nil {
		t.Fatal("expected error when no store configured")
	}
}

// ===========================================================================
// Helpers
// ===========================================================================

// buildSimpleManifest creates a manifest with:
//
//	hello.txt (depth 0)
//	sub/      (depth 0)
//	  nested.txt (depth 1)
func buildSimpleManifest() *c4m.Manifest {
	m := c4m.NewManifest()
	m.AddEntry(&c4m.Entry{
		Name: "hello.txt", Depth: 0, Mode: 0644, Size: 11,
		Timestamp: c4m.NullTimestamp(),
	})
	m.AddEntry(&c4m.Entry{
		Name: "sub/", Depth: 0, Mode: os.ModeDir | 0755, Size: -1,
		Timestamp: c4m.NullTimestamp(),
	})
	m.AddEntry(&c4m.Entry{
		Name: "nested.txt", Depth: 1, Mode: 0644, Size: 14,
		Timestamp: c4m.NullTimestamp(),
	})
	m.SortEntries()
	return m
}
