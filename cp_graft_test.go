package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Avalanche-io/c4/c4m"
	c4store "github.com/Avalanche-io/c4/store"
)

// A copy into a c4m must give the c4m a scan of the same copy made on disk
// (cp -R, merging into folders that exist) gives: entries under the right
// folder, a file replacing a file, a folder merging into a folder, folder
// sizes and C4 IDs re-derived, and no folder dated by the clock. A folder
// the copy creates for a source folder takes that folder's time; one with
// no source has a null time (filled from its newest child, as a scan does).

// buildOtherTree writes a destination tree whose names overlap the README
// project: README.md and shots/ exist, and a/ sits before z/.
func buildOtherTree(t *testing.T, root string) {
	t.Helper()
	for p, body := range map[string]string{
		"README.md":        "other readme\n",
		"shots/090/x.txt":  "x\n",
		"a/keep.txt":       "keep\n",
		"z/last.txt":       "last\n",
		"z/deep/inner.txt": "inner\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	pinTimes(t, root)
}

// copyInto copies the file or the contents of the folder src into the
// folder dst on disk, creating dst and merging into folders that exist.
func copyInto(src, dst string) error {
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		if err := os.MkdirAll(dst, 0755); err != nil {
			return err
		}
		return copyFile(src, filepath.Join(dst, filepath.Base(src)))
	}
	return filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(out, 0755)
		}
		return copyFile(p, out)
	})
}

func TestCpIntoAC4mMatchesARescanOfTheSameCopy(t *testing.T) {
	cases := []struct {
		name     string
		real     bool   // capture from the real project instead of project.c4m
		src, dst string // paths inside the source and other.c4m
		nullRels []string
	}{
		{name: "into an existing folder (README L163)", src: "shots/", dst: "shots/"},
		{name: "into a new folder under a folder that isn't last", src: "deliveries/v3/", dst: "a/new/"},
		{name: "through a missing intermediate folder", src: "deliveries/v3/", dst: "a/b/new/", nullRels: []string{"a/b"}},
		{name: "the whole c4m into the root, replacing and merging", src: "", dst: ""},
		{name: "the whole c4m into a new folder", src: "", dst: "full/", nullRels: []string{"full"}},
		{name: "a file into an existing nested folder", src: "README.md", dst: "z/deep/"},
		{name: "an empty folder into a new folder", src: "scratch/", dst: "a/empty/"},
		{name: "capture into an existing folder", real: true, src: "deliveries/", dst: "a/"},
		{name: "capture into a new folder", real: true, src: "deliveries/", dst: "z/cap/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			storePath := filepath.Join(dir, "store")
			c4store.NewTreeStore(storePath)
			t.Setenv("C4_STORE", storePath)
			t.Setenv("C4_CONTEXT", "")
			withTestExit(t)

			project := filepath.Join(dir, "project")
			buildEditTree(t, project)
			other := filepath.Join(dir, "other")
			buildOtherTree(t, other)
			projectC4m := filepath.Join(dir, "project.c4m")
			otherC4m := filepath.Join(dir, "other.c4m")
			captureTree(t, project, projectC4m)
			captureTree(t, other, otherC4m)

			src := projectC4m + ":" + tc.src
			if tc.real {
				src = filepath.Join(project, filepath.FromSlash(tc.src)) + "/"
			}
			if code := catchExit(func() { runCp([]string{src, otherC4m + ":" + tc.dst}) }); code != -1 {
				t.Fatalf("cp: exit %d", code)
			}
			edited, _ := os.ReadFile(otherC4m)

			if err := copyInto(filepath.Join(project, filepath.FromSlash(tc.src)), filepath.Join(other, filepath.FromSlash(tc.dst))); err != nil {
				t.Fatal(err)
			}
			pinTimes(t, other, tc.nullRels...)
			want := captureTree(t, other, filepath.Join(dir, "rescan.c4m"))
			if string(edited) != want {
				t.Errorf("c4sh cp:\n%s\nrescan of the same copy:\n%s", edited, want)
			}
		})
	}
}

