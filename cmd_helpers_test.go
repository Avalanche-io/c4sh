package main

import (
	"os"
	"testing"

	"github.com/Avalanche-io/c4/c4m"
)

// ---------------------------------------------------------------------------
// m.GetEntry (replaces findEntryByPath)
// ---------------------------------------------------------------------------

func TestGetEntrySynthetic(t *testing.T) {
	m := testManifest()

	tests := []struct {
		name     string
		subPath  string
		wantName string // empty means expect nil
	}{
		{"empty path", "", "", },
		{"root file", "README.md", "README.md"},
		{"root dir", "src/", "src/"},
		{"nested file main.go", "src/main.go", "main.go"},
		{"nested file util.go", "src/util.go", "util.go"},
		{"deep nested", "src/internal/core.go", "core.go"},
		{"nested dir", "src/internal/", "internal/"},
		{"docs dir", "docs/", "docs/"},
		{"docs file", "docs/guide.md", "guide.md"},
		{"nonexistent", "missing.txt", ""},
		{"wrong parent", "docs/main.go", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var entry *c4m.Entry
			if tt.subPath != "" {
				entry = m.GetEntry(tt.subPath)
			}
			if tt.wantName == "" {
				if entry != nil {
					t.Errorf("GetEntry(%q) = (%q), want nil", tt.subPath, entry.Name)
				}
				return
			}
			if entry == nil {
				t.Fatalf("GetEntry(%q) = nil, want %q", tt.subPath, tt.wantName)
			}
			if entry.Name != tt.wantName {
				t.Errorf("GetEntry(%q).Name = %q, want %q", tt.subPath, entry.Name, tt.wantName)
			}
		})
	}
}

