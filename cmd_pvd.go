package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Avalanche-io/c4sh/internal/ctx"
)

// runPvd implements "c4sh pvd" — print virtual directory.
// Outputs the current c4m context as a colon path: "name.c4m:path/"
// When not in a c4m context, outputs nothing.
func runPvd() {
	cur := ctx.Current()
	if cur == nil {
		return
	}

	// Use the basename of the c4m file for portability
	name := filepath.Base(cur.C4mPath)
	// Strip .c4m extension for cleaner output
	name = strings.TrimSuffix(name, ".c4m")

	cwd := cur.CWD
	if cwd != "" {
		fmt.Printf("%s:%s\n", name, cwd)
	} else {
		fmt.Printf("%s:\n", name)
	}
}
