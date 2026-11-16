package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	c4store "github.com/Avalanche-io/c4/store"
)

// The extract.sh that c4sh pool writes must recreate the tree, root-level
// files included, in a folder named after the c4m (README L213-215), and
// must say so in its exit status when it couldn't.

// poolTree captures the README example project into dir/<name>, pools it
// into dir/bundle and returns the bundle path.
func poolTree(t *testing.T, dir, name string) string {
	t.Helper()
	storePath := filepath.Join(dir, "store")
	store, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")

	root := filepath.Join(dir, "project")
	buildEditTree(t, root)
	c4mPath := filepath.Join(dir, name)
	captureTree(t, root, c4mPath)
	m, err := loadManifest(c4mPath)
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(dir, "bundle")
	if _, err := poolManifest(m, store, c4mPath, bundle); err != nil {
		t.Fatal(err)
	}
	return bundle
}

// runExtract runs bundle/extract.sh from cwd with args and returns its
// exit code and output.
func runExtract(t *testing.T, bundle, cwd string, args ...string) (int, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("extract.sh needs a POSIX sh")
	}
	if err := os.MkdirAll(cwd, 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", append([]string{filepath.Join(bundle, "extract.sh")}, args...)...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), string(out)
	}
	if err != nil {
		t.Fatal(err)
	}
	return 0, string(out)
}

var extractedFiles = map[string]string{
	"README.md":                 "readme\n",
	"shots/010/comp.nk":         "comp\n",
	"shots/020/plate.exr":       "plate\n",
	"deliveries/v3/d.txt":       "d3\n",
	"deliveries/v3/notes/n.txt": "n\n",
}

func checkExtracted(t *testing.T, out string) {
	t.Helper()
	for p, body := range extractedFiles {
		got, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(p)))
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		if string(got) != body {
			t.Errorf("%s = %q, want %q", p, got, body)
		}
	}
	if fi, err := os.Stat(filepath.Join(out, "scratch")); err != nil || !fi.IsDir() {
		t.Errorf("scratch/ not created: %v", err)
	}
}

func TestExtractScriptDefaultFolderIsTheC4mName(t *testing.T) {
	for _, name := range []string{"project", "it's a test"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			bundle := poolTree(t, dir, name+".c4m")
			cwd := filepath.Join(dir, "ex")
			code, out := runExtract(t, bundle, cwd)
			if code != 0 {
				t.Fatalf("exit %d:\n%s", code, out)
			}
			ents, _ := os.ReadDir(cwd)
			if len(ents) != 1 || ents[0].Name() != name {
				var got []string
				for _, e := range ents {
					got = append(got, e.Name())
				}
				t.Fatalf("extracted into %q, want one folder %q", got, name)
			}
			checkExtracted(t, filepath.Join(cwd, name))
		})
	}
}

func TestExtractScriptIntoANewFolderGivenAsArgument(t *testing.T) {
	dir := t.TempDir()
	bundle := poolTree(t, dir, "project.c4m")
	cwd := filepath.Join(dir, "ex")
	code, out := runExtract(t, bundle, cwd, "out/nested")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	checkExtracted(t, filepath.Join(cwd, "out", "nested"))
}

func TestExtractScriptExitsNonzeroWhenAnObjectIsMissing(t *testing.T) {
	dir := t.TempDir()
	bundle := poolTree(t, dir, "project.c4m")
	// Remove README.md's object from the bundle's store.
	var removed bool
	filepath.Walk(filepath.Join(bundle, "store"), func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() && !removed {
			b, _ := os.ReadFile(p)
			if string(b) == "readme\n" {
				os.Remove(p)
				removed = true
			}
		}
		return nil
	})
	if !removed {
		t.Fatal("README.md's object not found in the bundle store")
	}
	code, out := runExtract(t, bundle, filepath.Join(dir, "ex"))
	if code == 0 {
		t.Fatalf("exit 0 with a missing object:\n%s", out)
	}
	// The rest of the tree is still written.
	if _, err := os.Stat(filepath.Join(dir, "ex", "project", "shots", "010", "comp.nk")); err != nil {
		t.Errorf("the rest of the tree was not written: %v", err)
	}
}

func TestExtractScriptExitsNonzeroWhenAWriteFails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes into a read-only folder")
	}
	dir := t.TempDir()
	bundle := poolTree(t, dir, "project.c4m")
	cwd := filepath.Join(dir, "ex")
	locked := filepath.Join(cwd, "out", "shots", "010")
	if err := os.MkdirAll(locked, 0755); err != nil {
		t.Fatal(err)
	}
	os.Chmod(locked, 0555)
	t.Cleanup(func() { os.Chmod(locked, 0755) })

	code, out := runExtract(t, bundle, cwd, "out")
	if code == 0 {
		t.Fatalf("exit 0 though comp.nk could not be written:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(cwd, "out", "README.md")); err != nil {
		t.Errorf("the rest of the tree was not written: %v", err)
	}
}