// A copy within one c4m reads its source before it writes.
func TestCpWithinOneC4m(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	c4store.NewTreeStore(storePath)
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	project := filepath.Join(dir, "project")
	buildEditTree(t, project)
	c4mPath := filepath.Join(dir, "project.c4m")
	captureTree(t, project, c4mPath)

	if code := catchExit(func() { runCp([]string{c4mPath + ":shots/", c4mPath + ":deliveries/v3/notes/"}) }); code != -1 {
		t.Fatalf("cp: exit %d", code)
	}
	if code := catchExit(func() { runCp([]string{c4mPath + ":README.md", c4mPath + ":shots/010/"}) }); code != -1 {
		t.Fatalf("cp README.md: exit %d", code)
	}
	edited, _ := os.ReadFile(c4mPath)
	if err := copyInto(filepath.Join(project, "shots"), filepath.Join(project, "deliveries/v3/notes")); err != nil {
		t.Fatal(err)
	}
	if err := copyInto(filepath.Join(project, "README.md"), filepath.Join(project, "shots/010")); err != nil {
		t.Fatal(err)
	}
	pinTimes(t, project)
	if want := captureTree(t, project, filepath.Join(dir, "rescan.c4m")); string(edited) != want {
		t.Errorf("c4sh cp:\n%s\nrescan:\n%s", edited, want)
	}
}

// A folder can't replace a file of the same name, or a file a folder; the
// copy refuses and the c4m is left as it was.
func TestCpIntoAC4mRefusesAKindClash(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	c4store.NewTreeStore(storePath)
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	project := filepath.Join(dir, "project")
	buildEditTree(t, project)
	projectC4m := filepath.Join(dir, "project.c4m")
	captureTree(t, project, projectC4m)

	other := filepath.Join(dir, "other")
	os.MkdirAll(filepath.Join(other, "README.md"), 0755)
	os.WriteFile(filepath.Join(other, "README.md", "f"), []byte("f"), 0644)
	os.WriteFile(filepath.Join(other, "scratch"), []byte("a file"), 0644)
	pinTimes(t, other)
	otherC4m := filepath.Join(dir, "other.c4m")
	before := captureTree(t, other, otherC4m)

	for _, c := range [][2]string{{"README.md", ""}, {"scratch/", "scratch/"}} {
		r, w, _ := os.Pipe()
		orig := os.Stderr
		os.Stderr = w
		code := catchExit(func() { runCp([]string{projectC4m + ":" + c[0], otherC4m + ":" + c[1]}) })
		w.Close()
		os.Stderr = orig
		msg, _ := io.ReadAll(r)
		if code != 1 {
			t.Errorf("cp %s %s: exit %d, want 1", c[0], c[1], code)
		}
		if !strings.Contains(string(msg), "cannot overwrite") && !strings.Contains(string(msg), "not a directory") {
			t.Errorf("cp %s %s: stderr %q", c[0], c[1], msg)
		}
	}
	if after, _ := os.ReadFile(otherC4m); string(after) != before {
		t.Errorf("a refused cp changed the c4m:\n%s", after)
	}
}

// A folder a copy creates with no source folder is never dated by the
// clock.
func TestCpCreatesNoClockDatedFolder(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	c4store.NewTreeStore(storePath)
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	empty := filepath.Join(dir, "empty.c4m")
	os.WriteFile(empty, nil, 0644)
	dst := filepath.Join(dir, "dst.c4m")
	if code := catchExit(func() { runCp([]string{empty + ":", dst + ":x/y/"}) }); code != -1 {
		t.Fatalf("cp: exit %d", code)
	}
	m, err := loadManifest(dst)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range m.Entries {
		if !e.Timestamp.Equal(c4m.NullTimestamp()) {
			t.Errorf("%s timestamp = %v, want null", m.EntryPath(e), e.Timestamp)
		}
	}
	if len(m.Entries) != 2 {
		t.Errorf("got %d entries, want x/ and x/y/", len(m.Entries))
	}
}
