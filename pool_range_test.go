package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Avalanche-io/c4"
	"github.com/Avalanche-io/c4/c4m"
	"github.com/Avalanche-io/c4/scan"
	c4store "github.com/Avalanche-io/c4/store"
)

// A folded range names its frames through its ID list, and a bundle needs
// each frame's content exactly as it needs a file's: bundled when the
// store has it, counted and named when it doesn't. The list itself may be
// inline in the c4m or a store object addressed by the range's C4 ID.

// captureRange scans a tree holding shot/f.0001.exr..f.0003.exr and a file
// with the library's sequence detection, stores every file's content, and
// writes the c4m. With inline false the ID list is stored and left out of
// the c4m, as the c4 CLI writes it. It returns the frames' C4 IDs.
func captureRange(t *testing.T, store c4store.Store, root, c4mPath string, inline bool) []c4.ID {
	t.Helper()
	os.MkdirAll(filepath.Join(root, "shot"), 0755)
	var frames []c4.ID
	for i := 1; i <= 3; i++ {
		body := fmt.Sprintf("frame %d\n", i)
		os.WriteFile(filepath.Join(root, "shot", fmt.Sprintf("f.%04d.exr", i)), []byte(body), 0644)
		frames = append(frames, c4.Identify(strings.NewReader(body)))
	}
	os.WriteFile(filepath.Join(root, "readme"), []byte("note\n"), 0644)
	pinTimes(t, root)

	m, err := scan.Dir(root, scan.WithMode(scan.ModeFull), scan.WithSequenceDetection(true))
	if err != nil {
		t.Fatal(err)
	}
	var seq *c4m.Entry
	for _, e := range m.Entries {
		if e.IsSequence {
			seq = e
		}
	}
	if seq == nil {
		t.Fatal("the scan folded no range")
	}
	// The scanner folds the range but may not keep its ID list (c4sh
	// main's pinned core drops it); c4m.DetectSequences, which the scanner
	// folds with, returns it.
	unfolded, err := scan.Dir(root, scan.WithMode(scan.ModeFull))
	if err != nil {
		t.Fatal(err)
	}
	list, ok := c4m.DetectSequences(unfolded).RangeData[seq.C4ID]
	if !ok {
		t.Fatal("the library returned no ID list for the scanned range")
	}
	m.RangeData = map[c4.ID]string{seq.C4ID: list}
	if !inline {
		if _, err := store.Put(strings.NewReader(list)); err != nil {
			t.Fatal(err)
		}
		m.RangeData = nil
	}
	for _, p := range []string{"readme", "shot/f.0001.exr", "shot/f.0002.exr", "shot/f.0003.exr"} {
		f, err := os.Open(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.Put(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := saveManifest(c4mPath, m); err != nil {
		t.Fatal(err)
	}
	return frames
}

func TestPoolBundlesARangesFrames(t *testing.T) {
	for _, inline := range []bool{true, false} {
		name := "stored ID list"
		if inline {
			name = "inline ID list"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			storePath := filepath.Join(dir, "store")
			store, _ := c4store.NewTreeStore(storePath)
			t.Setenv("C4_STORE", storePath)
			t.Setenv("C4_CONTEXT", "")

			c4mPath := filepath.Join(dir, "seq.c4m")
			frames := captureRange(t, store, filepath.Join(dir, "project"), c4mPath, inline)

			bundle := filepath.Join(dir, "bundle")
			code, out := runPoolCapturing(t, c4mPath, bundle)
			if code != -1 {
				t.Fatalf("pool: exit %d, want 0:\n%s", code, out)
			}
			if !strings.Contains(out, "0 missing") {
				t.Errorf("pool output %q, want 0 missing", out)
			}
			b, _ := c4store.NewTreeStore(filepath.Join(bundle, "store"))
			for i, id := range frames {
				if !b.Has(id) {
					t.Errorf("frame %d (%s) is not in the bundle", i+1, id)
				}
			}
		})
	}
}

func TestPoolNamesAMissingFrameAndExits1(t *testing.T) {
	for _, inline := range []bool{true, false} {
		name := "stored ID list"
		if inline {
			name = "inline ID list"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			storePath := filepath.Join(dir, "store")
			store, _ := c4store.NewTreeStore(storePath)
			t.Setenv("C4_STORE", storePath)
			t.Setenv("C4_CONTEXT", "")

			c4mPath := filepath.Join(dir, "seq.c4m")
			frames := captureRange(t, store, filepath.Join(dir, "project"), c4mPath, inline)
			if err := store.Remove(frames[1]); err != nil {
				t.Fatal(err)
			}

			bundle := filepath.Join(dir, "bundle")
			code, out := runPoolCapturing(t, c4mPath, bundle)
			if code != 1 {
				t.Errorf("pool: exit %d, want 1:\n%s", code, out)
			}
			if !strings.Contains(out, "1 missing") {
				t.Errorf("pool output %q, want 1 missing", out)
			}
			if !strings.Contains(out, "missing shot/f.0002.exr "+frames[1].String()) {
				t.Errorf("pool output %q does not name shot/f.0002.exr and its C4 ID", out)
			}
			b, _ := c4store.NewTreeStore(filepath.Join(bundle, "store"))
			for _, i := range []int{0, 2} {
				if !b.Has(frames[i]) {
					t.Errorf("frame %d, which the store has, is not in the bundle", i+1)
				}
			}
		})
	}
}

// Without its ID list a range has no frames to name: the bundle counts the
// list missing, under the range's own path, and nothing else.
func TestPoolWithoutARangesIDListNamesOnlyTheList(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	store, _ := c4store.NewTreeStore(storePath)
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")

	c4mPath := filepath.Join(dir, "seq.c4m")
	captureRange(t, store, filepath.Join(dir, "project"), c4mPath, false)
	m, err := loadManifest(c4mPath)
	if err != nil {
		t.Fatal(err)
	}
	var seq *c4m.Entry
	for _, e := range m.Entries {
		if e.IsSequence {
			seq = e
		}
	}
	if err := store.Remove(seq.C4ID); err != nil {
		t.Fatal(err)
	}

	code, out := runPoolCapturing(t, c4mPath, filepath.Join(dir, "bundle"))
	if code != 1 {
		t.Errorf("pool: exit %d, want 1:\n%s", code, out)
	}
	want := "missing " + strings.TrimSuffix(m.EntryPath(seq), "/") + " " + seq.C4ID.String()
	if !strings.Contains(out, "1 missing") || !strings.Contains(out, want) {
		t.Errorf("pool output %q, want 1 missing, naming %q", out, want)
	}
}
