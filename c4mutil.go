package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

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
