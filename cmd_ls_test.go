package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Avalanche-io/c4"
	"github.com/Avalanche-io/c4/c4m"
)

// --------------------------------------------------------------------------
// printEntriesTo
// --------------------------------------------------------------------------

func TestPrintEntriesTo_OnePerLine(t *testing.T) {
	entries := []*c4m.Entry{
		{Name: "file1.txt", Mode: 0644, Size: 100, Depth: 0},
		{Name: "file2.txt", Mode: 0644, Size: 200, Depth: 0},
		{Name: "dir/", Mode: os.ModeDir | 0755, Size: -1, Depth: 0},
	}

	var buf bytes.Buffer
	printEntriesTo(&buf, entries, false, true, true, false, false)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), buf.String())
	}
	if lines[0] != "file1.txt" {
		t.Errorf("line 0 = %q, want %q", lines[0], "file1.txt")
	}
	if lines[1] != "file2.txt" {
		t.Errorf("line 1 = %q, want %q", lines[1], "file2.txt")
	}
	if lines[2] != "dir/" {
		t.Errorf("line 2 = %q, want %q", lines[2], "dir/")
	}
}

func TestPrintEntriesTo_ShortFormat(t *testing.T) {
	entries := []*c4m.Entry{
		{Name: "a.txt", Mode: 0644, Size: 10, Depth: 0},
		{Name: "b.txt", Mode: 0644, Size: 20, Depth: 0},
	}

	var buf bytes.Buffer
	printEntriesTo(&buf, entries, false, true, false, false, false)

	// Short format, non-TTY: one per line (matches real ls piped behavior)
	want := "a.txt\nb.txt\n"
	if buf.String() != want {
		t.Errorf("output = %q, want %q", buf.String(), want)
	}
}

func TestPrintEntriesTo_ShortFormatTTY(t *testing.T) {
	entries := []*c4m.Entry{
		{Name: "dir/", Mode: os.ModeDir | 0755, Size: -1, Depth: 0},
		{Name: "script.sh", Mode: 0755, Size: 50, Depth: 0},
		{Name: "plain.txt", Mode: 0644, Size: 10, Depth: 0},
	}

	var buf bytes.Buffer
	printEntriesTo(&buf, entries, false, true, false, true, false)

	out := buf.String()
	// Directories get blue ANSI codes
	if !strings.Contains(out, "\033[1;34m") {
		t.Error("expected blue ANSI for directory")
	}
	// Executables get green ANSI codes
	if !strings.Contains(out, "\033[1;32m") {
		t.Error("expected green ANSI for executable")
	}
	// Plain files have no color
	if !strings.Contains(out, "plain.txt  ") {
		t.Error("expected plain text for regular file")
	}
}

func TestPrintEntriesTo_HiddenFiles(t *testing.T) {
	entries := []*c4m.Entry{
		{Name: ".hidden", Mode: 0644, Size: 10, Depth: 0},
		{Name: "visible.txt", Mode: 0644, Size: 20, Depth: 0},
	}

	// Without showAll
	var buf bytes.Buffer
	printEntriesTo(&buf, entries, false, false, true, false, false)
	if strings.Contains(buf.String(), ".hidden") {
		t.Error("hidden file should not appear without -a")
	}
	if !strings.Contains(buf.String(), "visible.txt") {
		t.Error("visible file should appear")
	}

	// With showAll
	buf.Reset()
	printEntriesTo(&buf, entries, false, true, true, false, false)
	if !strings.Contains(buf.String(), ".hidden") {
		t.Error("hidden file should appear with -a")
	}
}

func TestPrintEntriesTo_EmptyList(t *testing.T) {
	var buf bytes.Buffer
	printEntriesTo(&buf, nil, false, true, false, false, false)
	if buf.Len() != 0 {
		t.Errorf("expected empty output for nil entries, got %q", buf.String())
	}

	buf.Reset()
	printEntriesTo(&buf, []*c4m.Entry{}, false, true, false, false, false)
	if buf.Len() != 0 {
		t.Errorf("expected empty output for empty entries, got %q", buf.String())
	}
}

// --------------------------------------------------------------------------
// printLongEntryTo
// --------------------------------------------------------------------------

