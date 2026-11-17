package main

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Avalanche-io/c4"
	"github.com/Avalanche-io/c4/c4m"
	c4store "github.com/Avalanche-io/c4/store"
)

// poolResult holds counters from a pool operation.
type poolResult struct {
	Copied  int
	Skipped int
	Missing int
	// MissingEntries names each missing object: its path in the c4m and
	// its C4 ID.
	MissingEntries []string
}

// runPool implements "c4sh pool <c4m> <dir>" — bundle a c4m file with all
// its referenced store objects into a self-contained portable directory.
//
// Result:
//
//	<dir>/
//	  <name>.c4m       (copy of the manifest)
//	  store/           (TreeStore with only referenced objects)
//	  extract.sh       (self-extracting shell script)
func runPool(args []string) {
	if len(args) < 2 {
		die("pool: usage: c4sh pool <file.c4m> <output-dir>")
	}

	c4mPath := args[0]
	if !strings.HasSuffix(c4mPath, ".c4m") {
		if _, err := os.Stat(c4mPath + ".c4m"); err == nil {
			c4mPath += ".c4m"
		}
	}

	outDir := args[1]

	// Load manifest
	m, err := loadManifest(c4mPath)
	if err != nil {
		die("pool: %v", err)
	}

	// Open source store
	src, err := openStore()
	if err != nil {
		die("pool: %v", err)
	}

	res, err := poolManifest(m, src, c4mPath, outDir)
	if err != nil {
		die("pool: %v", err)
	}

	for _, miss := range res.MissingEntries {
		fmt.Fprintf(os.Stderr, "c4sh: pool: missing %s\n", miss)
	}
	fmt.Printf("Pool: %d objects copied, %d skipped, %d missing\n", res.Copied, res.Skipped, res.Missing)
	if res.Missing > 0 {
		osExit(1)
	}
}

// poolManifest bundles a manifest and its store objects into outDir.
// It copies referenced objects from src into a new TreeStore at outDir/store,
// copies the c4m file, and generates extract.sh.
func poolManifest(m *c4m.Manifest, src c4store.Store, c4mPath, outDir string) (poolResult, error) {
	poolStorePath := filepath.Join(outDir, "store")
	dst, err := c4store.NewTreeStore(poolStorePath)
	if err != nil {
		return poolResult{}, fmt.Errorf("create pool store: %v", err)
	}

	var res poolResult
	seen := make(map[c4.ID]bool)

	// bundle copies one object into the bundle's store, or counts it
	// missing under name (its path in the c4m).
	bundle := func(id c4.ID, name string) error {
		if seen[id] {
			return nil
		}
		seen[id] = true
		if dst.Has(id) {
			res.Skipped++
			return nil
		}
		rc, err := src.Open(id)
		if err != nil {
			res.Missing++
			res.MissingEntries = append(res.MissingEntries, name+" "+id.String())
			return nil
		}
		defer rc.Close()
		if _, err := dst.Put(rc); err != nil {
			return fmt.Errorf("store %s: %v", id, err)
		}
		res.Copied++
		return nil
	}

	for _, e := range m.Entries {
		if e.C4ID.IsNil() {
			continue
		}
		if poolNeedsObject(m, e) {
			if err := bundle(e.C4ID, strings.TrimSuffix(m.EntryPath(e), "/")); err != nil {
				return res, err
			}
		}
		if !e.IsSequence {
			continue
		}
		// A range's ID list names its frames' content, which the bundle
		// needs like any file's.
		dir := path.Dir(m.EntryPath(e))
		for _, f := range rangeFrames(m, e, src) {
			if f.C4ID.IsNil() {
				continue
			}
			if err := bundle(f.C4ID, path.Join(dir, f.Name)); err != nil {
				return res, err
			}
		}
	}

	// Copy the c4m file
	c4mDest := filepath.Join(outDir, filepath.Base(c4mPath))
	if err := copyFile(c4mPath, c4mDest); err != nil {
		return res, fmt.Errorf("copy manifest: %v", err)
	}

	// Generate self-extracting script
	if err := writeExtractScript(outDir, filepath.Base(c4mPath), m); err != nil {
		return res, fmt.Errorf("write extract.sh: %v", err)
	}

	return res, nil
}

// poolNeedsObject reports whether a bundle needs e's C4 ID from the store.
// The bundle carries its c4m, and a listing travels with the c4m that
// describes it: a folder whose children the c4m lists, a known-empty
// folder, and a range whose ID list the c4m holds inline need nothing
// more. File content does, and so does a folder listed without its
// children or a range whose ID list isn't inline: those are addressed
// by their C4 ID, like any content.
func poolNeedsObject(m *c4m.Manifest, e *c4m.Entry) bool {
	switch {
	case e.IsDir():
		return len(m.Children(e)) == 0 && e.C4ID != emptyID
	case e.IsSequence:
		_, inline := m.RangeData[e.C4ID]
		return !inline
	}
	return true
}

