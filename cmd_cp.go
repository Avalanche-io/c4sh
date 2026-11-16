package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Avalanche-io/c4"
	"github.com/Avalanche-io/c4/c4m"
	"github.com/Avalanche-io/c4/reconcile"
	"github.com/Avalanche-io/c4/scan"
	"github.com/Avalanche-io/c4sh/internal/ctx"
)

// runCp implements "c4sh cp" — the universal verb for moving content across
// the boundary between real filesystems and c4m files.
//
// Three modes based on source/destination types:
//
//	cp ./project/ project.c4m:       → real → c4m  (capture)
//	cp project.c4m:shots/ ./out/     → c4m → real  (extract)
//	cp project.c4m:shots/ out.c4m:   → c4m → c4m   (instant)
func runCp(args []string) {
	// Strip flags (c4m operations are always recursive; other flags silently ignored)
	var filtered []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") && !strings.Contains(a, ".c4m") && !strings.Contains(a, ":") {
			continue
		}
		filtered = append(filtered, a)
	}

	if len(filtered) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: c4sh cp <source> <dest...>\n")
		osExit(1)
	}

	// When inside a c4m context, resolve bare relative paths as c4m references.
	cur := ctx.Current()
	if cur != nil {
		for i := range filtered {
			filtered[i] = resolveInContext(filtered[i], cur)
		}
	}

	// Multi-destination: 3+ args means first is source, rest are destinations.
	if len(filtered) >= 3 {
		src := filtered[0]
		dests := filtered[1:]
		cpMultiDest(src, dests)
		return
	}

	src := filtered[0]
	dst := filtered[1]

	srcIsC4m := isC4mPath(src)
	dstIsC4m := isC4mPath(dst)

	// If neither side is a c4m path, fall through to system cp
	if !srcIsC4m && !dstIsC4m {
		fallthrough_("cp", args)
		return
	}

	switch {
	case !srcIsC4m && dstIsC4m:
		cpRealToC4m(src, dst)
	case srcIsC4m && !dstIsC4m:
		cpC4mToReal(src, dst)
	case srcIsC4m && dstIsC4m:
		cpC4mToC4m(src, dst)
	}
}

// cpRealToC4m captures a real directory or file into a c4m file.
// Content goes to the store, structure goes to the c4m.
//
// Uses scan.Dir to produce the manifest, then stores content separately.
func cpRealToC4m(src, dst string) {
	src = expandHome(src)
	dstC4m, dstSub := splitC4mPath(dst)

	info, err := os.Stat(src)
	if err != nil {
		die("cp: %v", err)
	}

	store, err := openStore()
	if err != nil {
		die("cp: store: %v", err)
	}

	// Load or create destination manifest
	m, _ := loadManifest(dstC4m)
	if m == nil {
		m = c4m.NewManifest()
	}

	if !info.IsDir() {
		// Single file capture
		id, err := storeFile(store, src)
		if err != nil {
			die("cp: %v", err)
		}
		name := filepath.Base(src)
		entry := &c4m.Entry{
			Name:      name,
			Depth:     0,
			Mode:      info.Mode(),
			Size:      info.Size(),
			Timestamp: info.ModTime().UTC(),
			C4ID:      id,
		}
		if err := graftEntries(m, dstSub, nil, []*c4m.Entry{entry}); err != nil {
			die("cp: %v", err)
		}
	} else {
		// Directory capture — scan to produce manifest, then store content.
		scanned, err := scan.Dir(src, scan.WithMode(scan.ModeFull))
		if err != nil {
			die("cp: scan: %v", err)
		}

		// Store content for each file entry. The scanned manifest is in
		// walk order, so the kept entries are a block to graft.
		var block []*c4m.Entry
		for _, e := range scanned.Entries {
			cp := *e // shallow copy

			if e.IsDir() {
				block = append(block, &cp)
				continue
			}

			if !e.Mode.IsRegular() {
				continue
			}

			relPath := scanned.EntryPath(e)
			absPath := filepath.Join(src, filepath.FromSlash(relPath))
			id, sErr := storeFile(store, absPath)
			if sErr != nil {
				fmt.Fprintf(os.Stderr, "c4sh: cp: %s: %v\n", relPath, sErr)
				continue
			}
			cp.C4ID = id
			block = append(block, &cp)
		}
		// A folder the copy creates for the source takes the source's
		// observed mode and time.
		srcDir := &c4m.Entry{Name: info.Name() + "/", Mode: info.Mode(), Timestamp: info.ModTime().UTC()}
		if err := graftEntries(m, dstSub, srcDir, block); err != nil {
			die("cp: %v", err)
		}
	}

	if err := saveManifest(dstC4m, m); err != nil {
		die("cp: %v", err)
	}
}