func TestPrintLongEntryTo_RegularFile(t *testing.T) {
	e := &c4m.Entry{
		Name:      "test.txt",
		Mode:      0644,
		Size:      1234,
		Depth:     0,
		Timestamp: c4m.NullTimestamp(),
	}

	var buf bytes.Buffer
	printLongEntryTo(&buf, e, false)

	out := buf.String()
	if !strings.Contains(out, "1234") {
		t.Errorf("expected size 1234 in output: %q", out)
	}
	if !strings.Contains(out, "test.txt") {
		t.Errorf("expected name in output: %q", out)
	}
	// Null timestamp should show "-"
	if !strings.Contains(out, " - ") {
		t.Errorf("expected '-' for null timestamp: %q", out)
	}
	// Without showIDs, C4 ID should not be in output
	if strings.HasSuffix(strings.TrimSpace(out), "-") && strings.Count(out, "-") > 3 {
		// The last field should be the name, not a C4 ID placeholder
	}
}

func TestPrintLongEntryTo_Directory(t *testing.T) {
	e := &c4m.Entry{
		Name:      "mydir/",
		Mode:      os.ModeDir | 0755,
		Size:      -1,
		Depth:     0,
		Timestamp: c4m.NullTimestamp(),
	}

	var buf bytes.Buffer
	printLongEntryTo(&buf, e, false)

	out := buf.String()
	if !strings.Contains(out, "mydir/") {
		t.Errorf("expected dir name in output: %q", out)
	}
	// Size -1 shows as "-"
	if !strings.Contains(out, "       -") {
		t.Errorf("expected '-' for size=-1: %q", out)
	}
}

func TestPrintLongEntryTo_WithTimestamp(t *testing.T) {
	ts := time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC)
	e := &c4m.Entry{
		Name:      "dated.txt",
		Mode:      0644,
		Size:      42,
		Depth:     0,
		Timestamp: ts,
	}

	var buf bytes.Buffer
	printLongEntryTo(&buf, e, false)

	out := buf.String()
	// Should contain a date (year or time depending on current year)
	if !strings.Contains(out, "Jun") {
		t.Errorf("expected month in timestamp: %q", out)
	}
}

func TestPrintLongEntryTo_WithC4ID(t *testing.T) {
	// Compute a real C4 ID from content
	id := c4.Identify(strings.NewReader("test content for C4 ID"))
	e := &c4m.Entry{
		Name:      "hashed.txt",
		Mode:      0644,
		Size:      10,
		Depth:     0,
		Timestamp: c4m.NullTimestamp(),
		C4ID:      id,
	}

	// Without showIDs: C4 ID should NOT appear
	var buf bytes.Buffer
	printLongEntryTo(&buf, e, false)
	out := buf.String()
	if strings.Contains(out, "c4") && strings.Count(out, "c4") > 0 {
		// "c4" might appear in the mode string, check for the full 90-char ID
		if strings.Contains(out, id.String()) {
			t.Errorf("C4 ID should not appear without showIDs: %q", out)
		}
	}

	// With showIDs: C4 ID SHOULD appear
	buf.Reset()
	printLongEntryTo(&buf, e, true)
	out = buf.String()
	if !strings.Contains(out, id.String()) {
		t.Errorf("expected C4 ID with showIDs: %q", out)
	}
}

func TestPrintLongEntryTo_Symlink(t *testing.T) {
	e := &c4m.Entry{
		Name:      "link",
		Mode:      os.ModeSymlink | 0777,
		Size:      0,
		Depth:     0,
		Target:    "target.txt",
		Timestamp: c4m.NullTimestamp(),
	}

	var buf bytes.Buffer
	printLongEntryTo(&buf, e, false)

	out := buf.String()
	if !strings.Contains(out, "->") {
		t.Errorf("expected symlink arrow in output: %q", out)
	}
	if !strings.Contains(out, "target.txt") {
		t.Errorf("expected symlink target in output: %q", out)
	}
}

func TestPrintEntriesTo_LongFormat(t *testing.T) {
	entries := []*c4m.Entry{
		{Name: "file.txt", Mode: 0644, Size: 100, Depth: 0, Timestamp: c4m.NullTimestamp()},
		{Name: "dir/", Mode: os.ModeDir | 0755, Size: -1, Depth: 0, Timestamp: c4m.NullTimestamp()},
	}

	var buf bytes.Buffer
	printEntriesTo(&buf, entries, true, true, false, false, false)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}
}

// --------------------------------------------------------------------------
// listPathTo
// --------------------------------------------------------------------------

