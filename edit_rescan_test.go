package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Avalanche-io/c4"
	"github.com/Avalanche-io/c4/c4m"
	c4store "github.com/Avalanche-io/c4/store"
)

// An edit c4sh makes to a c4m file must give the c4m a scan of the edited
// tree gives: same placement, and the same derived directory sizes and
// C4 IDs. Each case below makes the edit twice, once with c4sh on a
// captured c4m and once on disk, then captures the edited tree and
// compares the two files byte for byte.

// editTime is the mtime of every file and folder in the test tree.
var editTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// buildEditTree writes the README's example project under root.
func buildEditTree(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		"README.md":                 "readme\n",
		"shots/010/comp.nk":         "comp\n",
		"shots/020/plate.exr":       "plate\n",
		"deliveries/v3/d.txt":       "d3\n",
		"deliveries/v3/notes/n.txt": "n\n",
	}
	for p, body := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "scratch"), 0755); err != nil {
		t.Fatal(err)
	}
	pinTimes(t, root, "")
}

// pinTimes sets every file and folder under root to editTime, except the
// folder at rel (slash path), which gets the epoch: a scan reads the epoch
// as a null timestamp, which is what c4sh mkdir records for a folder it
// creates and never observed.
func pinTimes(t *testing.T, root, nullRel string) {
	t.Helper()
	var paths []string
	filepath.Walk(root, func(p string, _ os.FileInfo, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
		return nil
	})
	// Children first, so setting a folder's time is the last write in it.
	for i := len(paths) - 1; i >= 0; i-- {
		ts := editTime
		if nullRel != "" && paths[i] == filepath.Join(root, filepath.FromSlash(nullRel)) {
			ts = time.Unix(0, 0)
		}
		if err := os.Chtimes(paths[i], ts, ts); err != nil {
			t.Fatal(err)
		}
	}
}

