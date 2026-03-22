package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	if store == nil {
		die("cp: no content store configured (set C4_STORE or run c4 init)")
	}

	// Load or create destination manifest
	m, _ := loadManifest(dstC4m)
	if m == nil {
		m = c4m.NewManifest()
	}

	// Determine the base depth for new entries based on dstSub
	baseDepth := 0
	if dstSub != "" {
		baseDepth = strings.Count(strings.TrimSuffix(dstSub, "/"), "/") + 1
		ensureParentDirs(m, dstSub)
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
			Depth:     baseDepth,
			Mode:      info.Mode(),
			Size:      info.Size(),
			Timestamp: info.ModTime().UTC(),
			C4ID:      id,
		}
		m.AddEntry(entry)
	} else {
		// Directory capture — scan to produce manifest, then store content.
		scanned, err := scan.Dir(src, scan.WithMode(scan.ModeFull))
		if err != nil {
			die("cp: scan: %v", err)
		}

		// Store content for each file entry and merge into destination manifest.
		// The scanned manifest is already in correct hierarchical order.
		// Use EntryPath() from the manifest for full path resolution.
		for _, e := range scanned.Entries {
			cp := *e // shallow copy
			cp.Depth = baseDepth + e.Depth

			if e.IsDir() {
				m.AddEntry(&cp)
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
			m.AddEntry(&cp)
		}
	}

	m.SortEntries()
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
	if store == nil {
		die("cp: no content store configured (set C4_STORE or run c4 init)")
	}

	// Collect entries to extract
	entries := entriesForSubtree(m, srcSub)
	if len(entries) == 0 {
		die("cp: %s: not found in %s", srcSub, srcC4m)
	}

	// If srcSub points to a single file entry, extract just that
	if len(entries) == 1 && !entries[0].entry.IsDir() {
		e := entries[0].entry
		dstPath := dst
		if di, err := os.Stat(dst); err == nil && di.IsDir() {
			dstPath = filepath.Join(dst, e.Name)
		}
		extractEntry(store, e, dstPath)
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

	// Collect source entries
	entries := entriesForSubtree(srcM, srcSub)
	if len(entries) == 0 {
		die("cp: %s: not found in %s", srcSub, srcC4m)
	}

	// Calculate destination base depth
	dstBaseDepth := 0
	if dstSub != "" {
		dstBaseDepth = strings.Count(strings.TrimSuffix(dstSub, "/"), "/") + 1
		ensureParentDirs(dstM, dstSub)
	}

	// Copy entries with adjusted depth
	for _, re := range entries {
		cp := *re.entry // shallow copy
		cp.Depth = dstBaseDepth + re.depthOffset
		dstM.AddEntry(&cp)
	}

	dstM.SortEntries()
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
		if store == nil {
			die("cp: no content store configured (set C4_STORE or run c4 init)")
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
			m = wrapManifestInSubpath(m, sub)
		}

		if sErr := saveManifest(cd.c4mFile, m); sErr != nil {
			fmt.Fprintf(os.Stderr, "c4sh: cp: %v\n", sErr)
		}
	}
}

// wrapManifestInSubpath creates a new manifest with all entries shifted
// under the given subpath. Parent directories are created as needed.
func wrapManifestInSubpath(m *c4m.Manifest, sub string) *c4m.Manifest {
	wrapped := c4m.NewManifest()
	baseDepth := strings.Count(strings.TrimSuffix(sub, "/"), "/") + 1
	ensureParentDirs(wrapped, sub)
	for _, e := range m.Entries {
		cp := *e
		cp.Depth = baseDepth + e.Depth
		wrapped.AddEntry(&cp)
	}
	wrapped.SortEntries()
	return wrapped
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

	// Find the target entry by full path
	target := m.GetEntry(srcSub)
	if target == nil {
		// Try with trailing slash for directories
		target = m.GetEntry(srcSub + "/")
	}
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

// storeFile reads a file, stores its content, and returns the C4 ID.
func storeFile(store interface{ Put(io.Reader) (c4.ID, error) }, path string) (c4.ID, error) {
	f, err := os.Open(path)
	if err != nil {
		return c4.ID{}, err
	}
	defer f.Close()
	return store.Put(f)
}

// extractEntry extracts a single file entry from the store to disk.
func extractEntry(store interface {
	Open(c4.ID) (io.ReadCloser, error)
}, e *c4m.Entry, dstPath string) {
	if err := doExtractEntry(store, e, dstPath); err != nil {
		fmt.Fprintf(os.Stderr, "c4sh: cp: %v\n", err)
	}
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

// ensureParentDirs adds directory entries for all path components of subPath
// that don't already exist in the manifest.
func ensureParentDirs(m *c4m.Manifest, subPath string) {
	parts := strings.Split(strings.TrimSuffix(subPath, "/"), "/")
	for i := range parts {
		partial := strings.Join(parts[:i+1], "/") + "/"
		if m.GetEntry(partial) != nil {
			continue
		}
		m.AddEntry(&c4m.Entry{
			Name:      parts[i] + "/",
			Depth:     i,
			Mode:      os.ModeDir | 0755,
			Size:      -1,
			Timestamp: time.Now().UTC(),
		})
	}
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}
