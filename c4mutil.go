package main

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Avalanche-io/c4"
	"github.com/Avalanche-io/c4/c4m"
	c4store "github.com/Avalanche-io/c4/store"
)

// loadManifest reads and decodes a c4m file.
func loadManifest(c4mPath string) (*c4m.Manifest, error) {
	f, err := os.Open(c4mPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return c4m.NewDecoder(f).Decode()
}

// saveManifest atomically writes a manifest to a c4m file.
func saveManifest(c4mPath string, m *c4m.Manifest) error {
	tmp, err := os.CreateTemp(filepath.Dir(c4mPath), ".c4sh-*.c4m")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	enc := c4m.NewEncoder(tmp)
	if err := enc.Encode(m); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	tmp.Close()

	return os.Rename(tmpPath, c4mPath)
}

// openStore opens the configured content store.
// Supports local filesystem, S3, and multi-store configurations.
func openStore() (c4store.Store, error) {
	s, err := c4store.OpenStore()
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, fmt.Errorf("no content store configured (set C4_STORE or add store to ~/.c4/config)")
	}
	return s, nil
}

// resolveContextPath resolves a user path within the current c4m context.
// If the user is in a c4m context (C4_CONTEXT set), relative paths are
// resolved against C4_CWD. Returns (c4mFile, subPath).
func resolveContextPath(userPath string) (c4mFile string, subPath string) {
	// Explicit c4m path (has colon or .c4m suffix)
	if strings.Contains(userPath, ":") || strings.HasSuffix(userPath, ".c4m") {
		return splitC4mPath(userPath)
	}

	// Check if path.c4m exists (extension-free reference)
	if _, err := os.Stat(userPath + ".c4m"); err == nil {
		return userPath + ".c4m", ""
	}

	// If in c4m context, resolve relative to current c4m position
	ctx := os.Getenv("C4_CONTEXT")
	cwd := os.Getenv("C4_CWD")
	if ctx != "" {
		resolved := path.Clean(path.Join(cwd, userPath))
		if resolved == "." {
			resolved = ""
		}
		return ctx, resolved
	}

	// Not a c4m path
	return "", userPath
}

// findEntry finds an entry in a manifest by its full path.
// Returns nil if not found or if subPath is empty (root level).
func findEntry(m *c4m.Manifest, subPath string) *c4m.Entry {
	if subPath == "" {
		return nil // root level
	}
	return m.GetEntry(subPath)
}

// entriesAtPath returns direct children at a given path within the manifest.
// An empty subPath returns root-level entries. The subPath should not have
// a leading slash (e.g., "shots/010/" not "/shots/010/").
func entriesAtPath(m *c4m.Manifest, subPath string) []*c4m.Entry {
	if subPath == "" {
		return m.Root()
	}
	// Ensure directory paths end with /
	dirPath := subPath
	if !strings.HasSuffix(dirPath, "/") {
		dirPath += "/"
	}
	e := m.GetEntry(dirPath)
	if e == nil {
		return nil
	}
	return m.Children(e)
}

// entryFullPath returns the full path of an entry, stripping the
// trailing slash for directories.
func entryFullPath(m *c4m.Manifest, e *c4m.Entry) string {
	return strings.TrimSuffix(m.EntryPath(e), "/")
}

// takeSubtree removes e and every entry below it from m and returns them in
// walk order. A c4m lists a folder's descendants directly after its line,
// each deeper than it, so the subtree is the run of entries that follows e
// until one is at e's depth or shallower.
func takeSubtree(m *c4m.Manifest, e *c4m.Entry) []*c4m.Entry {
	i := entryIndex(m, e)
	j := i + 1
	for j < len(m.Entries) && m.Entries[j].Depth > e.Depth {
		j++
	}
	block := append([]*c4m.Entry(nil), m.Entries[i:j]...)
	m.Entries = append(m.Entries[:i:i], m.Entries[j:]...)
	m.InvalidateIndex()
	return block
}