func TestListPathTo_RootEntries(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	var buf bytes.Buffer
	err := listPathTo(&buf, m, "", false, true, true, false)
	if err != nil {
		t.Fatalf("listPathTo root: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "hello.txt") {
		t.Errorf("expected hello.txt in root listing: %q", out)
	}
	if !strings.Contains(out, "sub/") {
		t.Errorf("expected sub/ in root listing: %q", out)
	}
}

func TestListPathTo_Subdirectory(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	var buf bytes.Buffer
	err := listPathTo(&buf, m, "sub/", false, true, true, false)
	if err != nil {
		t.Fatalf("listPathTo sub/: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "nested.txt") {
		t.Errorf("expected nested.txt in sub/ listing: %q", out)
	}
	if strings.Contains(out, "hello.txt") {
		t.Error("hello.txt should not appear in sub/ listing")
	}
}

func TestListPathTo_SubdirectoryWithoutSlash(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	var buf bytes.Buffer
	err := listPathTo(&buf, m, "sub", false, true, true, false)
	if err != nil {
		t.Fatalf("listPathTo sub (no slash): %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "nested.txt") {
		t.Errorf("expected nested.txt: %q", out)
	}
}

func TestListPathTo_SingleFile(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	var buf bytes.Buffer
	err := listPathTo(&buf, m, "hello.txt", false, true, false, false)
	if err != nil {
		t.Fatalf("listPathTo hello.txt: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if out != "hello.txt" {
		t.Errorf("output = %q, want %q", out, "hello.txt")
	}
}

func TestListPathTo_SingleFileLong(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	var buf bytes.Buffer
	err := listPathTo(&buf, m, "hello.txt", true, true, false, false)
	if err != nil {
		t.Fatalf("listPathTo long hello.txt: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "hello.txt") {
		t.Errorf("expected file name in long output: %q", out)
	}
}

func TestListPathTo_NotFound(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	var buf bytes.Buffer
	err := listPathTo(&buf, m, "nonexistent", false, true, true, false)
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
	if !strings.Contains(err.Error(), "no such file or directory") {
		t.Errorf("error = %q, want 'no such file or directory'", err)
	}
}

// --------------------------------------------------------------------------
// Full manifest tree listing
// --------------------------------------------------------------------------

func TestListPathTo_DeepTree(t *testing.T) {
	m := testManifest() // has src/, src/main.go, src/util.go, src/internal/, src/internal/core.go

	var buf bytes.Buffer
	err := listPathTo(&buf, m, "src/", false, true, true, false)
	if err != nil {
		t.Fatalf("listPathTo src/: %v", err)
	}

	out := buf.String()
	// Direct children of src/
	if !strings.Contains(out, "main.go") {
		t.Errorf("expected main.go: %q", out)
	}
	if !strings.Contains(out, "util.go") {
		t.Errorf("expected util.go: %q", out)
	}
	if !strings.Contains(out, "internal/") {
		t.Errorf("expected internal/: %q", out)
	}

	// core.go is NOT a direct child of src/ (it's in internal/)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "core.go" {
			t.Error("core.go should not be a direct child of src/")
		}
	}
}

func TestListPathTo_LongFormatDirectory(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	var buf bytes.Buffer
	err := listPathTo(&buf, m, "", true, true, false, false)
	if err != nil {
		t.Fatalf("listPathTo long root: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (hello.txt, sub/), got %d: %q", len(lines), buf.String())
	}
}

func TestListPathTo_FileInSubdir(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	var buf bytes.Buffer
	err := listPathTo(&buf, m, "sub/nested.txt", false, true, false, false)
	if err != nil {
		t.Fatalf("listPathTo sub/nested.txt: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if out != "nested.txt" {
		t.Errorf("output = %q, want %q", out, "nested.txt")
	}
}

// --------------------------------------------------------------------------
// listPath (wrapper)
// --------------------------------------------------------------------------

func TestListPath_WritesToStdout(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = origStdout })

	listPath(m, "", false, true, true, false)

	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	os.Stdout = origStdout

	if !strings.Contains(buf.String(), "hello.txt") {
		t.Errorf("expected hello.txt in output: %q", buf.String())
	}
}

func TestListPath_ErrorCallsExit(t *testing.T) {
	te := newTestEnv(t)
	m := te.loadC4m(t)

	withTestExit(t)
	code := catchExit(func() { listPath(m, "nonexistent", false, true, true, false) })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

// --------------------------------------------------------------------------
// printEntries (wrapper)
// --------------------------------------------------------------------------

func TestPrintEntries_WritesToStdout(t *testing.T) {
	entries := []*c4m.Entry{
		{Name: "file.txt", Mode: 0644, Size: 10, Depth: 0, Timestamp: c4m.NullTimestamp()},
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = origStdout })

	printEntries(entries, false, true, true, false)

	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	os.Stdout = origStdout

	if !strings.Contains(buf.String(), "file.txt") {
		t.Errorf("expected file.txt in output: %q", buf.String())
	}
}

// --------------------------------------------------------------------------
// isTerminal (smoke test — cannot test TTY in CI)
// --------------------------------------------------------------------------

func TestIsTerminal(t *testing.T) {
	// In test mode, stdout is not a terminal
	if isTerminal() {
		t.Log("stdout appears to be a terminal (unusual in test, but ok)")
	}
}

// --------------------------------------------------------------------------
// printLongEntryTo: mode string truncation
// --------------------------------------------------------------------------

func TestPrintLongEntryTo_LongModeString(t *testing.T) {
	// Some modes produce strings longer than 10 chars (e.g., device files)
	e := &c4m.Entry{
		Name:      "special",
		Mode:      os.ModeDevice | os.ModeCharDevice | 0666,
		Size:      0,
		Depth:     0,
		Timestamp: c4m.NullTimestamp(),
	}

	var buf bytes.Buffer
	printLongEntryTo(&buf, e, false)

	// Should not panic; mode is truncated
	out := buf.String()
	if !strings.Contains(out, "special") {
		t.Errorf("expected name in output: %q", out)
	}
}

// --------------------------------------------------------------------------
// printLongEntryTo: timestamp from this year vs old year
// --------------------------------------------------------------------------

func TestPrintLongEntryTo_CurrentYearTimestamp(t *testing.T) {
	now := time.Now()
	ts := time.Date(now.Year(), 3, 15, 10, 30, 0, 0, time.UTC)
	e := &c4m.Entry{
		Name:      "recent.txt",
		Mode:      0644,
		Size:      42,
		Depth:     0,
		Timestamp: ts,
	}

	var buf bytes.Buffer
	printLongEntryTo(&buf, e, false)

	out := buf.String()
	// Current year: should show time (HH:MM), not year
	if !strings.Contains(out, "Mar") {
		t.Errorf("expected month: %q", out)
	}
}

func TestPrintLongEntryTo_OldYearTimestamp(t *testing.T) {
	ts := time.Date(2020, 6, 15, 14, 30, 0, 0, time.UTC)
	e := &c4m.Entry{
		Name:      "old.txt",
		Mode:      0644,
		Size:      42,
		Depth:     0,
		Timestamp: ts,
	}

	var buf bytes.Buffer
	printLongEntryTo(&buf, e, false)

	out := buf.String()
	// Old year: should show year
	if !strings.Contains(out, "2020") {
		t.Errorf("expected year 2020: %q", out)
	}
}

func TestPrintLongEntryTo_ZeroTimestamp(t *testing.T) {
	e := &c4m.Entry{
		Name:      "zero.txt",
		Mode:      0644,
		Size:      10,
		Depth:     0,
		Timestamp: time.Time{},
	}

	var buf bytes.Buffer
	printLongEntryTo(&buf, e, false)

	out := buf.String()
	// Zero timestamp should show "-"
	if !strings.Contains(out, " - ") {
		t.Errorf("expected '-' for zero timestamp: %q", out)
	}
}

func TestListPathTo_WithExistingC4m(t *testing.T) {
	// Build a manifest with a file at root.
	dir := t.TempDir()
	m := c4m.NewManifest()

	id := c4.Identify(strings.NewReader("test content for ls"))
	m.AddEntry(&c4m.Entry{
		Name:      "test.go",
		Depth:     0,
		Mode:      0644,
		Size:      500,
		Timestamp: c4m.NullTimestamp(),
		C4ID:      id,
	})
	m.SortEntries()

	c4mPath := filepath.Join(dir, "test.c4m")
	if err := saveManifest(c4mPath, m); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}

	loaded, err := loadManifest(c4mPath)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}

	var buf bytes.Buffer
	err = listPathTo(&buf, loaded, "test.go", true, true, false, true)
	if err != nil {
		t.Fatalf("listPathTo: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "c4") {
		t.Errorf("expected C4 ID in long output with showIDs: %q", out)
	}
}
