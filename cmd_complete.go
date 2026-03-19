package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Avalanche-io/c4sh/internal/ctx"
)

// runComplete implements "c4sh --complete <partial>" for tab completion.
// Outputs one completion candidate per line.
func runComplete(args []string) {
	if len(args) == 0 {
		return
	}
	partial := args[0]

	cur := ctx.Current()

	// Outside c4m context: complete .c4m files and regular directories
	if cur == nil {
		completeFilesystem(partial)
		return
	}

	// Inside c4m context: complete from manifest entries
	m, err := loadManifest(cur.C4mPath)
	if err != nil {
		return
	}

	// Resolve the partial path against the current CWD
	prefix := ""
	lookupDir := cur.CWD
	if strings.Contains(partial, "/") {
		prefix = filepath.Dir(partial) + "/"
		lookupDir = cur.Resolve(filepath.Dir(partial))
	}

	entries := entriesAtPath(m, lookupDir)

	base := filepath.Base(partial)
	if partial == "" || strings.HasSuffix(partial, "/") {
		base = ""
	}

	for _, e := range entries {
		name := strings.TrimSuffix(e.Name, "/")
		if strings.HasPrefix(name, ".") {
			continue
		}
		if base != "" && !strings.HasPrefix(name, base) {
			continue
		}
		if e.IsDir() {
			fmt.Println(prefix + name + "/")
		} else {
			fmt.Println(prefix + name)
		}
	}
}

// completeFilesystem lists .c4m files and directories from the real filesystem.
func completeFilesystem(partial string) {
	dir := "."
	base := partial

	if strings.Contains(partial, "/") {
		dir = filepath.Dir(partial)
		base = filepath.Base(partial)
	}
	if partial == "" || strings.HasSuffix(partial, "/") {
		if partial != "" {
			dir = strings.TrimSuffix(partial, "/")
		}
		base = ""
	}

	prefix := ""
	if dir != "." {
		prefix = dir + "/"
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if base != "" && !strings.HasPrefix(name, base) {
			continue
		}
		if e.IsDir() {
			fmt.Println(prefix + name + "/")
		} else if strings.HasSuffix(name, ".c4m") {
			fmt.Println(prefix + name)
		}
	}
}
