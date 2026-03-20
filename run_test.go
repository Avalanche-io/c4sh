package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	c4store "github.com/Avalanche-io/c4/store"

	"github.com/Avalanche-io/c4sh/internal/ctx"
)

// exitError is a sentinel type used by the test exit hook.
type exitError struct {
	code int
}

func (e exitError) Error() string { return "exit" }

// withTestExit replaces osExit with a function that panics with exitError,
// allowing tests to recover and check the exit code. The original osExit
// is restored when the test finishes.
func withTestExit(t *testing.T) {
	t.Helper()
	orig := osExit
	osExit = func(code int) { panic(exitError{code}) }
	t.Cleanup(func() { osExit = orig })
}

// catchExit calls fn and returns the exit code if osExit was called,
// or -1 if fn returned normally.
func catchExit(fn func()) int {
	var code int = -1
	func() {
		defer func() {
			if r := recover(); r != nil {
				if e, ok := r.(exitError); ok {
					code = e.code
				} else {
					panic(r)
				}
			}
		}()
		fn()
	}()
	return code
}

// ===========================================================================
// runMv
// ===========================================================================

func TestRunMv_NoArgs(t *testing.T) {
	withTestExit(t)
	code := catchExit(func() { runMv(nil) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRunMv_Rename(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	code := catchExit(func() { runMv([]string{"hello.txt", "renamed.txt"}) })
	if code != -1 {
		t.Fatalf("unexpected exit code = %d", code)
	}

	m := te.loadC4m(t)
	if e, _ := findEntryByPath(m, "hello.txt"); e != nil {
		t.Error("hello.txt should not exist")
	}
	if e, _ := findEntryByPath(m, "renamed.txt"); e == nil {
		t.Error("renamed.txt should exist")
	}
}

func TestRunMv_CrossC4mError(t *testing.T) {
	te := newTestEnv(t)
	// Create a second c4m file
	te2 := newTestEnv(t)
	withTestExit(t)

	code := catchExit(func() {
		runMv([]string{te.c4mPath + ":hello.txt", te2.c4mPath + ":dest.txt"})
	})
	if code != 1 {
		t.Errorf("exit code = %d, want 1 for cross-c4m mv", code)
	}
}

func TestRunMv_MoveRoot(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	// Moving "" (the root) should fail
	code := catchExit(func() {
		runMv([]string{te.c4mPath + ":", "somewhere"})
	})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

// ===========================================================================
// runRm
// ===========================================================================

func TestRunRm_NoArgs(t *testing.T) {
	withTestExit(t)
	t.Setenv("C4_CONTEXT", "/tmp/fake.c4m")
	code := catchExit(func() { runRm(nil) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRunRm_RemoveFile(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	code := catchExit(func() { runRm([]string{"hello.txt"}) })
	if code != -1 {
		t.Fatalf("unexpected exit code = %d", code)
	}

	m := te.loadC4m(t)
	if e, _ := findEntryByPath(m, "hello.txt"); e != nil {
		t.Error("hello.txt should not exist after rm")
	}
}

func TestRunRm_ForceFlag(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	code := catchExit(func() { runRm([]string{"-f", "nonexistent"}) })
	if code != -1 {
		t.Errorf("exit code = %d, want -1 (no exit) with -f", code)
	}
}

func TestRunRm_RecursiveDir(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	code := catchExit(func() { runRm([]string{"-r", "sub/"}) })
	if code != -1 {
		t.Fatalf("unexpected exit code = %d", code)
	}

	m := te.loadC4m(t)
	if e, _ := findEntryByPath(m, "sub/"); e != nil {
		t.Error("sub/ should not exist")
	}
}

func TestRunRm_DirWithoutRecursive(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	code := catchExit(func() { runRm([]string{"sub/"}) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1 for dir without -r", code)
	}
}

// ===========================================================================
// runMkdir
// ===========================================================================

func TestRunMkdir_NoArgs(t *testing.T) {
	withTestExit(t)
	t.Setenv("C4_CONTEXT", "/tmp/fake.c4m")
	code := catchExit(func() { runMkdir(nil) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRunMkdir_CreateDir(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	code := catchExit(func() { runMkdir([]string{"newdir"}) })
	if code != -1 {
		t.Fatalf("unexpected exit code = %d", code)
	}

	m := te.loadC4m(t)
	if e, _ := findEntryByPath(m, "newdir/"); e == nil {
		t.Error("newdir/ should exist")
	}
}

func TestRunMkdir_WithParents(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	code := catchExit(func() { runMkdir([]string{"-p", "x/y/z"}) })
	if code != -1 {
		t.Fatalf("unexpected exit code = %d", code)
	}

	m := te.loadC4m(t)
	for _, p := range []string{"x/", "x/y/", "x/y/z/"} {
		if e, _ := findEntryByPath(m, p); e == nil {
			t.Errorf("%s should exist", p)
		}
	}
}

func TestRunMkdir_AlreadyExists(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	code := catchExit(func() { runMkdir([]string{"sub"}) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1 for existing dir", code)
	}
}

// ===========================================================================
// runCd
// ===========================================================================

func TestRunCd_NoContext_Home(t *testing.T) {
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runCd(nil) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit code = %d", code)
	}

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "builtin cd") {
		t.Errorf("expected builtin cd, got %q", out)
	}
}

func TestRunCd_Dash(t *testing.T) {
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runCd([]string{"-"}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "unset C4_CONTEXT") {
		t.Errorf("expected unset, got %q", out)
	}
}

func TestRunCd_IntoC4m(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runCd([]string{te.c4mPath}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "C4_CONTEXT=") {
		t.Errorf("expected C4_CONTEXT export, got %q", out)
	}
}

func TestRunCd_NavigateWithin(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runCd([]string{"sub"}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "C4_CWD=") {
		t.Errorf("expected C4_CWD update, got %q", out)
	}
}

func TestRunCd_ExitContext(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "sub/")
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runCd(nil) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "unset C4_CONTEXT") {
		t.Errorf("expected unset, got %q", out)
	}
}

// ===========================================================================
// runPool
// ===========================================================================

func TestRunPool_NoArgs(t *testing.T) {
	withTestExit(t)
	code := catchExit(func() { runPool(nil) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRunPool_Success(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_STORE", te.storePath)
	withTestExit(t)

	bundleDir := filepath.Join(te.dir, "pool_bundle")

	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runPool([]string{te.c4mPath, bundleDir}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	if _, err := os.Stat(filepath.Join(bundleDir, "store")); err != nil {
		t.Error("pool store should exist")
	}
}

// ===========================================================================
// runIngest
// ===========================================================================

func TestRunIngest_NoArgs(t *testing.T) {
	withTestExit(t)
	code := catchExit(func() { runIngest(nil) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRunIngest_Success(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	// Pool first
	bundleDir := filepath.Join(te.dir, "bundle")
	_, err := poolManifest(m, te.store, te.c4mPath, bundleDir)
	if err != nil {
		t.Fatalf("poolManifest: %v", err)
	}

	// Create target store
	newStorePath := filepath.Join(te.dir, "new_store")
	_, err = c4store.NewTreeStore(newStorePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}
	t.Setenv("C4_STORE", newStorePath)
	withTestExit(t)

	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runIngest([]string{bundleDir}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}
}

func TestRunIngest_NoStoreDir(t *testing.T) {
	withTestExit(t)
	code := catchExit(func() { runIngest([]string{"/nonexistent/bundle"}) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

// ===========================================================================
// runRsync
// ===========================================================================

func TestRunRsync_NoArgs(t *testing.T) {
	withTestExit(t)
	code := catchExit(func() { runRsync(nil) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRunRsync_BothRemote(t *testing.T) {
	withTestExit(t)
	code := catchExit(func() { runRsync([]string{"host1:/a", "host2:/b"}) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1 for both remote", code)
	}
}

func TestRunRsync_BothLocal(t *testing.T) {
	withTestExit(t)
	code := catchExit(func() { runRsync([]string{"/local/a", "/local/b"}) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1 for both local", code)
	}
}

// ===========================================================================
// runShellInit
// ===========================================================================

func TestRunShellInit_Bash(t *testing.T) {
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runShellInit([]string{"--bash"}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "function cd") {
		t.Error("expected bash script")
	}
}

func TestRunShellInit_Unsupported(t *testing.T) {
	t.Setenv("SHELL", "/bin/fish")
	withTestExit(t)

	code := catchExit(func() { runShellInit(nil) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

// ===========================================================================
// runLs
// ===========================================================================

func TestRunLs_InContext(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runLs(nil) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "hello.txt") {
		t.Errorf("expected hello.txt, got %q", out)
	}
}

func TestRunLs_WithPath(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runLs([]string{te.c4mPath + ":sub/"}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "nested.txt") {
		t.Errorf("expected nested.txt, got %q", out)
	}
}

func TestRunLs_BareC4m(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runLs([]string{te.c4mPath}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "hello.txt") {
		t.Errorf("expected hello.txt, got %q", out)
	}
}

func TestRunLs_LongFlagAndAll(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runLs([]string{"-la"}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}
}

func TestRunLs_InContextRelativePath(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runLs([]string{"sub/"}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "nested.txt") {
		t.Errorf("expected nested.txt, got %q", out)
	}
}

// ===========================================================================
// runCat
// ===========================================================================

func TestRunCat_FromC4m(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_STORE", te.storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runCat([]string{te.c4mPath + ":hello.txt"}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if out != "hello world" {
		t.Errorf("cat output = %q, want %q", out, "hello world")
	}
}

func TestRunCat_InContext(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_STORE", te.storePath)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "sub/")
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runCat([]string{"nested.txt"}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if out != "nested content" {
		t.Errorf("cat output = %q, want %q", out, "nested content")
	}
}

// ===========================================================================
// die
// ===========================================================================

func TestDie(t *testing.T) {
	withTestExit(t)
	code := catchExit(func() { die("test error %d", 42) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

// ===========================================================================
// runComplete
// ===========================================================================

func TestRunComplete_Empty(t *testing.T) {
	withTestExit(t)
	code := catchExit(func() { runComplete(nil) })
	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}
}

func TestRunComplete_InContext(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runComplete([]string{"hel"}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "hello.txt") {
		t.Errorf("expected hello.txt completion, got %q", out)
	}
}

func TestRunComplete_InContextSubdir(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runComplete([]string{"sub/"}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "nested.txt") {
		t.Errorf("expected nested.txt, got %q", out)
	}
}

// ===========================================================================
// runCp
// ===========================================================================

func TestRunCp_RealToC4m(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	_, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	// Create source
	srcFile := filepath.Join(dir, "input.txt")
	os.WriteFile(srcFile, []byte("test content"), 0644)

	c4mPath := filepath.Join(dir, "out.c4m")

	code := catchExit(func() { runCp([]string{srcFile, c4mPath + ":"}) })
	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	m, err := loadManifest(c4mPath)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if len(m.Entries) != 1 {
		t.Errorf("entries = %d, want 1", len(m.Entries))
	}
}

func TestRunCp_C4mToReal(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_STORE", te.storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	outDir := filepath.Join(te.dir, "extract_out")

	code := catchExit(func() { runCp([]string{te.c4mPath + ":", outDir}) })
	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "hello.txt"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("content = %q, want %q", data, "hello world")
	}
}

func TestRunCp_C4mToC4m(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	dstC4m := filepath.Join(te.dir, "dest.c4m")

	code := catchExit(func() { runCp([]string{te.c4mPath + ":", dstC4m + ":"}) })
	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	m, err := loadManifest(dstC4m)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if len(m.Entries) == 0 {
		t.Error("dest manifest should have entries")
	}
}

func TestRunCp_NoArgs(t *testing.T) {
	withTestExit(t)
	code := catchExit(func() { runCp(nil) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRunCp_C4mSubpathToReal(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_STORE", te.storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	outDir := filepath.Join(te.dir, "sub_extract")
	os.MkdirAll(outDir, 0755)

	// Extract just sub/ from the c4m
	code := catchExit(func() { runCp([]string{te.c4mPath + ":sub/", outDir}) })
	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "nested.txt"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "nested content" {
		t.Errorf("content = %q, want %q", data, "nested content")
	}
}

func TestRunCp_MultiDest(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store")
	_, err := c4store.NewTreeStore(storePath)
	if err != nil {
		t.Fatalf("NewTreeStore: %v", err)
	}
	t.Setenv("C4_STORE", storePath)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	srcDir := filepath.Join(dir, "src")
	os.MkdirAll(srcDir, 0755)
	os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("data"), 0644)

	dest1 := filepath.Join(dir, "d1")
	dest2 := filepath.Join(dir, "d2")

	code := catchExit(func() { runCp([]string{srcDir, dest1, dest2}) })
	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	// Both destinations should have the file
	for _, d := range []string{dest1, dest2} {
		data, err := os.ReadFile(filepath.Join(d, "file.txt"))
		if err != nil {
			t.Errorf("read %s: %v", d, err)
			continue
		}
		if string(data) != "data" {
			t.Errorf("%s/file.txt = %q, want %q", d, data, "data")
		}
	}
}

// ===========================================================================
// runLs additional cases
// ===========================================================================

func TestRunLs_ExtensionFree(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	// The c4m file is "test.c4m", try listing with "test" (extension-free)
	noExt := strings.TrimSuffix(te.c4mPath, ".c4m")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runLs([]string{noExt}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "hello.txt") {
		t.Errorf("expected hello.txt, got %q", out)
	}
}

func TestRunLs_LongOptions(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	// --color=auto should be ignored
	code := catchExit(func() { runLs([]string{"--color=auto"}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}
}

func TestRunLs_OnePerLine(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runLs([]string{"-1"}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %q", len(lines), out)
	}
}

// ===========================================================================
// runCd additional cases
// ===========================================================================

func TestRunCd_TildePrefix(t *testing.T) {
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runCd([]string{"~/Documents"}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "builtin cd") {
		t.Errorf("expected builtin cd, got %q", out)
	}
}

func TestRunCd_AbsPathInContext(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "sub/")
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runCd([]string{"/tmp"}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "unset C4_CONTEXT") {
		t.Errorf("expected unset, got %q", out)
	}
}

func TestRunCd_C4mWithColon(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runCd([]string{te.c4mPath + ":sub/"}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "C4_CONTEXT=") {
		t.Errorf("expected C4_CONTEXT, got %q", out)
	}
}

func TestRunCd_PlainDir(t *testing.T) {
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runCd([]string{"/tmp"}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "builtin cd") {
		t.Errorf("expected builtin cd, got %q", out)
	}
}

// ===========================================================================
// runCat additional cases
// ===========================================================================

func TestRunCat_BareC4m(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	code := catchExit(func() { runCat([]string{te.c4mPath}) })
	if code != 1 {
		t.Errorf("expected exit 1 for bare c4m file, got %d", code)
	}
}

func TestRunCat_NoSubpath(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	code := catchExit(func() { runCat([]string{te.c4mPath + ":"}) })
	if code != 1 {
		t.Errorf("expected exit 1 for empty subpath, got %d", code)
	}
}

// ===========================================================================
// runMv additional cases
// ===========================================================================

func TestRunMv_MoveIntoDir(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	code := catchExit(func() { runMv([]string{"hello.txt", "sub/"}) })
	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	m := te.loadC4m(t)
	if e, _ := findEntryByPath(m, "sub/hello.txt"); e == nil {
		t.Error("sub/hello.txt should exist after mv into sub/")
	}
}

func TestRunMv_NonexistentSource(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	code := catchExit(func() { runMv([]string{"nosuch.txt", "renamed.txt"}) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

// ===========================================================================
// runPool additional cases
// ===========================================================================

func TestRunPool_BadC4m(t *testing.T) {
	withTestExit(t)
	code := catchExit(func() { runPool([]string{"/nonexistent/file.c4m", "/tmp/out"}) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRunPool_MissingObjects(t *testing.T) {
	te := newTestEnv(t)
	// Use an empty store (no objects)
	emptyStorePath := filepath.Join(te.dir, "empty_store")
	c4store.NewTreeStore(emptyStorePath)
	t.Setenv("C4_STORE", emptyStorePath)
	withTestExit(t)

	bundleDir := filepath.Join(te.dir, "pool_missing")

	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runPool([]string{te.c4mPath, bundleDir}) })

	w.Close()
	os.Stdout = old

	// Should exit 1 because objects are missing
	if code != 1 {
		t.Errorf("exit code = %d, want 1 for missing objects", code)
	}
}

// ===========================================================================
// runMkdir additional cases
// ===========================================================================

func TestRunMkdir_MissingParent(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	code := catchExit(func() { runMkdir([]string{"nonexistent/child"}) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRunMkdir_MultipleArgs(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	code := catchExit(func() { runMkdir([]string{"dir1", "dir2"}) })
	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	m := te.loadC4m(t)
	if e, _ := findEntryByPath(m, "dir1/"); e == nil {
		t.Error("dir1/ should exist")
	}
	if e, _ := findEntryByPath(m, "dir2/"); e == nil {
		t.Error("dir2/ should exist")
	}
}

// ===========================================================================
// runIngest additional cases
// ===========================================================================

func TestRunIngest_CopiesC4mFiles(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	bundleDir := filepath.Join(te.dir, "bundle_with_c4m")
	_, err := poolManifest(m, te.store, te.c4mPath, bundleDir)
	if err != nil {
		t.Fatalf("poolManifest: %v", err)
	}

	newStorePath := filepath.Join(te.dir, "ingest_store2")
	c4store.NewTreeStore(newStorePath)
	t.Setenv("C4_STORE", newStorePath)

	workDir := filepath.Join(te.dir, "ingest_work")
	os.MkdirAll(workDir, 0755)
	origDir, _ := os.Getwd()
	os.Chdir(workDir)
	t.Cleanup(func() { os.Chdir(origDir) })

	withTestExit(t)

	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runIngest([]string{bundleDir}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	// Check that the c4m file was copied to workDir
	if _, err := os.Stat(filepath.Join(workDir, "test.c4m")); err != nil {
		t.Error("test.c4m should be copied to working directory")
	}
}

// ===========================================================================
// runRm additional cases
// ===========================================================================

func TestRunRm_CrossC4m(t *testing.T) {
	te := newTestEnv(t)
	te2 := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	// Try to rm a path from a different c4m
	code := catchExit(func() { runRm([]string{te2.c4mPath + ":hello.txt"}) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1 for cross-c4m rm", code)
	}
}

func TestRunRm_Root(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	code := catchExit(func() { runRm([]string{te.c4mPath + ":"}) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1 for removing root", code)
	}
}

// ===========================================================================
// completeFilesystem
// ===========================================================================

func TestCompleteFilesystem_DirWithSlash(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "subdir"), 0755)
	os.WriteFile(filepath.Join(dir, "subdir", "project.c4m"), []byte{}, 0644)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(origDir) })

	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	completeFilesystem("subdir/")

	w.Close()
	os.Stdout = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "project.c4m") {
		t.Errorf("expected project.c4m, got %q", out)
	}
}

func TestCompleteFilesystem_EmptyPartial(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.c4m"), []byte{}, 0644)
	os.MkdirAll(filepath.Join(dir, "mydir"), 0755)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(origDir) })

	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	completeFilesystem("")

	w.Close()
	os.Stdout = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "test.c4m") {
		t.Errorf("expected test.c4m, got %q", out)
	}
	if !strings.Contains(out, "mydir/") {
		t.Errorf("expected mydir/, got %q", out)
	}
}

// ===========================================================================
// runMkdir: cross-c4m, root
// ===========================================================================

func TestRunMkdir_CrossC4m(t *testing.T) {
	te := newTestEnv(t)
	te2 := newTestEnv(t)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	// First path determines c4m; second path from different c4m triggers error
	code := catchExit(func() {
		runMkdir([]string{te.c4mPath + ":newdir1", te2.c4mPath + ":newdir2"})
	})
	if code != 1 {
		t.Errorf("exit code = %d, want 1 for cross-c4m mkdir", code)
	}
}

func TestRunMkdir_Root(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	// Without -p, creating root should error
	code := catchExit(func() { runMkdir([]string{te.c4mPath + ":"}) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1 for mkdir root without -p", code)
	}
}

func TestRunMkdir_RootWithParents(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	// With -p, creating root is a no-op (not an error)
	code := catchExit(func() { runMkdir([]string{"-p", te.c4mPath + ":"}) })
	if code != -1 {
		t.Errorf("exit code = %d, want -1 (no exit) for mkdir -p root", code)
	}
}

// ===========================================================================
// runMv: no-context fallthrough (can't fully test but verify path)
// ===========================================================================

func TestRunMv_WithColonPath(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	code := catchExit(func() {
		runMv([]string{te.c4mPath + ":hello.txt", te.c4mPath + ":renamed.txt"})
	})
	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	m := te.loadC4m(t)
	if e, _ := findEntryByPath(m, "renamed.txt"); e == nil {
		t.Error("renamed.txt should exist")
	}
}

// ===========================================================================
// runRm: full rm flow with context & flags
// ===========================================================================

func TestRunRm_MultiFlagsRF(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", te.c4mPath)
	t.Setenv("C4_CWD", "")
	withTestExit(t)

	code := catchExit(func() { runRm([]string{"-rf", "sub/"}) })
	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	m := te.loadC4m(t)
	if e, _ := findEntryByPath(m, "sub/"); e != nil {
		t.Error("sub/ should not exist")
	}
}

// ===========================================================================
// runPool: ext-free c4m name
// ===========================================================================

func TestRunPool_ExtensionFree(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_STORE", te.storePath)
	withTestExit(t)

	bundleDir := filepath.Join(te.dir, "pool_extfree")
	noExt := strings.TrimSuffix(te.c4mPath, ".c4m")

	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runPool([]string{noExt, bundleDir}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}
}

// ===========================================================================
// runLs: error paths
// ===========================================================================

func TestRunLs_BadC4mFile(t *testing.T) {
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	code := catchExit(func() { runLs([]string{"/nonexistent.c4m:path"}) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRunLs_BadBareC4m(t *testing.T) {
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	code := catchExit(func() { runLs([]string{"/nonexistent.c4m"}) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

// ===========================================================================
// runCat: additional error paths
// ===========================================================================

func TestRunCat_ExtensionFreeC4m(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_CONTEXT", "")
	withTestExit(t)

	// Trying to cat an extension-free c4m reference (project where project.c4m exists)
	noExt := strings.TrimSuffix(te.c4mPath, ".c4m")
	code := catchExit(func() { runCat([]string{noExt}) })
	if code != 1 {
		t.Errorf("expected exit 1 for extension-free c4m ref, got %d", code)
	}
}

// ===========================================================================
// enterContextAt / navigateWithin / exitContext (thin wrappers)
// ===========================================================================

func TestEnterContextAt_Success(t *testing.T) {
	te := newTestEnv(t)
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	enterContextAt(te.c4mPath, "")

	w.Close()
	os.Stdout = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "C4_CONTEXT=") {
		t.Errorf("expected C4_CONTEXT, got %q", out)
	}
}

func TestEnterContextAt_Error(t *testing.T) {
	withTestExit(t)
	code := catchExit(func() { enterContextAt("/nonexistent.c4m", "") })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestEnterContext_Success(t *testing.T) {
	te := newTestEnv(t)
	withTestExit(t)

	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	enterContext(te.c4mPath)

	w.Close()
	os.Stdout = old
}

func TestNavigateWithin_Success(t *testing.T) {
	te := newTestEnv(t)
	withTestExit(t)

	cur := &ctx.Context{C4mPath: te.c4mPath, CWD: ""}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	navigateWithin(cur, "sub")

	w.Close()
	os.Stdout = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "C4_CWD=") {
		t.Errorf("expected C4_CWD, got %q", out)
	}
}

func TestNavigateWithin_Error(t *testing.T) {
	te := newTestEnv(t)
	withTestExit(t)

	cur := &ctx.Context{C4mPath: te.c4mPath, CWD: ""}

	code := catchExit(func() { navigateWithin(cur, "nonexistent") })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestExitContext_Success(t *testing.T) {
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitContext("/tmp")

	w.Close()
	os.Stdout = old

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "unset C4_CONTEXT") {
		t.Error("expected unset")
	}
}

// ===========================================================================
// listPath / printEntries / printLongEntry (thin wrappers)
// ===========================================================================

func TestListPath_Success(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)
	withTestExit(t)

	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	listPath(m, "", false, true, true)

	w.Close()
	os.Stdout = old
}

func TestListPath_Error(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)
	withTestExit(t)

	code := catchExit(func() { listPath(m, "nonexistent", false, true, true) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestPrintEntries_Wrapper(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)
	entries := entriesAtPath(m, "")

	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	printEntries(entries, false, true, true)

	w.Close()
	os.Stdout = old
}

func TestPrintLongEntry_Wrapper(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)
	e := findEntry(m, "hello.txt")

	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	printLongEntry(e)

	w.Close()
	os.Stdout = old
}

// ===========================================================================
// catFromC4m (thin wrapper)
// ===========================================================================

func TestCatFromC4m_Wrapper(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_STORE", te.storePath)
	withTestExit(t)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	catFromC4m(te.c4mPath, "hello.txt")

	w.Close()
	os.Stdout = old

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	if string(buf[:n]) != "hello world" {
		t.Errorf("output = %q, want %q", string(buf[:n]), "hello world")
	}
}

func TestCatFromC4m_Error(t *testing.T) {
	te := newTestEnv(t)
	t.Setenv("C4_STORE", te.storePath)
	withTestExit(t)

	code := catchExit(func() { catFromC4m(te.c4mPath, "nonexistent.txt") })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRunComplete_OutsideContext(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("C4_CONTEXT", "")
	// Create a c4m file
	os.WriteFile(filepath.Join(dir, "project.c4m"), []byte{}, 0644)
	withTestExit(t)

	// Change working directory to the temp dir
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(origDir) })

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := catchExit(func() { runComplete([]string{"proj"}) })

	w.Close()
	os.Stdout = old

	if code != -1 {
		t.Fatalf("unexpected exit = %d", code)
	}

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "project.c4m") {
		t.Errorf("expected project.c4m completion, got %q", out)
	}
}
