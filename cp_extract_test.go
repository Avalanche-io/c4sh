package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Avalanche-io/c4/c4m"
	c4store "github.com/Avalanche-io/c4/store"
)

// Extracting a folder writes its contents whatever their number: a folder
// holding one file is still a folder, and an empty folder is still found.

// README L159: cp project.c4m:shots/010/ ./workspace/, where shots/010/
// holds one file and ./workspace/ does not exist yet.
func TestCpExtractsANestedOneFileFolder(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	c4store.NewTreeStore(storePath)
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	root := filepath.Join(dir, "project")
	buildEditTree(t, root)
	c4mPath := filepath.Join(dir, "project.c4m")
	captureTree(t, root, c4mPath)

	for _, existing := range []bool{false, true} {
		ws := filepath.Join(dir, "workspace")
		os.RemoveAll(ws)
		if existing {
			os.Mkdir(ws, 0755)
		}
		code := catchExit(func() { runCp([]string{c4mPath + ":shots/010/", ws + "/"}) })
		if code != -1 {
			t.Fatalf("workspace existing=%v: exit %d", existing, code)
		}
		got, err := os.ReadFile(filepath.Join(ws, "comp.nk"))
		if err != nil {
			t.Fatalf("workspace existing=%v: %v", existing, err)
		}
		if string(got) != "comp\n" {
			t.Errorf("workspace existing=%v: comp.nk = %q", existing, got)
		}
	}
}

func TestCpExtractsAnEmptyFolder(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	c4store.NewTreeStore(storePath)
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	root := filepath.Join(dir, "project")
	buildEditTree(t, root)
	c4mPath := filepath.Join(dir, "project.c4m")
	captureTree(t, root, c4mPath)

	out := filepath.Join(dir, "out")
	code := catchExit(func() { runCp([]string{c4mPath + ":scratch/", out + "/"}) })
	if code != -1 {
		t.Fatalf("exit %d", code)
	}
	ents, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Errorf("out/ has %d entries, want 0", len(ents))
	}
}

// A file whose content can't be written is a failure, not a warning.
func TestCpSingleFileFailureExitsNonzero(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_STORE", te.storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	other, _ := c4store.NewTreeStore(filepath.Join(te.dir, "other"))
	id, _ := other.Put(strings.NewReader("not in the configured store"))
	m := te.loadC4m(t)
	m.AddEntry(&c4m.Entry{Name: "missing.txt", Mode: 0644, Size: 27, Timestamp: c4m.NullTimestamp(), C4ID: id})
	m.SortEntries()
	saveManifest(te.c4mPath, m)

	code := catchExit(func() { runCp([]string{te.c4mPath + ":missing.txt", filepath.Join(te.dir, "missing.txt")}) })
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
}
