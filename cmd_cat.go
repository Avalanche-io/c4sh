package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

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
				os.Exit(1)
			}
			abs, _ := filepath.Abs(c4mFile)
			catFromC4m(abs, subPath)
			continue
		}

		// Bare .c4m or extension-free c4m reference
		if strings.HasSuffix(arg, ".c4m") {
			fmt.Fprintf(os.Stderr, "c4sh: cat: %s: is a c4m file, not a content file\n", arg)
			os.Exit(1)
		}
		if _, err := os.Stat(arg + ".c4m"); err == nil {
			fmt.Fprintf(os.Stderr, "c4sh: cat: %s: is a c4m file, not a content file\n", arg)
			os.Exit(1)
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
	m, err := loadManifest(c4mPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "c4sh: cat: %v\n", err)
		os.Exit(1)
	}

	e := findEntry(m, subPath)
	if e == nil {
		fmt.Fprintf(os.Stderr, "c4sh: cat: %s: not found in %s\n",
			subPath, filepath.Base(c4mPath))
		os.Exit(1)
	}

	if e.IsDir() {
		fmt.Fprintf(os.Stderr, "c4sh: cat: %s: is a directory\n", subPath)
		os.Exit(1)
	}

	if e.C4ID.IsNil() {
		// Null C4 ID means empty file or content not available
		if e.Size == 0 {
			return // empty file, nothing to output
		}
		fmt.Fprintf(os.Stderr, "c4sh: cat: %s: no content (null C4 ID)\n", subPath)
		os.Exit(1)
	}

	s, err := openStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "c4sh: cat: store error: %v\n", err)
		os.Exit(1)
	}
	if s == nil {
		fmt.Fprintf(os.Stderr, "c4sh: cat: no store configured\n")
		os.Exit(1)
	}

	rc, err := s.Open(e.C4ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "c4sh: cat: content not available for %s (%s)\n",
			subPath, e.C4ID)
		os.Exit(1)
	}
	defer rc.Close()

	copyToStdout(rc)
}

// copyToStdout copies src to stdout, handling EPIPE (broken pipe) gracefully.
func copyToStdout(src io.Reader) {
	if _, err := io.Copy(os.Stdout, src); err != nil {
		if errors.Is(err, syscall.EPIPE) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "c4sh: cat: write error: %v\n", err)
		os.Exit(1)
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
