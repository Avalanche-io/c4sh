package main

import (
	"fmt"
	"os"
	"path"
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
	tmp, err := os.CreateTemp("", "c4sh-*.c4m")
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

// findEntry finds an entry in a manifest by its tree path.
// The subPath uses "/" separated components where directory names include
// their trailing slash (e.g., "shots/010/" or "shots/010/comp.exr").
// Returns nil if not found or if subPath is empty (root level).
func findEntry(m *c4m.Manifest, subPath string) *c4m.Entry {
	if subPath == "" {
		return nil // root level
	}
	return walkTreePath(m, subPath)
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
	e := walkTreePath(m, dirPath)
	if e == nil {
		return nil
	}
	return m.Children(e)
}

// walkTreePath traverses the manifest tree to find an entry by its full
// path. Each path component is matched against entry names at the
// corresponding tree level using Root() and Children().
func walkTreePath(m *c4m.Manifest, treePath string) *c4m.Entry {
	parts := splitPathComponents(treePath)
	if len(parts) == 0 {
		return nil
	}

	// Single component: search root entries directly
	if len(parts) == 1 {
		for _, e := range m.Root() {
			if e.Name == parts[0] {
				return e
			}
		}
		return nil
	}

	// Multi-component: walk the tree
	current := m.Root()
	for i, part := range parts {
		var found *c4m.Entry
		for _, e := range current {
			if e.Name == part {
				found = e
				break
			}
		}
		if found == nil {
			return nil
		}
		if i == len(parts)-1 {
			return found
		}
		if !found.IsDir() {
			return nil
		}
		current = m.Children(found)
	}
	return nil
}

// splitPathComponents splits a tree path into its name components.
// Directory components retain their trailing "/".
// Example: "shots/010/comp.exr" → ["shots/", "010/", "comp.exr"]
func splitPathComponents(p string) []string {
	var parts []string
	for p != "" {
		i := strings.IndexByte(p, '/')
		if i < 0 {
			parts = append(parts, p)
			break
		}
		parts = append(parts, p[:i+1])
		p = p[i+1:]
	}
	return parts
}

// entryFullPath reconstructs the full path of an entry from root.
func entryFullPath(m *c4m.Manifest, e *c4m.Entry) string {
	ancestors := m.Ancestors(e)
	parts := make([]string, 0, len(ancestors)+1)
	for _, a := range ancestors {
		parts = append(parts, strings.TrimSuffix(a.Name, "/"))
	}
	parts = append(parts, strings.TrimSuffix(e.Name, "/"))
	return strings.Join(parts, "/")
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

// die prints an error and exits.
func die(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "c4sh: "+format+"\n", args...)
	os.Exit(1)
}