// cpC4mToReal extracts content from a c4m file to the real filesystem.
//
// For full-manifest and subtree extractions, uses the reconcile package
// to plan and apply filesystem operations. This gets correct timestamp
// truncation, deepest-first directory timestamps, and symlink safety.
func cpC4mToReal(src, dst string) {
	dst = expandHome(dst)
	srcC4m, srcSub := splitC4mPath(src)

	m, err := loadManifest(srcC4m)
	if err != nil {
		die("cp: %v", err)
	}

	store, err := openStore()
	if err != nil {
		die("cp: store: %v", err)
	}

	// What the source names decides the kind of copy, never how many
	// entries it holds: a folder with one file is still a folder.
	target := subtreeRoot(m, srcSub)
	if srcSub != "" && target == nil {
		die("cp: %s: not found in %s", srcSub, srcC4m)
	}

	// A single file: write just that.
	if target != nil && !target.IsDir() {
		dstPath := dst
		if di, err := os.Stat(dst); err == nil && di.IsDir() {
			dstPath = filepath.Join(dst, target.Name)
		}
		if err := doExtractEntry(store, target, dstPath); err != nil {
			die("cp: %v", err)
		}
		return
	}

	// A folder (or the whole c4m): write its contents into dst.
	entries := entriesForSubtree(m, srcSub)
	if len(entries) == 0 {
		if err := os.MkdirAll(dst, 0755); err != nil {
			die("cp: %v", err)
		}
		return
	}

	// Build a sub-manifest from the resolved entries for reconcile.
	subM := c4m.NewManifest()
	for _, re := range entries {
		cp := *re.entry
		cp.Depth = re.depthOffset
		subM.AddEntry(&cp)
	}
	subM.SortEntries()

	// Use reconcile to materialize the manifest to the directory.
	// The store implements reconcile.ContentSource (Has + Open).
	r := reconcile.New(reconcile.WithSource(store))
	plan, err := r.Plan(subM, dst)
	if err != nil {
		die("cp: reconcile plan: %v", err)
	}
	if !plan.IsComplete() {
		// Report missing content but continue with what we have.
		for _, mid := range plan.Missing {
			fmt.Fprintf(os.Stderr, "c4sh: cp: missing content %s\n", mid)
		}
		die("cp: %d objects missing from store", len(plan.Missing))
	}

	result, err := r.Apply(plan, dst)
	if err != nil {
		die("cp: reconcile apply: %v", err)
	}
	for _, e := range result.Errors {
		fmt.Fprintf(os.Stderr, "c4sh: cp: %v\n", e)
	}
}

// cpC4mToC4m copies entries between c4m files. No content I/O — just
// manifest text manipulation. Content IDs stay the same.
func cpC4mToC4m(src, dst string) {
	srcC4m, srcSub := splitC4mPath(src)
	dstC4m, dstSub := splitC4mPath(dst)

	srcM, err := loadManifest(srcC4m)
	if err != nil {
		die("cp: %v", err)
	}

	// Load or create destination manifest
	dstM := srcM
	sameFile := srcC4m == dstC4m
	if !sameFile {
		dstM, _ = loadManifest(dstC4m)
		if dstM == nil {
			dstM = c4m.NewManifest()
		}
	}

	// A file is copied into dstSub; a folder (or the root) has its
	// contents copied, and a folder the copy creates for it takes the
	// source folder's mode and stated time.
	target := subtreeRoot(srcM, srcSub)
	if srcSub != "" && target == nil {
		die("cp: %s: not found in %s", srcSub, srcC4m)
	}
	var block []*c4m.Entry
	var srcDir *c4m.Entry
	if target != nil && !target.IsDir() {
		block = []*c4m.Entry{target}
	} else {
		srcDir = target
		for _, re := range entriesForSubtree(srcM, srcSub) {
			cp := *re.entry
			cp.Depth = re.depthOffset
			block = append(block, &cp)
		}
	}

	if err := graftEntries(dstM, dstSub, srcDir, block); err != nil {
		die("cp: %v", err)
	}
	if err := saveManifest(dstC4m, dstM); err != nil {
		die("cp: %v", err)
	}
}