// placeSubtree puts block (an entry and its descendants in walk order,
// depths already set for the new place) under parent, or at the root when
// parent is nil, and restores canonical order. The c4m package works out
// an entry's parent from where it sits in Entries, so the block goes
// directly after the parent's line: appended to the end, or left where it
// was with only its depth changed, it would land under whichever folder
// happened to precede it.
func placeSubtree(m *c4m.Manifest, parent *c4m.Entry, block []*c4m.Entry) {
	at := 0
	if parent != nil {
		at = entryIndex(m, parent) + 1
	}
	entries := make([]*c4m.Entry, 0, len(m.Entries)+len(block))
	entries = append(entries, m.Entries[:at]...)
	entries = append(entries, block...)
	entries = append(entries, m.Entries[at:]...)
	m.Entries = entries
	m.InvalidateIndex()
	m.SortEntries()
}

func entryIndex(m *c4m.Manifest, e *c4m.Entry) int {
	for i, x := range m.Entries {
		if x == e {
			return i
		}
	}
	panic("c4sh: entry is not in the c4m")
}

// rederiveDirs re-derives the size and C4 ID of each folder in dirs and of
// all its ancestors. Both are functions of a folder's listed children (the
// spec's "Computing Directory C4 IDs"), so an edit below a folder makes
// them stale all the way up. A folder's timestamp is its own modification
// time, which its children don't determine, so a stated one is kept; a
// null one is filled from the newest child, as c4m.PropagateMetadata (and
// so a scan) fills it. Nil entries (the root) are skipped.
func rederiveDirs(m *c4m.Manifest, dirs ...*c4m.Entry) {
	stale := make(map[*c4m.Entry]bool)
	for _, d := range dirs {
		if d == nil {
			continue
		}
		stale[d] = true
		for _, a := range m.Ancestors(d) {
			stale[a] = true
		}
	}
	// Walk order puts every folder before its descendants, so walking it
	// backwards derives each folder after the folders below it.
	for i := len(m.Entries) - 1; i >= 0; i-- {
		d := m.Entries[i]
		if !stale[d] {
			continue
		}
		d.Size, d.Timestamp, d.C4ID = deriveDir(d, m.Children(d))
	}
}

// deriveDir returns the size, timestamp and C4 ID that dir's listed
// children give it, by the library's rules. The size and a null timestamp
// are what c4m.PropagateMetadata computes for the folder over its direct
// children; a size is null if any child's is. The C4 ID is the C4 ID of
// the children's canonical one-level listing, as Manifest.WriteCanonical
// writes it. No children is an empty folder: size 0, the timestamp as
// stated, and the C4 ID of the empty listing. The children are not
// modified.
func deriveDir(dir *c4m.Entry, children []*c4m.Entry) (int64, time.Time, c4.ID) {
	listing := c4m.NewManifest()
	scope := []*c4m.Entry{{Name: dir.Name, Depth: 0, Mode: dir.Mode, Size: -1, Timestamp: dir.Timestamp}}
	nullChildSize := false
	for _, c := range children {
		at0, at1 := *c, *c
		at0.Depth, at1.Depth = 0, 1
		listing.AddEntry(&at0)
		scope = append(scope, &at1)
		if c.Size < 0 {
			nullChildSize = true
		}
	}
	c4m.PropagateMetadata(scope)
	size := scope[0].Size
	if nullChildSize {
		// PropagateMetadata reads a childless folder with a null size as
		// empty; here its listing is elsewhere, so the size is unknown.
		size = -1
	}
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(listing.WriteCanonical(pw)) }()
	return size, scope[0].Timestamp, c4.Identify(pr)
}

// storeRoot returns the filesystem root of a store, if it has one.
// Returns "" for S3 or other non-filesystem stores.
func storeRoot(s c4store.Store) string {
	type rooter interface{ Root() string }
	if r, ok := s.(rooter); ok {
		return r.Root()
	}
	return ""
}

// osExit is the function used to exit the process. It can be replaced
// in tests to prevent actual exits.
var osExit = os.Exit

// die prints an error and exits.
func die(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "c4sh: "+format+"\n", args...)
	osExit(1)
}
