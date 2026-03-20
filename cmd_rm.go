package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Avalanche-io/c4/c4m"
)

// runRm implements "c4sh rm" — remove entries from a c4m file.
//
// Operates directly on the c4m manifest: loads, removes matching entries, saves.
// No live directory involved.
func runRm(args []string) {
	var recursive, force bool
	var paths []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") && !strings.Contains(a, ".c4m") {
			for _, ch := range a[1:] {
				switch ch {
				case 'r', 'R':
					recursive = true
				case 'f':
					force = true
				}
			}
			continue
		}
		paths = append(paths, a)
	}

	if len(paths) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: c4sh rm [-rf] <path>...\n")
		osExit(1)
	}

	// Resolve first path to determine c4m file
	c4mFile, _ := resolveContextPath(paths[0])
	if c4mFile == "" {
		// Not in c4m context — fall through to system rm
		fallthrough_("rm", args)
		return
	}

	m, err := loadManifest(c4mFile)
	if err != nil {
		die("rm: %v", err)
	}

	modified, errs := rmEntries(m, paths, recursive, force)

	if modified {
		if err := saveManifest(c4mFile, m); err != nil {
			die("rm: %v", err)
		}
	}

	if errs > 0 {
		osExit(1)
	}
}

// rmEntries removes entries from a manifest by subpath. Returns whether the
// manifest was modified and the number of errors encountered.
// Each path in subPaths is treated as a subpath within the manifest (not a
// full c4m-qualified path).
func rmEntries(m *c4m.Manifest, subPaths []string, recursive, force bool) (modified bool, errs int) {
	for _, subPath := range subPaths {
		// Try both as file and directory
		entry := m.GetEntry(subPath)
		if entry == nil {
			// Try with trailing slash for directories
			entry = m.GetEntry(strings.TrimSuffix(subPath, "/") + "/")
		}
		if entry == nil {
			if !force {
				errs++
			}
			continue
		}

		if entry.IsDir() && !recursive {
			errs++
			continue
		}

		// Collect entries to remove: the entry itself + descendants
		toRemove := map[*c4m.Entry]bool{entry: true}
		if entry.IsDir() {
			for _, d := range m.Descendants(entry) {
				toRemove[d] = true
			}
		}

		// Remove entries
		newEntries := make([]*c4m.Entry, 0, len(m.Entries)-len(toRemove))
		for _, e := range m.Entries {
			if !toRemove[e] {
				newEntries = append(newEntries, e)
			}
		}
		m.Entries = newEntries
		m.InvalidateIndex()
		modified = true
	}
	return
}