// cpMultiDest copies a real source directory to multiple destinations
// simultaneously using reconcile.Distribute. Each source file is read
// once and fanned out to all targets in a single pass.
func cpMultiDest(src string, dests []string) {
	src = expandHome(src)
	info, err := os.Stat(src)
	if err != nil {
		die("cp: %v", err)
	}
	if !info.IsDir() {
		die("cp: multi-dest requires a directory source")
	}

	// Classify destinations and build reconcile targets.
	type c4mDest struct {
		c4mFile string
	}
	var targets []reconcile.Target
	var c4mDests []c4mDest // parallel to targets, only for c4m entries

	for _, d := range dests {
		if !isC4mPath(d) {
			targets = append(targets, reconcile.ToDir(expandHome(d)))
			c4mDests = append(c4mDests, c4mDest{})
			continue
		}
		c4mFile, _ := splitC4mPath(d)
		store, sErr := openStore()
		if sErr != nil {
			die("cp: store: %v", sErr)
		}
		targets = append(targets, reconcile.ToStore(store))
		c4mDests = append(c4mDests, c4mDest{c4mFile: c4mFile})
	}

	result, err := reconcile.Distribute(src, targets...)
	if err != nil {
		die("cp: %v", err)
	}

	// Report per-target errors.
	for _, tr := range result.Targets {
		for _, e := range tr.Errors {
			fmt.Fprintf(os.Stderr, "c4sh: cp: %s: %v\n", tr.Target, e)
		}
	}

	// Save manifest for each c4m destination.
	for i, cd := range c4mDests {
		if cd.c4mFile == "" {
			continue
		}
		_, sub := splitC4mPath(dests[i])

		// If the c4m destination has a subpath, wrap the manifest entries.
		m := result.Manifest
		if sub != "" {
			srcDir := &c4m.Entry{Name: info.Name() + "/", Mode: info.Mode(), Timestamp: info.ModTime().UTC()}
			wrapped, wErr := wrapManifestInSubpath(m, sub, srcDir)
			if wErr != nil {
				fmt.Fprintf(os.Stderr, "c4sh: cp: %v\n", wErr)
				continue
			}
			m = wrapped
		}

		if sErr := saveManifest(cd.c4mFile, m); sErr != nil {
			fmt.Fprintf(os.Stderr, "c4sh: cp: %v\n", sErr)
		}
	}
}

// wrapManifestInSubpath returns a new manifest holding m's entries under
// the folder sub, created as graftEntries creates it (the last folder
// takes srcDir's mode and time).
func wrapManifestInSubpath(m *c4m.Manifest, sub string, srcDir *c4m.Entry) (*c4m.Manifest, error) {
	wrapped := c4m.NewManifest()
	if err := graftEntries(wrapped, sub, srcDir, m.Entries); err != nil {
		return nil, err
	}
	return wrapped, nil
}