// captureTree captures root into c4mPath with c4sh cp and returns its text.
func captureTree(t *testing.T, root, c4mPath string) string {
	t.Helper()
	os.Remove(c4mPath)
	cpRealToC4m(root+"/", c4mPath+":")
	b, err := os.ReadFile(c4mPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestEditsMatchARescanOfTheEditedTree(t *testing.T) {
	cases := []struct {
		name    string
		c4sh    [][]string // c4sh commands; paths are relative to the c4m
		disk    func(root string) error
		nullRel string // a folder c4sh created, whose time it does not know
	}{
		{
			name:    "mkdir beside existing folders (README L191)",
			c4sh:    [][]string{{"mkdir", "deliveries/v4/"}},
			disk:    func(r string) error { return os.Mkdir(filepath.Join(r, "deliveries/v4"), 0755) },
			nullRel: "deliveries/v4",
		},
		{
			name:    "mkdir at the root",
			c4sh:    [][]string{{"mkdir", "archive/"}},
			disk:    func(r string) error { return os.Mkdir(filepath.Join(r, "archive"), 0755) },
			nullRel: "archive",
		},
		{
			name:    "mkdir -p under an existing folder",
			c4sh:    [][]string{{"mkdir", "-p", "shots/030/"}},
			disk:    func(r string) error { return os.MkdirAll(filepath.Join(r, "shots/030"), 0755) },
			nullRel: "shots/030",
		},
		{
			name: "mkdir then mv a file into it (README L191-192)",
			c4sh: [][]string{{"mkdir", "deliveries/v4/"}, {"mv", "shots/010/comp.nk", "deliveries/v4/"}},
			disk: func(r string) error {
				if err := os.Mkdir(filepath.Join(r, "deliveries/v4"), 0755); err != nil {
					return err
				}
				return os.Rename(filepath.Join(r, "shots/010/comp.nk"), filepath.Join(r, "deliveries/v4/comp.nk"))
			},
			// c4sh made v4 with a null time; with a child in it, both
			// c4sh and a scan fill that from the newest child.
			nullRel: "deliveries/v4",
		},
		{
			name: "mv a file into an existing folder at the same depth",
			c4sh: [][]string{{"mv", "shots/010/comp.nk", "deliveries/v3/"}},
			disk: func(r string) error {
				return os.Rename(filepath.Join(r, "shots/010/comp.nk"), filepath.Join(r, "deliveries/v3/comp.nk"))
			},
		},
		{
			name: "mv a folder to a new name at the root (README L17)",
			c4sh: [][]string{{"mv", "shots/010/", "archive/"}},
			disk: func(r string) error {
				return os.Rename(filepath.Join(r, "shots/010"), filepath.Join(r, "archive"))
			},
		},
		{
			name: "mv a folder deeper, into another folder",
			c4sh: [][]string{{"mv", "deliveries/", "shots/020/"}},
			disk: func(r string) error {
				return os.Rename(filepath.Join(r, "deliveries"), filepath.Join(r, "shots/020/deliveries"))
			},
		},
		{
			name: "rename a file in place",
			c4sh: [][]string{{"mv", "README.md", "readme.txt"}},
			disk: func(r string) error {
				return os.Rename(filepath.Join(r, "README.md"), filepath.Join(r, "readme.txt"))
			},
		},
		{
			name: "mv the last file out of a folder",
			c4sh: [][]string{{"mv", "shots/010/comp.nk", "comp.nk"}},
			disk: func(r string) error {
				return os.Rename(filepath.Join(r, "shots/010/comp.nk"), filepath.Join(r, "comp.nk"))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			storePath := filepath.Join(dir, "store")
			if _, err := c4store.NewTreeStore(storePath); err != nil {
				t.Fatal(err)
			}
			t.Setenv("C4_STORE", storePath)
			t.Setenv("C4_CONTEXT", "")
			withTestExit(t)

			root := filepath.Join(dir, "project")
			buildEditTree(t, root)
			c4mPath := filepath.Join(dir, "project.c4m")
			captureTree(t, root, c4mPath)

			for _, cmd := range tc.c4sh {
				args := make([]string, 0, len(cmd)-1)
				for _, a := range cmd[1:] {
					if strings.HasPrefix(a, "-") {
						args = append(args, a)
						continue
					}
					args = append(args, c4mPath+":"+a)
				}
				run := map[string]func([]string){"mv": runMv, "mkdir": runMkdir}[cmd[0]]
				if code := catchExit(func() { run(args) }); code != -1 {
					t.Fatalf("c4sh %s: exit %d", strings.Join(cmd, " "), code)
				}
			}
			edited, err := os.ReadFile(c4mPath)
			if err != nil {
				t.Fatal(err)
			}

			if err := tc.disk(root); err != nil {
				t.Fatal(err)
			}
			pinTimes(t, root, tc.nullRel)
			want := captureTree(t, root, filepath.Join(dir, "rescan.c4m"))

			if string(edited) != want {
				t.Errorf("c4sh edit:\n%s\nrescan of the edited tree:\n%s", edited, want)
			}
		})
	}
}

// A folder can't move into itself or below itself: the moved subtree would
// have to contain its own new parent.
func TestMvRefusesAFolderIntoItself(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	c4store.NewTreeStore(storePath)
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	root := filepath.Join(dir, "project")
	buildEditTree(t, root)
	c4mPath := filepath.Join(dir, "project.c4m")
	before := captureTree(t, root, c4mPath)

	for _, dst := range []string{"shots/010/", "shots/"} {
		code := catchExit(func() { runMv([]string{c4mPath + ":shots/", c4mPath + ":" + dst}) })
		if code != 1 {
			t.Errorf("mv shots/ %s: exit %d, want 1", dst, code)
		}
	}
	after, _ := os.ReadFile(c4mPath)
	if string(after) != before {
		t.Errorf("a refused mv changed the c4m:\n%s", after)
	}
}

// mv onto a name that already exists in the destination folder refuses,
// and never drops either entry.
func TestMvRefusesAnExistingNameInTheDestinationFolder(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	c4store.NewTreeStore(storePath)
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	root := filepath.Join(dir, "project")
	buildEditTree(t, root)
	if err := os.WriteFile(filepath.Join(root, "deliveries/v3/comp.nk"), []byte("other\n"), 0644); err != nil {
		t.Fatal(err)
	}
	pinTimes(t, root, "")
	c4mPath := filepath.Join(dir, "project.c4m")
	before := captureTree(t, root, c4mPath)

	code := catchExit(func() { runMv([]string{c4mPath + ":shots/010/comp.nk", c4mPath + ":deliveries/v3/"}) })
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	after, _ := os.ReadFile(c4mPath)
	if string(after) != before {
		t.Errorf("a refused mv changed the c4m:\n%s", after)
	}
}

// A folder listed without its children (its listing lives under its C4 ID)
// may carry a null size. A parent re-derived over it can't know its own
// size either, so it derives to null rather than counting the child as 0.
func TestRederiveKeepsANullChildSizeNull(t *testing.T) {
	ts := editTime
	listed := c4.Identify(strings.NewReader("elsewhere\n"))
	m := c4m.NewManifest()
	top := &c4m.Entry{Name: "top/", Depth: 0, Mode: os.ModeDir | 0755, Size: 999, Timestamp: ts, C4ID: listed}
	m.AddEntry(top)
	m.AddEntry(&c4m.Entry{Name: "collapsed/", Depth: 1, Mode: os.ModeDir | 0755, Size: -1, Timestamp: ts, C4ID: listed})
	m.AddEntry(&c4m.Entry{Name: "known/", Depth: 1, Mode: os.ModeDir | 0755, Size: 0, Timestamp: ts, C4ID: c4.Identify(strings.NewReader(""))})
	m.SortEntries()

	addEmptyDir(m, m.GetEntry("top/known/"), "new")
	if top.Size != -1 {
		t.Errorf("top/ size = %d, want null (-1): a child's size is null", top.Size)
	}
	if top.C4ID == listed {
		t.Error("top/ C4 ID was not re-derived")
	}
	if !top.Timestamp.Equal(ts) {
		t.Errorf("top/ timestamp = %v, want the stated %v", top.Timestamp, ts)
	}
	if c := m.GetEntry("top/collapsed/"); c.Size != -1 {
		t.Errorf("collapsed/ size = %d, want it left null", c.Size)
	}
}
