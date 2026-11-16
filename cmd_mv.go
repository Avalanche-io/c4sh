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
		osExit(1)
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

	if err := mvEntry(m, srcSub, dstSub); err != nil {
		die("mv: %v", err)
	}

	if err := saveManifest(srcC4m, m); err != nil {
		die("mv: %v", err)
	}
}

// mvEntry moves or renames an entry within a manifest. The manifest is
// modified in place via MoveEntry (entry renamed, depths adjusted, re-sorted).
// srcSub and dstSub are paths relative to the c4m root.
func mvEntry(m *c4m.Manifest, srcSub, dstSub string) error {
	// Find source entry by full path. Directory entries are keyed with a
	// trailing slash and the shell path resolution strips it, so try the
	// path as given, then the directory spelling (as rm and cp do).
	srcEntry := m.GetEntry(srcSub)
	if srcEntry == nil {
		srcEntry = m.GetEntry(strings.TrimSuffix(srcSub, "/") + "/")
	}
	if srcEntry == nil {
		return fmt.Errorf("%s: not found", srcSub)
	}

	// Determine effective destination path
	effDst := dstSub
	if effDst == "" {
		return fmt.Errorf("cannot move to c4m root")
	}

	// If dest is an existing directory, move source inside it
	dstEntry := m.GetEntry(strings.TrimSuffix(effDst, "/") + "/")
	if dstEntry == nil {
		dstEntry = m.GetEntry(effDst)
	}
	if dstEntry != nil && dstEntry.IsDir() {
		effDst = path.Join(strings.TrimSuffix(effDst, "/"), strings.TrimSuffix(srcEntry.Name, "/"))
		if srcEntry.IsDir() {
			effDst += "/"
		}
		// The name inside the folder may be taken too.
		dstEntry = m.GetEntry(strings.TrimSuffix(effDst, "/"))
		if dstEntry == nil {
			dstEntry = m.GetEntry(strings.TrimSuffix(effDst, "/") + "/")
		}
	}

	// Check destination doesn't already exist (unless it's a directory we're moving into)
	if dstEntry != nil && dstEntry != srcEntry {
		return fmt.Errorf("%s: already exists", strings.TrimSuffix(effDst, "/"))
	}

	// Parse destination into parent path and new name
	dstParent, dstName := path.Split(strings.TrimSuffix(effDst, "/"))
	dstParent = strings.TrimSuffix(dstParent, "/")
	if srcEntry.IsDir() {
		dstName += "/"
	}

	// Find the new parent entry (nil for root level)
	var newParent *c4m.Entry
	if dstParent != "" {
		newParent = m.GetEntry(dstParent + "/")
		if newParent == nil {
			return fmt.Errorf("%s: parent directory does not exist", effDst)
		}
	}

	// A folder can't move into itself or below itself.
	if newParent != nil && srcEntry.IsDir() {
		if newParent == srcEntry {
			return fmt.Errorf("cannot move %s into itself", srcSub)
		}
		for _, a := range m.Ancestors(newParent) {
			if a == srcEntry {
				return fmt.Errorf("cannot move %s into itself", srcSub)
			}
		}
	}

	// Take the entry and its subtree out, give it its new name and
	// depths, and put it directly under its new parent. c4m.MoveEntry
	// changes the depths but leaves the entries where they are, so the
	// c4m package reads the moved entry as a child of whichever folder
	// precedes it.
	oldParent := m.Parent(srcEntry)
	targetDepth := 0
	if newParent != nil {
		targetDepth = newParent.Depth + 1
	}
	block := takeSubtree(m, srcEntry)
	delta := targetDepth - srcEntry.Depth
	for _, e := range block {
		e.Depth += delta
	}
	srcEntry.Name = dstName
	placeSubtree(m, newParent, block)

	// Both folders' listings changed, so their sizes and IDs (and their
	// ancestors') are re-derived. The moved entry's own values don't
	// depend on where it sits.
	rederiveDirs(m, oldParent, newParent)
	return nil
}
