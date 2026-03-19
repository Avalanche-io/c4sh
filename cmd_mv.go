package main

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/Avalanche-io/c4/c4m"
)

// runMv implements "c4sh mv" — move/rename entries within a c4m file.
//
// Operates directly on the c4m manifest: loads, modifies entries, saves.
// No live directory involved.
func runMv(args []string) {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: c4sh mv <source> <dest>\n")
		os.Exit(1)
	}

	src := args[len(args)-2]
	dst := args[len(args)-1]

	srcC4m, srcSub := resolveContextPath(src)
	dstC4m, dstSub := resolveContextPath(dst)

	if srcC4m == "" {
		// Not in c4m context and not a c4m path — fall through to system mv
		fallthrough_("mv", args)
		return
	}

	if dstC4m == "" {
		dstC4m = srcC4m
		dstSub = dst
	}

	if srcC4m != dstC4m {
		die("mv: cannot move between different c4m files")
	}

	if srcSub == "" {
		die("mv: cannot move the c4m root")
	}

	m, err := loadManifest(srcC4m)
	if err != nil {
		die("mv: %v", err)
	}

	// Find source entry by full path
	srcEntry, srcIdx := findEntryByPath(m, srcSub)
	if srcEntry == nil {
		die("mv: %s: not found", src)
	}

	// Determine effective destination path
	effDst := dstSub
	if effDst == "" {
		die("mv: cannot move to c4m root")
	}

	// If dest is an existing directory, move source inside it
	dstEntry, _ := findEntryByPath(m, strings.TrimSuffix(effDst, "/")+"/")
	if dstEntry == nil {
		dstEntry, _ = findEntryByPath(m, effDst)
	}
	if dstEntry != nil && dstEntry.IsDir() {
		effDst = path.Join(strings.TrimSuffix(effDst, "/"), strings.TrimSuffix(srcEntry.Name, "/"))
		if srcEntry.IsDir() {
			effDst += "/"
		}
		dstEntry = nil // recalculate below
	}

	// Check destination doesn't already exist (unless it's a directory we're moving into)
	if dstEntry != nil && dstEntry != srcEntry {
		die("mv: %s: already exists", dst)
	}

	// Parse destination into parent path and new name
	dstParent, dstName := path.Split(strings.TrimSuffix(effDst, "/"))
	dstParent = strings.TrimSuffix(dstParent, "/")
	if srcEntry.IsDir() {
		dstName += "/"
	}

	// Calculate destination depth
	dstDepth := 0
	if dstParent != "" {
		// Verify parent directory exists
		parentEntry, _ := findEntryByPath(m, dstParent+"/")
		if parentEntry == nil {
			die("mv: %s: parent directory does not exist", effDst)
		}
		dstDepth = parentEntry.Depth + 1
	}

	// Calculate depth delta for descendants
	depthDelta := dstDepth - srcEntry.Depth

	// Update the source entry
	srcEntry.Name = dstName
	srcEntry.Depth = dstDepth

	// If moving a directory, update all descendants' depths
	if srcEntry.IsDir() {
		descendants := collectDescendants(m, srcIdx)
		for _, desc := range descendants {
			desc.Depth += depthDelta
		}
	}

	// Re-sort and save
	m.SortEntries()
	if err := saveManifest(srcC4m, m); err != nil {
		die("mv: %v", err)
	}
}

// findEntryByPath finds an entry in the manifest by its full reconstructed path.
// Returns the entry and its index in m.Entries, or (nil, -1) if not found.
func findEntryByPath(m *c4m.Manifest, subPath string) (*c4m.Entry, int) {
	if subPath == "" {
		return nil, -1
	}
	var dirStack []string
	for i, e := range m.Entries {
		if e.Depth < len(dirStack) {
			dirStack = dirStack[:e.Depth]
		}
		var fullPath string
		if len(dirStack) > 0 {
			fullPath = strings.Join(dirStack, "") + e.Name
		} else {
			fullPath = e.Name
		}
		if e.IsDir() {
			for len(dirStack) <= e.Depth {
				dirStack = append(dirStack, "")
			}
			dirStack[e.Depth] = e.Name
		}
		if fullPath == subPath {
			return e, i
		}
	}
	return nil, -1
}

// collectDescendants returns all entries that are descendants of the entry
// at the given index (entries following it with greater depth, until depth
// drops back to the entry's depth or below).
func collectDescendants(m *c4m.Manifest, idx int) []*c4m.Entry {
	if idx < 0 || idx >= len(m.Entries) {
		return nil
	}
	parentDepth := m.Entries[idx].Depth
	var desc []*c4m.Entry
	for i := idx + 1; i < len(m.Entries); i++ {
		if m.Entries[i].Depth <= parentDepth {
			break
		}
		desc = append(desc, m.Entries[i])
	}
	return desc
}
