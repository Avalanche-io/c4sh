package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Avalanche-io/c4"
	"github.com/Avalanche-io/c4/c4m"
	c4store "github.com/Avalanche-io/c4/store"
)

// A folded range's ID list names its frames as a listing names a folder's
// children. When the list is only inline in the c4m (not a store object),
// a copy that drops it drops the frames' C4 IDs. Through 1.0.26 both of
// cp's copies dropped it: into another c4m, and the sub-manifest an
// extraction hands to reconcile.

// rangeC4m captures a tree holding a three-frame range with its ID list
// inline only, and returns the c4m path and the range entry's C4 ID.
func rangeC4m(t *testing.T) (dir, c4mPath string, rangeID c4.ID, list string) {
	t.Helper()
	dir = t.TempDir()
	storePath := filepath.Join(dir, "store")
	store, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)
	c4mPath = filepath.Join(dir, "seq.c4m")
	captureRange(t, store, filepath.Join(dir, "tree"), c4mPath, true)
	m, err := loadManifest(c4mPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range m.Entries {
		if e.IsSequence {
			rangeID = e.C4ID
		}
	}
	list, ok := m.RangeData[rangeID]
	if rangeID.IsNil() || !ok {
		t.Fatal("the captured c4m holds no range with an inline list")
	}
	if store.Has(rangeID) {
		t.Fatal("the list is in the store; the test needs it inline only")
	}
	return dir, c4mPath, rangeID, list
}

func TestCpIntoAnotherC4mKeepsARangesInlineList(t *testing.T) {
	dir, src, rangeID, list := rangeC4m(t)
	for _, dst := range []string{"whole.c4m:", "nested.c4m:sub/", "folder.c4m:shot/"} {
		srcArg := src + ":"
		if dst == "folder.c4m:shot/" {
			srcArg = src + ":shot/"
		}
		dstPath := filepath.Join(dir, dst)
		if code := catchExit(func() { runCp([]string{srcArg, dstPath}) }); code != -1 {
			t.Fatalf("cp %s %s: exit %d", srcArg, dst, code)
		}
		c4mFile, _ := splitC4mPath(dstPath)
		got, err := loadManifest(c4mFile)
		if err != nil {
			t.Fatal(err)
		}
		if got.RangeData[rangeID] != list {
			t.Errorf("cp %s %s: the range's inline ID list was dropped", srcArg, dst)
		}
		b, _ := os.ReadFile(c4mFile)
		if !containsLine(string(b), list) {
			t.Errorf("cp %s %s: the written c4m has no ID list line:\n%s", srcArg, dst, b)
		}
	}
}

func TestCpExtractionHandsReconcileTheRangesList(t *testing.T) {
	_, src, rangeID, list := rangeC4m(t)
	m, err := loadManifest(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"", "shot/"} {
		subM := cpSubManifest(m, entriesForSubtree(m, sub))
		if subM.RangeData[rangeID] != list {
			t.Errorf("extracting %q: the sub-manifest has no ID list for the range", sub)
		}
		var seq *c4m.Entry
		for _, e := range subM.Entries {
			if e.IsSequence {
				seq = e
			}
		}
		if seq == nil {
			t.Fatalf("extracting %q: the sub-manifest has no range entry", sub)
		}
	}
}

func containsLine(text, line string) bool {
	start := 0
	for i := 0; i <= len(text); i++ {
		if i == len(text) || text[i] == '\n' {
			if text[start:i] == line {
				return true
			}
			start = i + 1
		}
	}
	return false
}
