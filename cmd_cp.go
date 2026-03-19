package main

import (
	"crypto/sha512"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Avalanche-io/c4"
	"github.com/Avalanche-io/c4/c4m"
	c4store "github.com/Avalanche-io/c4/store"
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
	// Strip -r/-R flags (always recursive for c4m operations, accept silently)
	var filtered []string
	for _, a := range args {
		if a == "-r" || a == "-R" || a == "--recursive" {
			continue
		}
		filtered = append(filtered, a)
	}

	if len(filtered) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: c4sh cp <source> <dest...>\n")
		os.Exit(1)
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
		baseDepth = strings.Count(dstSub, "/") + 1
		// Ensure parent directories exist in the manifest
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
		// Directory capture — walk the tree
		srcBase := src
		err := filepath.Walk(srcBase, func(p string, fi os.FileInfo, walkErr error) error {
			if walkErr != nil {
				fmt.Fprintf(os.Stderr, "c4sh: cp: %v\n", walkErr)
				return nil
			}

			rel, _ := filepath.Rel(srcBase, p)
			if rel == "." {
				return nil // skip root itself
			}

			// Calculate depth from relative path
			parts := strings.Split(rel, string(filepath.Separator))
			depth := baseDepth + len(parts) - 1

			name := fi.Name()

			if fi.IsDir() {
				entry := &c4m.Entry{
					Name:      name + "/",
					Depth:     depth,
					Mode:      fi.Mode() | os.ModeDir,
					Size:      -1, // computed by propagation
					Timestamp: fi.ModTime().UTC(),
				}
				m.AddEntry(entry)
				return nil
			}

			// Skip non-regular files
			if !fi.Mode().IsRegular() {
				return nil
			}

			id, err := storeFile(store, p)
			if err != nil {
				fmt.Fprintf(os.Stderr, "c4sh: cp: %s: %v\n", rel, err)
				return nil
			}

			entry := &c4m.Entry{
				Name:      name,
				Depth:     depth,
				Mode:      fi.Mode(),
				Size:      fi.Size(),
				Timestamp: fi.ModTime().UTC(),
				C4ID:      id,
			}
			m.AddEntry(entry)
			return nil
		})
		if err != nil {
			die("cp: walk: %v", err)
		}
	}

	m.SortEntries()
	if err := saveManifest(dstC4m, m); err != nil {
		die("cp: %v", err)
	}
}