// rangeFrames returns a folded range's frames, each with its own C4 ID,
// as the library expands them from the range's ID list: inline in the c4m,
// or else the store object the range's C4 ID addresses. Without the list
// there are no frames to name, as a folder whose listing isn't available
// has no children to name. A range is never expanded into frames carrying
// its own C4 ID, which names the list and no frame.
func rangeFrames(m *c4m.Manifest, e *c4m.Entry, src c4store.Store) []*c4m.Entry {
	list, ok := m.RangeData[e.C4ID]
	if !ok {
		rc, err := src.Open(e.C4ID)
		if err != nil {
			return nil
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil || !c4m.IsIDListContent(b) {
			return nil
		}
		list = string(b)
	}

	one := c4m.NewManifest()
	r := *e
	r.Depth = 0
	one.AddEntry(&r)
	one.RangeData = map[c4.ID]string{e.C4ID: list}
	expanded, _, err := c4m.NewSequenceExpander(c4m.SequenceEmbedded).ExpandManifest(one)
	if err != nil {
		return nil
	}
	var frames []*c4m.Entry
	for _, f := range expanded.Entries {
		if f.IsSequence || f.C4ID == e.C4ID {
			continue
		}
		frames = append(frames, f)
	}
	return frames
}

// emptyID is the C4 ID of no bytes: an empty file's content and an empty
// folder's listing.
var emptyID = c4.Identify(strings.NewReader(""))

// copyFile copies a regular file.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// writeExtractScript generates a portable shell script that materializes
// the c4m contents using only standard Unix tools — no c4 installation required.
// All paths are shell-escaped to prevent injection from malicious entry names.
func writeExtractScript(outDir, c4mName string, m *c4m.Manifest) error {
	scriptPath := filepath.Join(outDir, "extract.sh")
	f, err := os.OpenFile(scriptPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer f.Close()

	safeName := shellEscape(strings.TrimSuffix(c4mName, ".c4m"))

	fmt.Fprintf(f, "#!/bin/sh\n")
	fmt.Fprintf(f, "# Self-extracting script generated by c4sh pool.\n")
	fmt.Fprintf(f, "# No c4 installation required.\n")
	fmt.Fprintf(f, "# Not using set -e: a missing store object warns and the rest is still\n")
	fmt.Fprintf(f, "# written; the exit status is 1 if anything failed.\n")
	fmt.Fprintf(f, "DIR=\"$(cd \"$(dirname \"$0\")\" && pwd)\"\n")
	fmt.Fprintf(f, "STORE=\"$DIR/store\"\n")
	// The default is a plain assignment: inside "${1:-...}" the quotes
	// shellEscape adds would be literal characters of the folder name.
	fmt.Fprintf(f, "OUT=./%s\n", safeName)
	fmt.Fprintf(f, "if [ -n \"$1\" ]; then OUT=\"$1\"; fi\n")
	fmt.Fprintf(f, "STATUS=0\n")
	fmt.Fprintf(f, "echo 'Extracting to '\"$OUT\"' ...'\n")
	// Root-level files go straight into $OUT, so it must exist first.
	fmt.Fprintf(f, "mkdir -p \"$OUT\" || exit 1\n")

	// Build directory creation commands and file copy commands
	var dirStack []string
	for _, e := range m.Entries {
		fullPath := shellEscape(buildPath(dirStack, e))

		if e.IsDir() {
			for len(dirStack) > e.Depth {
				dirStack = dirStack[:len(dirStack)-1]
			}
			dirStack = append(dirStack, strings.TrimSuffix(e.Name, "/"))
			fmt.Fprintf(f, "mkdir -p \"$OUT\"/%s || STATUS=1\n", fullPath)
			continue
		}

		if e.C4ID.IsNil() {
			continue
		}

		// C4 IDs are base58 only (safe for shell), find in store trie
		idStr := e.C4ID.String()
		fmt.Fprintf(f, "SRC=$(find \"$STORE\" -name '%s' -print -quit 2>/dev/null)\n", idStr)
		fmt.Fprintf(f, "if [ -n \"$SRC\" ]; then cp \"$SRC\" \"$OUT\"/%s || STATUS=1; else echo \"warning: missing %s\" >&2; STATUS=1; fi\n", fullPath, idStr)
	}

	fmt.Fprintf(f, "if [ \"$STATUS\" -ne 0 ]; then echo 'Done, with errors.' >&2; exit 1; fi\n")
	fmt.Fprintf(f, "echo 'Done.'\n")
	return nil
}

// shellEscape wraps a string in single quotes, escaping any embedded single
// quotes with the '\'' idiom. This is safe for POSIX sh.
func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// buildPath reconstructs a file path from the directory stack and entry.
func buildPath(dirStack []string, e *c4m.Entry) string {
	parts := make([]string, 0, len(dirStack)+1)
	for i := 0; i < e.Depth && i < len(dirStack); i++ {
		parts = append(parts, dirStack[i])
	}
	parts = append(parts, strings.TrimSuffix(e.Name, "/"))
	return path.Join(parts...)
}
