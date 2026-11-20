package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Avalanche-io/c4"
	c4store "github.com/Avalanche-io/c4/store"
)

// c4sh rsync sends only what a c4m names. Through 1.0.26 a push copied the
// whole local store to the remote, content the c4m doesn't name included,
// and a pull copied the whole remote store back. These tests run the real
// rsync with a stand-in for ssh (RSYNC_RSH runs the remote side locally, in
// a fake remote home), and read the remote with the store the c4 CLI uses.

// rsyncEnv is one local store, one c4m captured into it, an unrelated
// object beside it, and a fake remote home.
type rsyncEnv struct {
	dir, c4mPath, remoteHome string
	local                    *c4store.TreeStore
	unrelated                c4.ID
}

func newRsyncEnv(t *testing.T) *rsyncEnv {
	t.Helper()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync is not installed")
	}
	dir := t.TempDir()
	e := &rsyncEnv{dir: dir, remoteHome: filepath.Join(dir, "remote")}
	local, err := c4store.NewTreeStore(filepath.Join(dir, "local"))
	if err != nil {
		t.Fatal(err)
	}
	e.local = local
	t.Setenv("C4_STORE", filepath.Join(dir, "local"))
	t.Setenv("C4_CONTEXT", "")
	t.Setenv("C4_REMOTE_STORE", "")
	withTestExit(t)

	root := filepath.Join(dir, "project")
	buildEditTree(t, root)
	e.c4mPath = filepath.Join(dir, "delivery.c4m")
	captureTree(t, root, e.c4mPath)

	// Content the c4m doesn't name, in the same local store.
	e.unrelated, err = local.Put(strings.NewReader("unrelated secret\n"))
	if err != nil {
		t.Fatal(err)
	}

	rsh := filepath.Join(dir, "fake-rsh.sh")
	script := "#!/bin/sh\ncd \"$FAKE_REMOTE_HOME\" || exit 9\nshift\nexec \"$@\"\n"
	if err := os.WriteFile(rsh, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RSYNC_RSH", rsh)
	t.Setenv("FAKE_REMOTE_HOME", e.remoteHome)
	os.MkdirAll(filepath.Join(e.remoteHome, "deliveries"), 0755)
	return e
}

// fileIDs returns the C4 IDs of every file the c4m names.
func (e *rsyncEnv) fileIDs(t *testing.T) map[c4.ID]string {
	t.Helper()
	m, err := loadManifest(e.c4mPath)
	if err != nil {
		t.Fatal(err)
	}
	ids := make(map[c4.ID]string)
	for _, en := range m.Entries {
		if !en.IsDir() && !en.C4ID.IsNil() {
			ids[en.C4ID] = m.EntryPath(en)
		}
	}
	if len(ids) == 0 {
		t.Fatal("the captured c4m names no files")
	}
	return ids
}

// runRsyncCapturing runs c4sh rsync and returns its exit code and output.
func runRsyncCapturing(t *testing.T, args ...string) (int, string) {
	t.Helper()
	r, w, _ := os.Pipe()
	origErr, origOut := os.Stderr, os.Stdout
	os.Stderr, os.Stdout = w, w
	done := make(chan string)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()
	code := catchExit(func() { runRsync(args) })
	w.Close()
	os.Stderr, os.Stdout = origErr, origOut
	return code, <-done
}

// objectFiles returns every object file under a store root, by name.
func objectFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			if _, perr := c4.Parse(info.Name()); perr == nil {
				out[info.Name()] = p
			}
		}
		return nil
	})
	return out
}

// exactPath is where a store reader that only follows exact-case folders
// to the deepest one that exists (every c4 release's first probe) looks
// for id.
func exactPath(root string, id c4.ID) string {
	s := id.String()
	dir := root
	for i := 0; i+2 <= len(s); i += 2 {
		sub := filepath.Join(dir, s[i:i+2])
		if info, err := os.Stat(sub); err != nil || !info.IsDir() {
			break
		}
		dir = sub
	}
	return filepath.Join(dir, s)
}

var sentRE = regexp.MustCompile(`sent (\d+) objects, (\d+) already on the remote`)
var fetchedRE = regexp.MustCompile(`fetched (\d+) objects, (\d+) already here`)

func count(t *testing.T, re *regexp.Regexp, out string) (int, int) {
	t.Helper()
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no %q line in:\n%s", re, out)
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	return a, b
}

