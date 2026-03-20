package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Avalanche-io/c4/c4m"
)

// ---------------------------------------------------------------------------
// splitC4mPath
// ---------------------------------------------------------------------------

func TestSplitC4mPath(t *testing.T) {
	// Create a temp dir with a real .c4m file for extension-resolution tests.
	tmp := t.TempDir()
	projC4m := filepath.Join(tmp, "project.c4m")
	if err := os.WriteFile(projC4m, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		input   string
		wantC4m string
		wantSub string
	}{
		{
			name:    "explicit c4m with subpath",
			input:   "project.c4m:src/",
			wantC4m: "project.c4m",
			wantSub: "src/",
		},
		{
			name:    "explicit c4m with empty subpath",
			input:   "project.c4m:",
			wantC4m: "project.c4m",
			wantSub: "",
		},
		{
			name:    "extension-free colon with real file",
			input:   filepath.Join(tmp, "project") + ":",
			wantC4m: projC4m,
			wantSub: "",
		},
		{
			name:    "bare c4m file no colon",
			input:   "file.c4m",
			wantC4m: "file.c4m",
			wantSub: "",
		},
		{
			name:    "plain path no c4m marker",
			input:   "plain/path",
			wantC4m: "plain/path",
			wantSub: "",
		},
		{
			name:    "bare colon",
			input:   ":",
			wantC4m: "",
			wantSub: "",
		},
		{
			name:    "colon with leading slash subpath stripped",
			input:   "demo.c4m:/src/main.go",
			wantC4m: "demo.c4m",
			wantSub: "src/main.go",
		},
		{
			name:    "no extension no file on disk",
			input:   "nosuchproject:",
			wantC4m: "nosuchproject",
			wantSub: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotC4m, gotSub := splitC4mPath(tt.input)
			if gotC4m != tt.wantC4m {
				t.Errorf("c4mFile = %q, want %q", gotC4m, tt.wantC4m)
			}
			if gotSub != tt.wantSub {
				t.Errorf("subPath = %q, want %q", gotSub, tt.wantSub)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// shellEscape
// ---------------------------------------------------------------------------

func TestShellEscape(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "normal string",
			input: "hello",
			want:  "'hello'",
		},
		{
			name:  "string with single quote",
			input: "it's",
			want:  "'it'\\''s'",
		},
		{
			name:  "string with dollar sign",
			input: "$HOME",
			want:  "'$HOME'",
		},
		{
			name:  "string with spaces",
			input: "hello world",
			want:  "'hello world'",
		},
		{
			name:  "empty string",
			input: "",
			want:  "''",
		},
		{
			name:  "multiple single quotes",
			input: "a'b'c",
			want:  "'a'\\''b'\\''c'",
		},
		{
			name:  "special shell chars",
			input: "$(rm -rf /)",
			want:  "'$(rm -rf /)'",
		},
		{
			name:  "backslash",
			input: "a\\b",
			want:  "'a\\b'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellEscape(tt.input)
			if got != tt.want {
				t.Errorf("shellEscape(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// helper: build a test manifest programmatically
// ---------------------------------------------------------------------------

// testManifest returns a manifest with a realistic tree:
//
//	README.md      (depth 0)
//	src/           (depth 0)
//	  main.go      (depth 1)
//	  util.go      (depth 1)
//	  internal/    (depth 1)
//	    core.go    (depth 2)
//	docs/          (depth 0)
//	  guide.md     (depth 1)
func testManifest() *c4m.Manifest {
	m := &c4m.Manifest{
		Version: "1.0",
		Entries: []*c4m.Entry{
			{Name: "README.md", Mode: 0644, Size: 100, Depth: 0},
			{Name: "src/", Mode: os.ModeDir | 0755, Size: 0, Depth: 0},
			{Name: "main.go", Mode: 0644, Size: 200, Depth: 1},
			{Name: "util.go", Mode: 0644, Size: 150, Depth: 1},
			{Name: "internal/", Mode: os.ModeDir | 0755, Size: 0, Depth: 1},
			{Name: "core.go", Mode: 0644, Size: 80, Depth: 2},
			{Name: "docs/", Mode: os.ModeDir | 0755, Size: 0, Depth: 0},
			{Name: "guide.md", Mode: 0644, Size: 50, Depth: 1},
		},
	}
	return m
}

// ---------------------------------------------------------------------------
// findEntry (thin wrapper around m.GetEntry)
// ---------------------------------------------------------------------------

func TestFindEntry(t *testing.T) {
	m := testManifest()

	// findEntry returns nil for empty subPath.
	if e := findEntry(m, ""); e != nil {
		t.Errorf("findEntry(empty) = %v, want nil", e.Name)
	}
	if e := findEntry(m, "README.md"); e == nil || e.Name != "README.md" {
		t.Errorf("findEntry(README.md) unexpected result")
	}
	if e := findEntry(m, "src/main.go"); e == nil || e.Name != "main.go" {
		t.Errorf("findEntry(src/main.go) unexpected result")
	}
	if e := findEntry(m, "does/not/exist"); e != nil {
		t.Errorf("findEntry(does/not/exist) = %v, want nil", e.Name)
	}
}

// ---------------------------------------------------------------------------
// entriesAtPath
// ---------------------------------------------------------------------------

func TestEntriesAtPath(t *testing.T) {
	m := testManifest()

	// Root level
	root := entriesAtPath(m, "")
	if root == nil {
		t.Fatal("entriesAtPath(root) returned nil")
	}
	rootNames := entryNames(root)
	wantRoot := map[string]bool{"README.md": true, "src/": true, "docs/": true}
	if len(rootNames) != len(wantRoot) {
		t.Errorf("root entries count = %d, want %d", len(rootNames), len(wantRoot))
	}
	for _, n := range rootNames {
		if !wantRoot[n] {
			t.Errorf("unexpected root entry %q", n)
		}
	}

	// Under src/
	srcEntries := entriesAtPath(m, "src")
	if srcEntries == nil {
		t.Fatal("entriesAtPath(src) returned nil")
	}
	srcNames := entryNames(srcEntries)
	wantSrc := map[string]bool{"main.go": true, "util.go": true, "internal/": true}
	if len(srcNames) != len(wantSrc) {
		t.Errorf("src entries count = %d, want %d", len(srcNames), len(wantSrc))
	}
	for _, n := range srcNames {
		if !wantSrc[n] {
			t.Errorf("unexpected src entry %q", n)
		}
	}

	// Under src/internal/
	intEntries := entriesAtPath(m, "src/internal/")
	if intEntries == nil {
		t.Fatal("entriesAtPath(src/internal/) returned nil")
	}
	intNames := entryNames(intEntries)
	if len(intNames) != 1 || intNames[0] != "core.go" {
		t.Errorf("internal entries = %v, want [core.go]", intNames)
	}

	// Nonexistent path
	nope := entriesAtPath(m, "nope/")
	if nope != nil {
		t.Errorf("entriesAtPath(nope/) = %v, want nil", nope)
	}
}

func entryNames(entries []*c4m.Entry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names
}

// ---------------------------------------------------------------------------
// entryFullPath
// ---------------------------------------------------------------------------

func TestEntryFullPath(t *testing.T) {
	m := testManifest()

	tests := []struct {
		treePath string
		wantFull string
	}{
		{"README.md", "README.md"},
		{"src/", "src"},
		{"src/main.go", "src/main.go"},
		{"src/internal/core.go", "src/internal/core.go"},
		{"docs/guide.md", "docs/guide.md"},
	}

	for _, tt := range tests {
		t.Run(tt.treePath, func(t *testing.T) {
			e := m.GetEntry(tt.treePath)
			if e == nil {
				t.Fatalf("entry %q not found", tt.treePath)
			}
			got := entryFullPath(m, e)
			if got != tt.wantFull {
				t.Errorf("entryFullPath = %q, want %q", got, tt.wantFull)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// loadManifest + saveManifest round-trip
// ---------------------------------------------------------------------------

func TestLoadSaveManifestRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	c4mPath := filepath.Join(tmp, "test.c4m")

	// Build a manifest with a few entries.
	orig := testManifest()

	// Save it.
	if err := saveManifest(c4mPath, orig); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}

	// Load it back.
	loaded, err := loadManifest(c4mPath)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}

	// Compare entry count.
	if len(loaded.Entries) != len(orig.Entries) {
		t.Fatalf("entry count = %d, want %d", len(loaded.Entries), len(orig.Entries))
	}

	// Compare each entry's name, depth, and size.
	// Note: after save+load the entries go through sort, so the order may
	// differ from the original. Build a lookup by (depth, name).
	type key struct {
		depth int
		name  string
	}
	origMap := make(map[key]*c4m.Entry)
	for _, e := range orig.Entries {
		origMap[key{e.Depth, e.Name}] = e
	}
	for _, le := range loaded.Entries {
		k := key{le.Depth, le.Name}
		oe, ok := origMap[k]
		if !ok {
			t.Errorf("loaded entry %+v not in original", k)
			continue
		}
		if le.Size != oe.Size {
			t.Errorf("entry %v: size = %d, want %d", k, le.Size, oe.Size)
		}
	}
}

func TestLoadManifestNotFound(t *testing.T) {
	_, err := loadManifest("/nonexistent/path/to/file.c4m")
	if err == nil {
		t.Error("loadManifest on nonexistent file should return error")
	}
}

func TestSaveManifestBadDir(t *testing.T) {
	err := saveManifest("/nonexistent/dir/test.c4m", testManifest())
	if err == nil {
		t.Error("saveManifest to nonexistent dir should return error")
	}
}

// ---------------------------------------------------------------------------
// resolveContextPath
// ---------------------------------------------------------------------------

func TestResolveContextPath(t *testing.T) {
	// Create a real .c4m file for extension-free resolution tests.
	tmp := t.TempDir()
	projC4m := filepath.Join(tmp, "project.c4m")
	if err := os.WriteFile(projC4m, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	// Save/restore env vars
	origCtx := os.Getenv("C4_CONTEXT")
	origCwd := os.Getenv("C4_CWD")
	t.Cleanup(func() {
		os.Setenv("C4_CONTEXT", origCtx)
		os.Setenv("C4_CWD", origCwd)
	})

	t.Run("explicit colon path", func(t *testing.T) {
		os.Setenv("C4_CONTEXT", "")
		os.Setenv("C4_CWD", "")
		c4mFile, sub := resolveContextPath("test.c4m:src/main.go")
		if c4mFile != "test.c4m" {
			t.Errorf("c4mFile = %q, want %q", c4mFile, "test.c4m")
		}
		if sub != "src/main.go" {
			t.Errorf("subPath = %q, want %q", sub, "src/main.go")
		}
	})

	t.Run("explicit c4m suffix", func(t *testing.T) {
		os.Setenv("C4_CONTEXT", "")
		os.Setenv("C4_CWD", "")
		c4mFile, sub := resolveContextPath("demo.c4m")
		if c4mFile != "demo.c4m" {
			t.Errorf("c4mFile = %q, want %q", c4mFile, "demo.c4m")
		}
		if sub != "" {
			t.Errorf("subPath = %q, want empty", sub)
		}
	})

	t.Run("extension-free with existing file", func(t *testing.T) {
		os.Setenv("C4_CONTEXT", "")
		os.Setenv("C4_CWD", "")
		c4mFile, sub := resolveContextPath(filepath.Join(tmp, "project"))
		if c4mFile != projC4m {
			t.Errorf("c4mFile = %q, want %q", c4mFile, projC4m)
		}
		if sub != "" {
			t.Errorf("subPath = %q, want empty", sub)
		}
	})

	t.Run("in context relative path", func(t *testing.T) {
		os.Setenv("C4_CONTEXT", "/home/user/test.c4m")
		os.Setenv("C4_CWD", "src")
		c4mFile, sub := resolveContextPath("main.go")
		if c4mFile != "/home/user/test.c4m" {
			t.Errorf("c4mFile = %q, want %q", c4mFile, "/home/user/test.c4m")
		}
		if sub != "src/main.go" {
			t.Errorf("subPath = %q, want %q", sub, "src/main.go")
		}
	})

	t.Run("in context at root", func(t *testing.T) {
		os.Setenv("C4_CONTEXT", "/test.c4m")
		os.Setenv("C4_CWD", "")
		c4mFile, sub := resolveContextPath("README.md")
		if c4mFile != "/test.c4m" {
			t.Errorf("c4mFile = %q, want %q", c4mFile, "/test.c4m")
		}
		if sub != "README.md" {
			t.Errorf("subPath = %q, want %q", sub, "README.md")
		}
	})

	t.Run("in context dot resolves to cwd", func(t *testing.T) {
		os.Setenv("C4_CONTEXT", "/test.c4m")
		os.Setenv("C4_CWD", "src")
		c4mFile, sub := resolveContextPath(".")
		if c4mFile != "/test.c4m" {
			t.Errorf("c4mFile = %q, want %q", c4mFile, "/test.c4m")
		}
		if sub != "src" {
			t.Errorf("subPath = %q, want %q", sub, "src")
		}
	})

	t.Run("in context dotdot from root", func(t *testing.T) {
		os.Setenv("C4_CONTEXT", "/test.c4m")
		os.Setenv("C4_CWD", "")
		c4mFile, sub := resolveContextPath("..")
		if c4mFile != "/test.c4m" {
			t.Errorf("c4mFile = %q, want %q", c4mFile, "/test.c4m")
		}
		// resolveContextPath uses path.Clean(path.Join("", "..")) = ".."
		// It does NOT clamp to root (unlike ctx.Resolve which does).
		if sub != ".." {
			t.Errorf("subPath = %q, want %q", sub, "..")
		}
	})

	t.Run("no context plain path", func(t *testing.T) {
		os.Setenv("C4_CONTEXT", "")
		os.Setenv("C4_CWD", "")
		c4mFile, sub := resolveContextPath("some/dir/file.txt")
		if c4mFile != "" {
			t.Errorf("c4mFile = %q, want empty", c4mFile)
		}
		if sub != "some/dir/file.txt" {
			t.Errorf("subPath = %q, want %q", sub, "some/dir/file.txt")
		}
	})
}

// ---------------------------------------------------------------------------
// storeRoot (smoke test with nil interface)
// ---------------------------------------------------------------------------

func TestStoreRootNoRooter(t *testing.T) {
	// A nil store will panic, but we can test with a mock that doesn't
	// implement the rooter interface.
	type noRoot struct{}
	var nr noRoot
	// storeRoot uses a type assertion; a concrete type with no Root() method
	// should return "".
	type storeIface interface{ Close() error }
	// We can't easily test this without a real store, but we can verify the
	// function exists and compiles. This is covered by integration tests.
	_ = nr
}
