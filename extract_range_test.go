package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Avalanche-io/c4/c4m"
	"github.com/Avalanche-io/c4/scan"
	c4store "github.com/Avalanche-io/c4/store"
)

// A pool bundle's extract.sh writes a folded range as its frames: one file
// per frame, named from the range expression, with the content of that
// frame's C4 ID. Through 1.0.26 it copied the range's own C4 ID, which
// names the ID list and no frame, into one file named f.[0001-0003].exr.
//
// These tests check frame names and content. Modes and times are not
// restored by extract.sh for any file (it restores content only; the fix
// is deferred to the next patch release), so they are not checked here.

// runExtractSh pools c4mPath from the configured store and runs the
// bundle's extract.sh into out with only the system tools on PATH.
func runExtractSh(t *testing.T, dir, c4mPath, out string) (int, string) {
	t.Helper()
	if code, msg := runPoolCapturing(t, c4mPath, filepath.Join(dir, "bundle")); code != -1 {
		t.Fatalf("pool: exit %d:\n%s", code, msg)
	}
	cmd := exec.Command("sh", filepath.Join(dir, "bundle", "extract.sh"), out)
	cmd.Env = append(os.Environ(), "PATH=/usr/bin:/bin")
	b, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		code = scriptExit(err)
	}
	return code, string(b)
}

func TestExtractShWritesARangesFrames(t *testing.T) {
	for _, inline := range []bool{true, false} {
		t.Run(fmt.Sprintf("inline=%v", inline), func(t *testing.T) {
			dir := t.TempDir()
			storePath := filepath.Join(dir, "store")
			store, _ := c4store.NewTreeStore(storePath)
			t.Setenv("C4_STORE", storePath)
			t.Setenv("C4_CONTEXT", "")
			withTestExit(t)
			c4mPath := filepath.Join(dir, "seq.c4m")
			captureRange(t, store, filepath.Join(dir, "tree"), c4mPath, inline)

			out := filepath.Join(dir, "out")
			code, msg := runExtractSh(t, dir, c4mPath, out)
			if code != 0 {
				t.Fatalf("extract.sh: exit %d:\n%s", code, msg)
			}
			for i := 1; i <= 3; i++ {
				p := filepath.Join(out, "shot", fmt.Sprintf("f.%04d.exr", i))
				b, err := os.ReadFile(p)
				if err != nil {
					t.Errorf("frame %d not written: %v", i, err)
					continue
				}
				if want := fmt.Sprintf("frame %d\n", i); string(b) != want {
					t.Errorf("frame %d holds %q, want %q", i, b, want)
				}
			}
			if b, err := os.ReadFile(filepath.Join(out, "readme")); err != nil || string(b) != "note\n" {
				t.Errorf("readme: %q, %v", b, err)
			}
			matches, _ := filepath.Glob(filepath.Join(out, "shot", "*[[]*"))
			if len(matches) > 0 {
				t.Errorf("a file named after the range expression was written: %v", matches)
			}
		})
	}
}

// Without the range's ID list (not inline, not in the bundle) there are no
// frames to name: the script says so, writes the rest, and exits 1.
func TestExtractShNamesARangeWithoutItsList(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	store, _ := c4store.NewTreeStore(storePath)
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)
	c4mPath := filepath.Join(dir, "seq.c4m")
	captureRange(t, store, filepath.Join(dir, "tree"), c4mPath, false)
	m, _ := loadManifest(c4mPath)
	for _, e := range m.Entries {
		if e.IsSequence {
			if err := store.Remove(e.C4ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if code, msg := runPoolCapturing(t, c4mPath, filepath.Join(dir, "bundle")); code == -1 {
		t.Logf("pool exited 0 although the list is missing:\n%s", msg)
	}
	out := filepath.Join(dir, "out")
	cmd := exec.Command("sh", filepath.Join(dir, "bundle", "extract.sh"), out)
	cmd.Env = append(os.Environ(), "PATH=/usr/bin:/bin")
	b, err := cmd.CombinedOutput()
	if err == nil || scriptExit(err) != 1 {
		t.Fatalf("extract.sh: %v, want exit 1:\n%s", err, b)
	}
	if !strings.Contains(string(b), "no ID list for this range") {
		t.Errorf("the missing list isn't named:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(out, "readme")); err != nil {
		t.Errorf("the rest wasn't written: %v", err)
	}
}

// A range of symbolic links is refused with the reason, and the rest is
// written.
func TestExtractShRefusesARangeOfLinks(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	c4store.NewTreeStore(storePath)
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	root := filepath.Join(dir, "tree")
	os.MkdirAll(root, 0755)
	for i := 1; i <= 3; i++ {
		os.WriteFile(filepath.Join(root, fmt.Sprintf("real%d", i)), []byte(fmt.Sprintf("%d\n", i)), 0644)
		os.Symlink(fmt.Sprintf("real%d", i), filepath.Join(root, fmt.Sprintf("l.%04d.exr", i)))
	}
	m, err := scan.Dir(root, scan.WithMode(scan.ModeFull), scan.WithSequenceDetection(true))
	if err != nil {
		t.Fatal(err)
	}
	var linkRange *c4m.Entry
	for _, e := range m.Entries {
		if e.IsSequence && e.IsSymlink() {
			linkRange = e
		}
	}
	if linkRange == nil {
		t.Skip("this core folds no range of links")
	}
	c4mPath := filepath.Join(dir, "links.c4m")
	if err := saveManifest(c4mPath, m); err != nil {
		t.Fatal(err)
	}
	runPoolCapturing(t, c4mPath, filepath.Join(dir, "bundle"))
	out := filepath.Join(dir, "out")
	cmd := exec.Command("sh", filepath.Join(dir, "bundle", "extract.sh"), out)
	cmd.Env = append(os.Environ(), "PATH=/usr/bin:/bin")
	b, err := cmd.CombinedOutput()
	if err == nil || scriptExit(err) != 1 {
		t.Fatalf("extract.sh: %v, want exit 1:\n%s", err, b)
	}
	if !strings.Contains(string(b), "a range of symbolic links is not extracted") {
		t.Errorf("the refusal doesn't give its reason:\n%s", b)
	}
	if matches, _ := filepath.Glob(filepath.Join(out, "l.*")); len(matches) > 0 {
		t.Errorf("link range members were written: %v", matches)
	}
}

// scriptExit returns a finished command's exit status, or -1.
func scriptExit(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