func TestGetEntrySyntheticPointsToCorrectEntry(t *testing.T) {
	// Verify that the returned entry is actually in m.Entries.
	m := testManifest()

	for _, path := range []string{"README.md", "src/", "src/main.go", "docs/guide.md"} {
		entry := m.GetEntry(path)
		if entry == nil {
			t.Errorf("GetEntry(%q) = nil", path)
			continue
		}
		found := false
		for _, e := range m.Entries {
			if e == entry {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("GetEntry(%q) returned entry not in m.Entries", path)
		}
	}
}

// ---------------------------------------------------------------------------
// m.Descendants (replaces collectDescendants)
// ---------------------------------------------------------------------------

func TestDescendants(t *testing.T) {
	m := testManifest()

	t.Run("src directory", func(t *testing.T) {
		srcEntry := m.GetEntry("src/")
		if srcEntry == nil {
			t.Fatal("src/ not found")
		}
		desc := m.Descendants(srcEntry)
		// src/ contains: main.go, util.go, internal/, core.go = 4 descendants
		names := make([]string, len(desc))
		for i, e := range desc {
			names[i] = e.Name
		}
		wantNames := map[string]bool{
			"main.go":   true,
			"util.go":   true,
			"internal/": true,
			"core.go":   true,
		}
		if len(desc) != len(wantNames) {
			t.Fatalf("descendant count = %d, want %d; got %v", len(desc), len(wantNames), names)
		}
		for _, n := range names {
			if !wantNames[n] {
				t.Errorf("unexpected descendant %q", n)
			}
		}
	})

	t.Run("internal subdirectory", func(t *testing.T) {
		intEntry := m.GetEntry("src/internal/")
		if intEntry == nil {
			t.Fatal("src/internal/ not found")
		}
		desc := m.Descendants(intEntry)
		if len(desc) != 1 {
			t.Fatalf("descendant count = %d, want 1", len(desc))
		}
		if desc[0].Name != "core.go" {
			t.Errorf("descendant = %q, want core.go", desc[0].Name)
		}
	})

	t.Run("docs directory", func(t *testing.T) {
		docsEntry := m.GetEntry("docs/")
		if docsEntry == nil {
			t.Fatal("docs/ not found")
		}
		desc := m.Descendants(docsEntry)
		if len(desc) != 1 {
			t.Fatalf("descendant count = %d, want 1", len(desc))
		}
		if desc[0].Name != "guide.md" {
			t.Errorf("descendant = %q, want guide.md", desc[0].Name)
		}
	})

	t.Run("file has no descendants", func(t *testing.T) {
		readmeEntry := m.GetEntry("README.md")
		if readmeEntry == nil {
			t.Fatal("README.md not found")
		}
		desc := m.Descendants(readmeEntry)
		if len(desc) != 0 {
			t.Errorf("file descendant count = %d, want 0", len(desc))
		}
	})

	t.Run("nil entry", func(t *testing.T) {
		desc := m.Descendants(nil)
		if desc != nil {
			t.Errorf("nil entry descendants = %v, want nil", desc)
		}
	})
}

// ---------------------------------------------------------------------------
// Descendants with a flat manifest
// ---------------------------------------------------------------------------

func TestDescendantsFlat(t *testing.T) {
	// All entries at depth 0 — no entry has descendants.
	m := &c4m.Manifest{
		Version: "1.0",
		Entries: []*c4m.Entry{
			{Name: "a.txt", Mode: 0644, Size: 10, Depth: 0},
			{Name: "b.txt", Mode: 0644, Size: 20, Depth: 0},
			{Name: "c.txt", Mode: 0644, Size: 30, Depth: 0},
		},
	}
	for _, e := range m.Entries {
		desc := m.Descendants(e)
		if len(desc) != 0 {
			t.Errorf("entry %s: descendant count = %d, want 0", e.Name, len(desc))
		}
	}
}

// ---------------------------------------------------------------------------
// GetEntry with dir trailing-slash variants
// ---------------------------------------------------------------------------

func TestGetEntryDirSlash(t *testing.T) {
	m := testManifest()

	// Directory names in the manifest include trailing slash.
	// GetEntry should find "src/" when asked for "src/"
	e := m.GetEntry("src/")
	if e == nil {
		t.Fatal("GetEntry(src/) = nil")
	}
	if e.Name != "src/" {
		t.Errorf("Name = %q, want src/", e.Name)
	}

	// Without trailing slash, "src" won't match "src/" because the full
	// path for directory entries includes the trailing slash.
	e2 := m.GetEntry("src")
	if e2 != nil {
		t.Errorf("GetEntry(src) = %q, want nil (name is src/)", e2.Name)
	}
}

// ---------------------------------------------------------------------------
// Descendants depth values
// ---------------------------------------------------------------------------

func TestDescendantsDepthValues(t *testing.T) {
	m := testManifest()
	srcEntry := m.GetEntry("src/")
	if srcEntry == nil {
		t.Fatal("src/ not found")
	}

	desc := m.Descendants(srcEntry)
	for _, d := range desc {
		if d.Depth <= srcEntry.Depth {
			t.Errorf("descendant %q has depth %d <= parent depth %d", d.Name, d.Depth, srcEntry.Depth)
		}
	}
}

// ---------------------------------------------------------------------------
// Empty manifest edge cases
// ---------------------------------------------------------------------------

func TestGetEntryEmptyManifest(t *testing.T) {
	m := &c4m.Manifest{Version: "1.0", Entries: []*c4m.Entry{}}

	e := m.GetEntry("anything")
	if e != nil {
		t.Errorf("GetEntry on empty manifest = %v, want nil", e)
	}
}

func TestDescendantsEmptyManifest(t *testing.T) {
	m := &c4m.Manifest{Version: "1.0", Entries: []*c4m.Entry{}}
	// No entries to descend from — Descendants(nil) returns nil.
	desc := m.Descendants(nil)
	if desc != nil {
		t.Errorf("Descendants on empty manifest = %v, want nil", desc)
	}
}

// ---------------------------------------------------------------------------
// Deeply nested manifest
// ---------------------------------------------------------------------------

func TestGetEntryDeep(t *testing.T) {
	m := &c4m.Manifest{
		Version: "1.0",
		Entries: []*c4m.Entry{
			{Name: "a/", Mode: os.ModeDir | 0755, Depth: 0},
			{Name: "b/", Mode: os.ModeDir | 0755, Depth: 1},
			{Name: "c/", Mode: os.ModeDir | 0755, Depth: 2},
			{Name: "d/", Mode: os.ModeDir | 0755, Depth: 3},
			{Name: "leaf.txt", Mode: 0644, Depth: 4},
		},
	}

	e := m.GetEntry("a/b/c/d/leaf.txt")
	if e == nil {
		t.Fatal("GetEntry(a/b/c/d/leaf.txt) = nil")
	}
	if e.Name != "leaf.txt" {
		t.Errorf("Name = %q, want leaf.txt", e.Name)
	}

	aEntry := m.GetEntry("a/")
	if aEntry == nil {
		t.Fatal("a/ not found")
	}
	desc := m.Descendants(aEntry)
	if len(desc) != 4 {
		t.Errorf("descendants of a/ = %d, want 4", len(desc))
	}
}
