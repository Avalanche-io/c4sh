package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Avalanche-io/c4/c4m"
)

// runMkdir implements "c4sh mkdir" — add directory entries to a c4m file.
//
// Operates directly on the c4m manifest: loads, adds directory entries, saves.
// No live directory involved.
func runMkdir(args []string) {
	var parents bool
	var paths []string
	for _, a := range args {
		if a == "-p" {
			parents = true
			continue
		}
		if strings.HasPrefix(a, "-") && !strings.Contains(a, ".c4m") {
			continue
		}
		paths = append(paths, a)
	}

	if len(paths) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: c4sh mkdir [-p] <dir>...\n")
		os.Exit(1)
	}

	// Resolve first path to determine c4m file
	c4mFile, _ := resolveContextPath(paths[0])
	if c4mFile == "" {
		// Not in c4m context — fall through to system mkdir
		fallthrough_("mkdir", args)
		return
	}

	m, err := loadManifest(c4mFile)
	if err != nil {
		die("mkdir: %v", err)
	}

	var errs int
	modified := false

	for _, p := range paths {
		pC4m, subPath := resolveContextPath(p)
		if pC4m == "" {
			pC4m = c4mFile
			subPath = p
		}
		if pC4m != c4mFile {
			fmt.Fprintf(os.Stderr, "c4sh: mkdir: cannot create across c4m files\n")
			errs++
			continue
		}
		if subPath == "" {
			if !parents {
				fmt.Fprintf(os.Stderr, "c4sh: mkdir: cannot create c4m root\n")
				errs++
			}
			continue
		}

		// Normalize: remove trailing slash for splitting
		clean := strings.TrimSuffix(subPath, "/")

		if parents {
			// Create all path components as needed
			if mkdirAll(m, clean) {
				modified = true
			}
		} else {
			if mkdirOne(m, clean, &errs) {
				modified = true
			}
		}
	}

	if modified {
		m.SortEntries()
		if err := saveManifest(c4mFile, m); err != nil {
			die("mkdir: %v", err)
		}
	}

	if errs > 0 {
		os.Exit(1)
	}
}

// mkdirOne creates a single directory entry. Returns true if the manifest was modified.
func mkdirOne(m *c4m.Manifest, dirPath string, errs *int) bool {
	// Check if it already exists
	existing, _ := findEntryByPath(m, dirPath+"/")
	if existing != nil {
		fmt.Fprintf(os.Stderr, "c4sh: mkdir: %s: already exists\n", dirPath)
		*errs++
		return false
	}

	// Determine parent and depth
	parent, name := splitDirName(dirPath)
	depth := 0
	if parent != "" {
		parentEntry, _ := findEntryByPath(m, parent+"/")
		if parentEntry == nil {
			fmt.Fprintf(os.Stderr, "c4sh: mkdir: %s: parent directory does not exist\n", dirPath)
			*errs++
			return false
		}
		depth = parentEntry.Depth + 1
	}

	entry := &c4m.Entry{
		Name:      name + "/",
		Depth:     depth,
		Mode:      os.ModeDir | 0755,
		Size:      -1,
		Timestamp: c4m.NullTimestamp(),
	}
	m.AddEntry(entry)
	return true
}

// mkdirAll creates a directory and all missing parent directories.
// Returns true if the manifest was modified.
func mkdirAll(m *c4m.Manifest, dirPath string) bool {
	// Split into components
	parts := strings.Split(dirPath, "/")
	modified := false

	for i := range parts {
		partial := strings.Join(parts[:i+1], "/")
		existing, _ := findEntryByPath(m, partial+"/")
		if existing != nil {
			continue
		}

		// Determine parent depth
		depth := 0
		if i > 0 {
			parentPath := strings.Join(parts[:i], "/")
			parentEntry, _ := findEntryByPath(m, parentPath+"/")
			if parentEntry != nil {
				depth = parentEntry.Depth + 1
			}
		}

		entry := &c4m.Entry{
			Name:      parts[i] + "/",
			Depth:     depth,
			Mode:      os.ModeDir | 0755,
			Size:      -1,
			Timestamp: c4m.NullTimestamp(),
		}
		m.AddEntry(entry)
		modified = true
	}

	return modified
}

// splitDirName splits "a/b/c" into ("a/b", "c").
// For a single component "foo", returns ("", "foo").
func splitDirName(p string) (parent, name string) {
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return "", p
	}
	return p[:i], p[i+1:]
}
