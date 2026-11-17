package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	c4store "github.com/Avalanche-io/c4/store"
)

// A copy the reconciler couldn't complete is a failure: cp names what it
// couldn't write and exits 1. Through 1.0.26 it printed the errors and
// exited 0. The failure here is an unwritable destination, so nothing
// depends on links.

// runCpCapturing runs c4sh cp and returns its exit code (-1 for none) and
// its combined stdout and stderr.
func runCpCapturing(t *testing.T, args ...string) (int, string) {
	t.Helper()
	withTestExit(t)
	r, w, _ := os.Pipe()
	origErr, origOut := os.Stderr, os.Stdout
	os.Stderr, os.Stdout = w, w
	code := catchExit(func() { runCp(args) })
	w.Close()
	os.Stderr, os.Stdout = origErr, origOut
	out, _ := io.ReadAll(r)
	return code, string(out)
}

// readOnlyDir makes an empty folder nothing can be written into.
func readOnlyDir(t *testing.T, path string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root writes into read-only folders")
	}
	os.MkdirAll(path, 0755)
	if err := os.Chmod(path, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0755) })
}

func TestCpExtractExitsOneWhenAWriteFails(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	c4store.NewTreeStore(storePath)
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")

	root := filepath.Join(dir, "project")
	buildEditTree(t, root)
	c4mPath := filepath.Join(dir, "p.c4m")
	captureTree(t, root, c4mPath)

	dest := filepath.Join(dir, "out")
	readOnlyDir(t, dest)
	code, out := runCpCapturing(t, c4mPath+":", dest+"/")
	if code != 1 {
		t.Errorf("cp: exit %d, want 1:\n%s", code, out)
	}
	if !strings.Contains(out, "c4sh: cp: ") || !strings.Contains(out, dest) {
		t.Errorf("cp output %q names no path under %s", out, dest)
	}
}

func TestCpMultiDestinationExitsOneWhenAWriteFails(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	c4store.NewTreeStore(storePath)
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")

	root := filepath.Join(dir, "project")
	buildEditTree(t, root)
	dest := filepath.Join(dir, "copy")
	readOnlyDir(t, dest)
	code, out := runCpCapturing(t, root+"/", filepath.Join(dir, "x.c4m")+":", dest+"/")
	if code != 1 {
		t.Errorf("cp: exit %d, want 1:\n%s", code, out)
	}
	if !strings.Contains(out, "c4sh: cp: ") || !strings.Contains(out, dest) {
		t.Errorf("cp output %q names no path under %s", out, dest)
	}
}