// cpC4mToReal extracts content from a c4m file to the real filesystem.
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
		// If dst is an existing directory, place file inside it
		if di, err := os.Stat(dst); err == nil && di.IsDir() {
			dstPath = filepath.Join(dst, e.Name)
		}
		extractEntry(store, e, dstPath)
		return
	}

	// Extract subtree
	if err := os.MkdirAll(dst, 0755); err != nil {
		die("cp: %v", err)
	}

	for _, re := range entries {
		dstPath := filepath.Join(dst, re.relPath)
		if re.entry.IsDir() {
			mode := re.entry.Mode.Perm()
			if mode == 0 {
				mode = 0755
			}
			if err := os.MkdirAll(dstPath, mode); err != nil {
				fmt.Fprintf(os.Stderr, "c4sh: cp: %v\n", err)
			}
			continue
		}
		extractEntry(store, re.entry, dstPath)
	}

	// Set directory timestamps in reverse order (deepest first)
	for i := len(entries) - 1; i >= 0; i-- {
		re := entries[i]
		if !re.entry.IsDir() {
			continue
		}
		if re.entry.Timestamp.Equal(c4m.NullTimestamp()) {
			continue
		}
		dstPath := filepath.Join(dst, re.relPath)
		os.Chtimes(dstPath, re.entry.Timestamp, re.entry.Timestamp)
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
		dstBaseDepth = strings.Count(dstSub, "/") + 1
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

// multiC4mDest tracks state for a c4m destination during multi-dest copy.
type multiC4mDest struct {
	c4mFile   string
	baseDepth int
	manifest  *c4m.Manifest
}

// cpMultiDest copies a real source directory to multiple destinations
// simultaneously. Each source file is read once and written to all
// destinations via io.MultiWriter — one read fans out to N writes.
func cpMultiDest(src string, dests []string) {
	src = expandHome(src)
	info, err := os.Stat(src)
	if err != nil {
		die("cp: %v", err)
	}
	if !info.IsDir() {
		die("cp: multi-dest requires a directory source")
	}

	// Classify destinations.
	var realDests []string
	var c4mDests []multiC4mDest
	for _, d := range dests {
		if !isC4mPath(d) {
			realDests = append(realDests, expandHome(d))
			continue
		}
		c4mFile, sub := splitC4mPath(d)
		m, _ := loadManifest(c4mFile)
		if m == nil {
			m = c4m.NewManifest()
		}
		baseDepth := 0
		if sub != "" {
			baseDepth = strings.Count(sub, "/") + 1
			ensureParentDirs(m, sub)
		}
		c4mDests = append(c4mDests, multiC4mDest{
			c4mFile:   c4mFile,
			baseDepth: baseDepth,
			manifest:  m,
		})
	}

	hasC4m := len(c4mDests) > 0

	// Open store if any c4m destination exists.
	var store c4store.Store
	if hasC4m {
		store, err = openStore()
		if err != nil {
			die("cp: store: %v", err)
		}
		if store == nil {
			die("cp: no content store configured (set C4_STORE or run c4 init)")
		}
	}

	// Create real destination roots.
	for _, rd := range realDests {
		if mkErr := os.MkdirAll(rd, 0755); mkErr != nil {
			die("cp: %v", mkErr)
		}
	}

	// Track directories for timestamp fixup.
	type dirRecord struct {
		relPath   string
		timestamp time.Time
	}
	var dirs []dirRecord

	// Walk source tree.
	srcBase := src
	walkErr := filepath.Walk(srcBase, func(p string, fi os.FileInfo, wErr error) error {
		if wErr != nil {
			fmt.Fprintf(os.Stderr, "c4sh: cp: %v\n", wErr)
			return nil
		}
		rel, _ := filepath.Rel(srcBase, p)
		if rel == "." {
			return nil
		}

		parts := strings.Split(rel, string(filepath.Separator))
		name := fi.Name()

		if fi.IsDir() {
			mode := fi.Mode().Perm()
			if mode == 0 {
				mode = 0755
			}
			for _, rd := range realDests {
				if mkErr := os.MkdirAll(filepath.Join(rd, rel), mode); mkErr != nil {
					fmt.Fprintf(os.Stderr, "c4sh: cp: %v\n", mkErr)
				}
			}
			dirs = append(dirs, dirRecord{relPath: rel, timestamp: fi.ModTime().UTC()})
			depth := len(parts) - 1
			for i := range c4mDests {
				c4mDests[i].manifest.AddEntry(&c4m.Entry{
					Name:      name + "/",
					Depth:     c4mDests[i].baseDepth + depth,
					Mode:      fi.Mode() | os.ModeDir,
					Size:      -1,
					Timestamp: fi.ModTime().UTC(),
				})
			}
			return nil
		}

		if !fi.Mode().IsRegular() {
			return nil
		}

		id := multiDestCopyFile(p, rel, fi, realDests, store, hasC4m)
		if id.IsNil() && !hasC4m {
			return nil // real-only copy succeeded, no ID needed
		}

		depth := len(parts) - 1
		for i := range c4mDests {
			c4mDests[i].manifest.AddEntry(&c4m.Entry{
				Name:      name,
				Depth:     c4mDests[i].baseDepth + depth,
				Mode:      fi.Mode(),
				Size:      fi.Size(),
				Timestamp: fi.ModTime().UTC(),
				C4ID:      id,
			})
		}
		return nil
	})
	if walkErr != nil {
		die("cp: walk: %v", walkErr)
	}

	// Save all c4m manifests.
	for _, cd := range c4mDests {
		cd.manifest.SortEntries()
		if sErr := saveManifest(cd.c4mFile, cd.manifest); sErr != nil {
			fmt.Fprintf(os.Stderr, "c4sh: cp: %v\n", sErr)
		}
	}

	// Set directory timestamps on real destinations (deepest first).
	for i := len(dirs) - 1; i >= 0; i-- {
		d := dirs[i]
		for _, rd := range realDests {
			dstPath := filepath.Join(rd, d.relPath)
			os.Chtimes(dstPath, d.timestamp, d.timestamp)
		}
	}
}

// multiDestCopyFile reads a single source file and writes it simultaneously
// to all real destinations and (if hasC4m) to a store temp file. Returns
// the computed C4 ID. On error the ID may be nil.
func multiDestCopyFile(srcPath, rel string, fi os.FileInfo, realDests []string, store c4store.Store, hasC4m bool) c4.ID {
	sf, err := os.Open(srcPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "c4sh: cp: %s: %v\n", rel, err)
		return c4.ID{}
	}
	defer sf.Close()

	hasher := sha512.New()
	writers := []io.Writer{hasher}
	var realFiles []*os.File

	for _, rd := range realDests {
		dstPath := filepath.Join(rd, rel)
		if mkErr := os.MkdirAll(filepath.Dir(dstPath), 0755); mkErr != nil {
			fmt.Fprintf(os.Stderr, "c4sh: cp: %v\n", mkErr)
			continue
		}
		rf, cErr := os.Create(dstPath)
		if cErr != nil {
			fmt.Fprintf(os.Stderr, "c4sh: cp: %v\n", cErr)
			continue
		}
		writers = append(writers, rf)
		realFiles = append(realFiles, rf)
	}

	var tmp *os.File
	if hasC4m && store != nil {
		tmp, err = os.CreateTemp(os.TempDir(), ".ingest.*")
		if err != nil {
			fmt.Fprintf(os.Stderr, "c4sh: cp: temp: %v\n", err)
			closeFiles(realFiles)
			return c4.ID{}
		}
		writers = append(writers, tmp)
	}

	multi := io.MultiWriter(writers...)
	if _, cpErr := io.Copy(multi, sf); cpErr != nil {
		fmt.Fprintf(os.Stderr, "c4sh: cp: %s: %v\n", rel, cpErr)
		closeFiles(realFiles)
		if tmp != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
		return c4.ID{}
	}

	closeFiles(realFiles)

	// Set metadata on real destination files.
	for _, rd := range realDests {
		dstPath := filepath.Join(rd, rel)
		mode := fi.Mode().Perm()
		if mode != 0 {
			os.Chmod(dstPath, mode)
		}
		os.Chtimes(dstPath, fi.ModTime(), fi.ModTime())
	}

	// Compute C4 ID.
	var id c4.ID
	copy(id[:], hasher.Sum(nil))

	// Finalize store content.
	if tmp != nil {
		if sErr := tmp.Sync(); sErr != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			fmt.Fprintf(os.Stderr, "c4sh: cp: %s: store sync: %v\n", rel, sErr)
			return id
		}
		tmpName := tmp.Name()
		tmp.Close()

		if store.Has(id) {
			os.Remove(tmpName)
		} else {
			// Pipe temp through store.Put for correct trie placement.
			if pErr := storeTempFile(store, tmpName); pErr != nil {
				fmt.Fprintf(os.Stderr, "c4sh: cp: %s: store: %v\n", rel, pErr)
			}
		}
	}

	return id
}

