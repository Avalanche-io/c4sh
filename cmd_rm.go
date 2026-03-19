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
		os.Exit(1)
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

	var errs int
	modified := false

	for _, p := range paths {
		pC4m, subPath := resolveContextPath(p)
		if pC4m == "" {
			pC4m = c4mFile
			subPath = p
		}
		if pC4m != c4mFile {
			fmt.Fprintf(os.Stderr, "c4sh: rm: cannot remove across c4m files\n")
			errs++
			continue
		}
		if subPath == "" {
			fmt.Fprintf(os.Stderr, "c4sh: rm: refusing to remove c4m root\n")
			errs++
			continue
		}

		// Try both as file and directory
		entry, idx := findEntryByPath(m, subPath)
		if entry == nil {
			// Try with trailing slash for directories
			entry, idx = findEntryByPath(m, strings.TrimSuffix(subPath, "/")+"/")
		}
		if entry == nil {
			if !force {
				fmt.Fprintf(os.Stderr, "c4sh: rm: %s: not found\n", p)
				errs++
			}
			continue
		}

		if entry.IsDir() && !recursive {
			fmt.Fprintf(os.Stderr, "c4sh: rm: %s: is a directory (use -r)\n", p)
			errs++
			continue
		}

		// Collect entries to remove: the entry itself + descendants
		toRemove := map[int]bool{idx: true}
		if entry.IsDir() {
			for i := idx + 1; i < len(m.Entries); i++ {
				if m.Entries[i].Depth <= entry.Depth {
					break
				}
				toRemove[i] = true
			}
		}

		// Remove in reverse order to preserve indices
		newEntries := make([]*c4m.Entry, 0, len(m.Entries)-len(toRemove))
		for i, e := range m.Entries {
			if !toRemove[i] {
				newEntries = append(newEntries, e)
			}
		}
		m.Entries = newEntries
		m.InvalidateIndex()
		modified = true
	}

	if modified {
		if err := saveManifest(c4mFile, m); err != nil {
			die("rm: %v", err)
		}
	}

	if errs > 0 {
		os.Exit(1)
	}
}