// graftEntries copies block (entries in walk order, its top level at depth
// 0) into m under the folder dstSub, "" for the root, as cp -R would:
//   - Missing folders on the way to dstSub are created. The last one is the
//     source folder's copy and takes srcDir's mode and stated time; the
//     others, and the last one when srcDir is nil, have no source, so their
//     time is null (never the clock) and their mode the default.
//   - An entry whose name is free is placed under its new parent; a file
//     replaces a file of the same name; a folder merges into a folder of the
//     same name, which keeps its own line.
//   - Every folder whose listing changed, and the folders above it, are
//     re-derived through the library (rederiveDirs).
//
// The block is copied first, so it may come from m itself.
func graftEntries(m *c4m.Manifest, dstSub string, srcDir *c4m.Entry, block []*c4m.Entry) error {
	copies := make([]*c4m.Entry, len(block))
	for i, e := range block {
		cp := *e
		copies[i] = &cp
	}
	parent, err := ensureDirPath(m, dstSub, srcDir)
	if err != nil {
		return err
	}
	// Into an empty folder (or an empty c4m) nothing can collide, so the
	// block goes in whole.
	empty := len(m.Entries) == 0
	if parent != nil {
		empty = len(m.Children(parent)) == 0
	}
	if empty {
		base := 0
		if parent != nil {
			base = parent.Depth + 1
		}
		for _, e := range copies {
			e.Depth += base
		}
		placeSubtree(m, parent, copies)
		rederiveDirs(m, parent)
		return nil
	}
	touched := []*c4m.Entry{parent}
	for _, sub := range splitTopLevel(copies) {
		t, err := mergeSubtree(m, parent, sub)
		if err != nil {
			return err
		}
		touched = append(touched, t...)
	}
	rederiveDirs(m, touched...)
	return nil
}

// ensureDirPath returns the folder dstSub names in m, creating any part of
// the path that is missing, as graftEntries describes. "" is the root (nil).
func ensureDirPath(m *c4m.Manifest, dstSub string, srcDir *c4m.Entry) (*c4m.Entry, error) {
	dstSub = strings.Trim(dstSub, "/")
	if dstSub == "" {
		return nil, nil
	}
	parts := strings.Split(dstSub, "/")
	var parent *c4m.Entry
	for i, part := range parts {
		if e := childNamed(m, parent, part+"/"); e != nil {
			parent = e
			continue
		}
		if childNamed(m, parent, part) != nil {
			return nil, fmt.Errorf("%s: not a directory", strings.Join(parts[:i+1], "/"))
		}
		e := &c4m.Entry{Name: part + "/", Mode: os.ModeDir | 0755, Timestamp: c4m.NullTimestamp()}
		if parent != nil {
			e.Depth = parent.Depth + 1
		}
		if i == len(parts)-1 && srcDir != nil {
			e.Mode, e.Timestamp = srcDir.Mode, srcDir.Timestamp
		}
		e.Size, _, e.C4ID = deriveDir(e, nil)
		placeSubtree(m, parent, []*c4m.Entry{e})
		parent = e
	}
	return parent, nil
}

// mergeSubtree puts sub (an entry and its descendants, top at depth 0)
// under parent, as graftEntries describes, and returns the folders whose
// listings changed.
func mergeSubtree(m *c4m.Manifest, parent *c4m.Entry, sub []*c4m.Entry) ([]*c4m.Entry, error) {
	top := sub[0]
	other := strings.TrimSuffix(top.Name, "/")
	if !top.IsDir() {
		other += "/"
	}
	if e := childNamed(m, parent, other); e != nil {
		return nil, fmt.Errorf("%s: cannot overwrite a %s with a %s", strings.TrimSuffix(m.EntryPath(e), "/"), kind(e), kind(top))
	}

	existing := childNamed(m, parent, top.Name)
	if existing != nil && existing.IsDir() {
		touched := []*c4m.Entry{existing}
		rest := sub[1:]
		for _, e := range rest {
			e.Depth--
		}
		for _, child := range splitTopLevel(rest) {
			t, err := mergeSubtree(m, existing, child)
			if err != nil {
				return nil, err
			}
			touched = append(touched, t...)
		}
		return touched, nil
	}
	if existing != nil {
		takeSubtree(m, existing)
	}
	base := 0
	if parent != nil {
		base = parent.Depth + 1
	}
	for _, e := range sub {
		e.Depth += base
	}
	placeSubtree(m, parent, sub)
	return []*c4m.Entry{parent}, nil
}

// splitTopLevel splits a block in walk order into its top-level subtrees.
func splitTopLevel(block []*c4m.Entry) [][]*c4m.Entry {
	var subs [][]*c4m.Entry
	for i, e := range block {
		if e.Depth == 0 || i == 0 {
			subs = append(subs, []*c4m.Entry{e})
			continue
		}
		subs[len(subs)-1] = append(subs[len(subs)-1], e)
	}
	return subs
}