// storeTempFile opens a temp file and stores its content via store.Put,
// then removes the temp file.
func storeTempFile(store c4store.Store, tmpPath string) error {
	f, err := os.Open(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return err
	}
	defer f.Close()
	defer os.Remove(tmpPath)
	_, err = store.Put(f)
	return err
}

// closeFiles closes a slice of open files, ignoring errors.
func closeFiles(files []*os.File) {
	for _, f := range files {
		f.Close()
	}
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
	target, targetIdx := findEntryByPath(m, srcSub)
	if target == nil {
		// Try with trailing slash for directories
		target, targetIdx = findEntryByPath(m, srcSub+"/")
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

	// Directory — collect it and all descendants
	var result []resolvedEntry
	baseDepth := target.Depth

	// Don't include the directory entry itself — include its children
	// so that "cp project.c4m:shots/ out.c4m:" copies shots' contents
	for i := targetIdx + 1; i < len(m.Entries); i++ {
		e := m.Entries[i]
		if e.Depth <= baseDepth {
			break // left the subtree
		}
		depthOff := e.Depth - baseDepth - 1
		relPath := reconstructRelPath(m, e, targetIdx)
		result = append(result, resolvedEntry{
			entry:       e,
			relPath:     relPath,
			depthOffset: depthOff,
		})
	}
	return result
}

// reconstructRelPath builds the path of entry e relative to the directory
// at parentIdx in the manifest's entry list.
func reconstructRelPath(m *c4m.Manifest, e *c4m.Entry, parentIdx int) string {
	full := entryFullPath(m, e)
	parentFull := entryFullPath(m, m.Entries[parentIdx])
	parentFull = strings.TrimSuffix(parentFull, "/")
	rel := strings.TrimPrefix(full, parentFull+"/")
	// Strip trailing slash for directory names in paths
	return strings.TrimSuffix(rel, "/")
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
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "c4sh: cp: %v\n", err)
		return
	}

	if e.C4ID.IsNil() {
		// Empty file — create it with correct mode
		f, err := os.Create(dstPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "c4sh: cp: %v\n", err)
			return
		}
		f.Close()
		setFileMetadata(dstPath, e)
		return
	}

	rc, err := store.Open(e.C4ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "c4sh: cp: content not found for %s (%s)\n", e.Name, e.C4ID)
		return
	}
	defer rc.Close()

	out, err := os.Create(dstPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "c4sh: cp: %v\n", err)
		return
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		fmt.Fprintf(os.Stderr, "c4sh: cp: write %s: %v\n", dstPath, err)
		return
	}
	out.Close()
	setFileMetadata(dstPath, e)
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
		existing, _ := findEntryByPath(m, partial)
		if existing != nil {
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