// pushInto pushes the c4m into a remote store made by makeRemote, then
// checks what arrived, where, and what the count says.
func pushInto(t *testing.T, makeRemote func(t *testing.T, root string) int) {
	e := newRsyncEnv(t)
	remoteRoot := filepath.Join(e.remoteHome, ".c4", "store")
	preexisting := makeRemote(t, remoteRoot)
	before := len(objectFiles(t, remoteRoot))
	if before != preexisting {
		t.Fatalf("remote fixture holds %d objects, want %d", before, preexisting)
	}

	code, out := runRsyncCapturing(t, e.c4mPath, "fakehost:deliveries/")
	if code != -1 {
		t.Fatalf("push: exit %d:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(e.remoteHome, "deliveries", "delivery.c4m")); err != nil {
		t.Errorf("the c4m didn't reach the remote: %v", err)
	}

	remote, err := c4store.NewTreeStore(remoteRoot)
	if err != nil {
		t.Fatal(err)
	}
	for id, p := range e.fileIDs(t) {
		rc, err := remote.Open(id)
		if err != nil {
			t.Errorf("remote store can't read %s (%s): %v", p, id, err)
			continue
		}
		got := c4.Identify(rc)
		rc.Close()
		if got != id {
			t.Errorf("remote object %s holds bytes of %s", id, got)
		}
		if _, err := os.Stat(exactPath(remoteRoot, id)); err != nil {
			t.Errorf("%s isn't where the remote's store looks first (%s)", p, exactPath(remoteRoot, id))
		}
	}
	after := objectFiles(t, remoteRoot)
	if p, ok := after[e.unrelated.String()]; ok {
		t.Errorf("an object the c4m doesn't name reached the remote: %s", p)
	}
	sent, already := count(t, sentRE, out)
	if sent != len(after)-before {
		t.Errorf("printed sent %d, but %d objects arrived", sent, len(after)-before)
	}

	// A second push sends nothing and says so.
	code, out = runRsyncCapturing(t, e.c4mPath, "fakehost:deliveries/")
	if code != -1 {
		t.Fatalf("second push: exit %d:\n%s", code, out)
	}
	sent2, already2 := count(t, sentRE, out)
	if sent2 != 0 || already2 != sent+already {
		t.Errorf("second push: sent %d, %d already; want 0, %d", sent2, already2, sent+already)
	}
	if n := len(objectFiles(t, remoteRoot)); n != len(after) {
		t.Errorf("second push changed the remote: %d objects, then %d", len(after), n)
	}
}

func TestRsyncPushIntoAFlatStoreSendsOnlyWhatTheC4mNames(t *testing.T) {
	pushInto(t, func(t *testing.T, root string) int {
		s, err := c4store.NewTreeStore(root)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			s.Put(strings.NewReader(fmt.Sprintf("remote's own %d\n", i)))
		}
		if entries, _ := os.ReadDir(root); len(entries) != 3 {
			t.Fatalf("flat fixture: %d root entries, want 3 files", len(entries))
		}
		return 3
	})
}

func TestRsyncPushIntoASplitStoreSendsOnlyWhatTheC4mNames(t *testing.T) {
	pushInto(t, func(t *testing.T, root string) int {
		s, err := c4store.NewTreeStore(root)
		if err != nil {
			t.Fatal(err)
		}
		s.SetSplitThreshold(4)
		for i := 0; i < 200; i++ {
			s.Put(strings.NewReader(fmt.Sprintf("remote's own %d\n", i)))
		}
		if info, err := os.Stat(filepath.Join(root, "c4")); err != nil || !info.IsDir() {
			t.Fatal("split fixture has no c4/ folder")
		}
		deeper, _ := filepath.Glob(filepath.Join(root, "c4", "*", "*"))
		if len(deeper) == 0 {
			t.Fatal("split fixture has no c4/XX/ level")
		}
		return 200
	})
}