// childNamed returns parent's child (a root entry when parent is nil)
// whose name is exactly name, or nil.
func childNamed(m *c4m.Manifest, parent *c4m.Entry, name string) *c4m.Entry {
	kids := m.Root()
	if parent != nil {
		kids = m.Children(parent)
	}
	for _, e := range kids {
		if e.Name == name {
			return e
		}
	}
	return nil
}

func kind(e *c4m.Entry) string {
	if e.IsDir() {
		return "folder"
	}
	return "file"
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// resolvedEntry holds an entry with its reconstructed relative path
// (relative to the subtree root being operated on) and depth offset.
type resolvedEntry struct {
	entry       *c4m.Entry
	relPath     string // path relative to subtree root
	depthOffset int    // depth relative to subtree root
}

// entriesForSubtree returns entries under a given subpath, with relative
// paths and depth offsets computed from the subtree root.
// If srcSub is empty, returns all entries at depth 0+.
// If srcSub points to a file, returns just that file.
// If srcSub points to a directory, returns the directory and all descendants.
func entriesForSubtree(m *c4m.Manifest, srcSub string) []resolvedEntry {
	if srcSub == "" {
		// All entries — return everything
		var result []resolvedEntry
		for _, e := range m.Entries {
			result = append(result, resolvedEntry{
				entry:       e,
				relPath:     entryFullPath(m, e),
				depthOffset: e.Depth,
			})
		}
		return result
	}

	target := subtreeRoot(m, srcSub)
	if target == nil {
		return nil
	}

	// Single file — return just this entry
	if !target.IsDir() {
		return []resolvedEntry{{
			entry:       target,
			relPath:     target.Name,
			depthOffset: 0,
		}}
	}

	// Directory — collect all descendants (not the directory itself)
	// so that "cp project.c4m:shots/ out.c4m:" copies shots' contents
	targetPath := strings.TrimSuffix(m.EntryPath(target), "/")
	baseDepth := target.Depth
	var result []resolvedEntry
	for _, e := range m.Descendants(target) {
		depthOff := e.Depth - baseDepth - 1
		full := entryFullPath(m, e)
		rel := strings.TrimPrefix(full, targetPath+"/")
		result = append(result, resolvedEntry{
			entry:       e,
			relPath:     rel,
			depthOffset: depthOff,
		})
	}
	return result
}

// subtreeRoot returns the entry srcSub names (a folder with or without its
// trailing slash), or nil for the c4m root or a name that isn't there.
func subtreeRoot(m *c4m.Manifest, srcSub string) *c4m.Entry {
	if srcSub == "" {
		return nil
	}
	if e := m.GetEntry(srcSub); e != nil {
		return e
	}
	return m.GetEntry(srcSub + "/")
}

// storeFile reads a file, stores its content, and returns the C4 ID.
func storeFile(store interface{ Put(io.Reader) (c4.ID, error) }, path string) (c4.ID, error) {
	f, err := os.Open(path)
	if err != nil {
		return c4.ID{}, err
	}
	defer f.Close()
	return store.Put(f)
}

// doExtractEntry extracts a single file entry from the store to disk.
// Returns an error instead of printing to stderr.
func doExtractEntry(store interface {
	Open(c4.ID) (io.ReadCloser, error)
}, e *c4m.Entry, dstPath string) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return err
	}

	if e.C4ID.IsNil() {
		// Empty file — create it with correct mode
		f, err := os.Create(dstPath)
		if err != nil {
			return err
		}
		f.Close()
		setFileMetadata(dstPath, e)
		return nil
	}

	rc, err := store.Open(e.C4ID)
	if err != nil {
		return fmt.Errorf("content not found for %s (%s)", e.Name, e.C4ID)
	}
	defer rc.Close()

	out, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return fmt.Errorf("write %s: %v", dstPath, err)
	}
	out.Close()
	setFileMetadata(dstPath, e)
	return nil
}

// setFileMetadata applies mode and timestamp from a c4m entry to a real file.
func setFileMetadata(path string, e *c4m.Entry) {
	mode := e.Mode.Perm()
	if mode != 0 {
		os.Chmod(path, mode)
	}
	if !e.Timestamp.Equal(c4m.NullTimestamp()) {
		os.Chtimes(path, e.Timestamp, e.Timestamp)
	}
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}
