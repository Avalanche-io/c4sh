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

	// Each path names an entry of the same c4m: an explicit c4m:path, or
	// a path relative to the current folder inside the c4m.
	var subPaths []string
	errs := 0
	for _, p := range paths {
		pC4m, sub := resolveContextPath(p)
		if pC4m == "" {
			pC4m, sub = c4mFile, p
		}
		if pC4m != c4mFile {
			fmt.Fprintf(os.Stderr, "c4sh: rm: %s: cannot remove across c4m files\n", p)
			errs++
			continue
		}
		if sub == "" {
			fmt.Fprintf(os.Stderr, "c4sh: rm: cannot remove the c4m root\n")
			errs++
			continue
		}
		subPaths = append(subPaths, sub)
	}

	modified, rmErrs := rmEntries(m, subPaths, recursive, force)
	errs += rmErrs

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
				fmt.Fprintf(os.Stderr, "c4sh: rm: %s: not found\n", strings.TrimSuffix(subPath, "/"))
				errs++
			}
			continue
		}

		if entry.IsDir() && !recursive {
			fmt.Fprintf(os.Stderr, "c4sh: rm: %s: is a directory\n", strings.TrimSuffix(subPath, "/"))
			errs++
			continue
		}

		// Remove the entry and everything below it. The folder it was in
		// has a new listing, so it and the folders above it are re-derived.
		parent := m.Parent(entry)
		takeSubtree(m, entry)
		rederiveDirs(m, parent)
		modified = true
	}
	return
}