// A pull fetches what the pulled c4m names from a remote store that holds
// much more, including an object a split left a level up.
func TestRsyncPullFetchesOnlyWhatTheC4mNames(t *testing.T) {
	e := newRsyncEnv(t)
	remoteRoot := filepath.Join(e.remoteHome, ".c4", "store")
	remote, err := c4store.NewTreeStore(remoteRoot)
	if err != nil {
		t.Fatal(err)
	}
	remote.SetSplitThreshold(4)
	for i := 0; i < 200; i++ {
		remote.Put(strings.NewReader(fmt.Sprintf("remote's own %d\n", i)))
	}
	ids := e.fileIDs(t)
	for id := range ids {
		rc, _ := e.local.Open(id)
		remote.Put(rc)
		rc.Close()
	}
	// One of the c4m's objects a level above its folder, where a split in
	// another process can leave it.
	var moved c4.ID
	for id := range ids {
		p, _ := remote.ContentPath(id)
		if filepath.Dir(p) != remoteRoot {
			up := filepath.Join(filepath.Dir(filepath.Dir(p)), id.String())
			if err := os.Rename(p, up); err != nil {
				t.Fatal(err)
			}
			moved = id
			break
		}
	}
	if moved.IsNil() {
		t.Fatal("no object to move up a level")
	}
	os.Rename(e.c4mPath, filepath.Join(e.remoteHome, "deliveries", "delivery.c4m"))

	// A fresh local store.
	freshRoot := filepath.Join(e.dir, "fresh")
	c4store.NewTreeStore(freshRoot)
	t.Setenv("C4_STORE", freshRoot)
	into := filepath.Join(e.dir, "pulled")
	os.MkdirAll(into, 0755)

	code, out := runRsyncCapturing(t, "fakehost:deliveries/delivery.c4m", into)
	if code != -1 {
		t.Fatalf("pull: exit %d:\n%s", code, out)
	}
	fresh, _ := c4store.NewTreeStore(freshRoot)
	for id, p := range ids {
		if !fresh.Has(id) {
			t.Errorf("pull didn't fetch %s (%s)", p, id)
		}
	}
	got := objectFiles(t, freshRoot)
	if len(got) != len(ids) {
		t.Errorf("pull fetched %d objects, want the c4m's %d", len(got), len(ids))
	}
	fetched, _ := count(t, fetchedRE, out)
	if fetched != len(got) {
		t.Errorf("printed fetched %d, but %d objects arrived", fetched, len(got))
	}
}

// A file whose content isn't in the local store is named, the rest is
// sent, and the push exits 1.
func TestRsyncPushNamesAMissingFileAndExits1(t *testing.T) {
	e := newRsyncEnv(t)
	ids := e.fileIDs(t)
	var gone c4.ID
	var gonePath string
	for id, p := range ids {
		gone, gonePath = id, p
		break
	}
	if err := e.local.Remove(gone); err != nil {
		t.Fatal(err)
	}
	code, out := runRsyncCapturing(t, e.c4mPath, "fakehost:deliveries/")
	if code != 1 {
		t.Fatalf("push with a missing object: exit %d, want 1:\n%s", code, out)
	}
	if !strings.Contains(out, "missing "+gonePath+" "+gone.String()) {
		t.Errorf("the missing file isn't named:\n%s", out)
	}
	remote, _ := c4store.NewTreeStore(filepath.Join(e.remoteHome, ".c4", "store"))
	for id, p := range ids {
		if id != gone && !remote.Has(id) {
			t.Errorf("%s wasn't sent", p)
		}
	}
}

// A folded range's frames travel with it, and its ID list too when the
// store holds it; an unrelated object still doesn't.
func TestRsyncPushSendsARangesFramesAndList(t *testing.T) {
	e := newRsyncEnv(t)
	root := filepath.Join(e.dir, "seq")
	seqC4m := filepath.Join(e.dir, "seq.c4m")
	frames := captureRange(t, e.local, root, seqC4m, false)

	code, out := runRsyncCapturing(t, seqC4m, "fakehost:deliveries/")
	if code != -1 {
		t.Fatalf("push: exit %d:\n%s", code, out)
	}
	remote, _ := c4store.NewTreeStore(filepath.Join(e.remoteHome, ".c4", "store"))
	for i, id := range frames {
		if !remote.Has(id) {
			t.Errorf("frame %d (%s) wasn't sent", i+1, id)
		}
	}
	m, _ := loadManifest(seqC4m)
	for _, en := range m.Entries {
		if en.IsSequence && !remote.Has(en.C4ID) {
			t.Errorf("the range's ID list %s wasn't sent", en.C4ID)
		}
	}
	if remote.Has(e.unrelated) {
		t.Error("an object the c4m doesn't name reached the remote")
	}
}
