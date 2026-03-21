package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Avalanche-io/c4sh/internal/ctx"
)

// runCat implements "c4sh cat" — c4m-aware cat with fallthrough.
//
// Resolves the path to a c4m entry, reads its C4 ID, and streams
// the content from the store. For non-c4m paths, execs the real cat.
func runCat(args []string) {
	cur := ctx.Current()

	// Quick check: if not in context and no args reference c4m, fallthrough
	if cur == nil && !hasC4mArg(args) {
		fallthrough_("cat", args)
		return
	}

	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue // skip flags
		}

		// Explicit c4m colon syntax (e.g., project.c4m:src/main.go)
		if strings.Contains(arg, ":") {
			c4mFile, subPath := splitC4mPath(arg)
			if subPath == "" {
				fmt.Fprintf(os.Stderr, "c4sh: cat: specify a file path: %s:<path>\n", c4mFile)
				osExit(1)
			}
			abs, _ := filepath.Abs(c4mFile)
			catFromC4m(abs, subPath)
			continue
		}

		// Bare .c4m or extension-free c4m reference
		if strings.HasSuffix(arg, ".c4m") {
			fmt.Fprintf(os.Stderr, "c4sh: cat: %s: is a c4m file, not a content file\n", arg)
			osExit(1)
		}
		if _, err := os.Stat(arg + ".c4m"); err == nil {
			fmt.Fprintf(os.Stderr, "c4sh: cat: %s: is a c4m file, not a content file\n", arg)
			osExit(1)
		}

		// In c4m context: resolve relative to current CWD
		if cur != nil {
			resolved := cur.Resolve(arg)
			catFromC4m(cur.C4mPath, resolved)
			continue
		}

		// Not in context, not a c4m path — fallthrough
		fallthrough_("cat", args)
		return
	}
}

// catFromC4m reads content for a file entry from the store.
func catFromC4m(c4mPath, subPath string) {
	if err := catFromC4mTo(os.Stdout, c4mPath, subPath); err != nil {
		fmt.Fprintf(os.Stderr, "c4sh: cat: %v\n", err)
		osExit(1)
	}
}

// catFromC4mTo reads content for a file entry from the store and writes it to w.
// Returns an error instead of calling os.Exit.
func catFromC4mTo(w io.Writer, c4mPath, subPath string) error {
	m, err := loadManifest(c4mPath)
	if err != nil {
		return err
	}

	e := findEntry(m, subPath)
	if e == nil {
		return fmt.Errorf("%s: not found in %s", subPath, filepath.Base(c4mPath))
	}

	if e.IsDir() {
		return fmt.Errorf("%s: is a directory", subPath)
	}

	if e.C4ID.IsNil() {
		if e.Size == 0 {
			return nil // empty file, nothing to output
		}
		return fmt.Errorf("%s: no content (null C4 ID)", subPath)
	}

	s, err := openStore()
	if err != nil {
		return fmt.Errorf("cannot open store: %v\n  The content store holds file data referenced by c4m entries.\n  Set C4_STORE or configure ~/.c4/config.", err)
	}
	if s == nil {
		return fmt.Errorf("no content store configured\n  Set C4_STORE to a store path, or create ~/.c4/config.\n  Use 'cp <dir> <file.c4m>:' to scan and store content.")
	}

	rc, err := s.Open(e.C4ID)
	if err != nil {
		return fmt.Errorf("%s: content not in store\n  C4 ID: %s\n  The c4m describes this file but the store does not have its content.\n  Use 'cp <source-dir> <file.c4m>:' to scan with storage.", subPath, e.C4ID)
	}
	defer rc.Close()

	if _, err := io.Copy(w, rc); err != nil {
		return fmt.Errorf("write error: %v", err)
	}
	return nil
}

// copyToStdout copies src to stdout, handling broken pipe gracefully.
func copyToStdout(src io.Reader) {
	if _, err := io.Copy(os.Stdout, src); err != nil {
		if isBrokenPipe(err) {
			osExit(0)
		}
		fmt.Fprintf(os.Stderr, "c4sh: cat: write error: %v\n", err)
		osExit(1)
	}
}

// hasC4mArg returns true if any non-flag argument references a c4m file.
func hasC4mArg(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if isC4mPath(arg) {
			return true
		}
	}
	return false
}
