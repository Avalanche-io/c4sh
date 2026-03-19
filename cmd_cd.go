package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Avalanche-io/c4sh/internal/ctx"
)

// runCd implements "c4sh cd" — outputs shell commands to eval.
//
// The shell cd() function calls this and evals the output:
//
//	cd() { eval "$(command c4sh cd "$@")"; }
//
// Outputs shell variable assignments (export/unset) and builtin cd commands.
func runCd(args []string) {
	target := ""
	if len(args) > 0 {
		target = args[0]
	}

	cur := ctx.Current()

	// cd with no args: if in context, exit context; otherwise go home
	if target == "" || target == "~" {
		if cur != nil {
			exitContext("")
		} else {
			home, _ := os.UserHomeDir()
			fmt.Printf("builtin cd %q\n", home)
		}
		return
	}

	// cd - = return to previous directory
	if target == "-" {
		fmt.Println("builtin cd -")
		return
	}

	// Expand ~ prefix
	if strings.HasPrefix(target, "~/") {
		home, _ := os.UserHomeDir()
		target = filepath.Join(home, target[2:])
	}

	// Case 1: explicit c4m path with colon (e.g., project.c4m:shots/010/)
	if strings.Contains(target, ":") {
		c4mFile, subPath := splitC4mPath(target)
		enterContextAt(c4mFile, subPath)
		return
	}

	// Case 2: target ends with .c4m (e.g., project.c4m)
	if strings.HasSuffix(target, ".c4m") {
		enterContext(target)
		return
	}

	// Case 3: target resolves to a c4m file (e.g., "project" where project.c4m exists)
	if _, err := os.Stat(target + ".c4m"); err == nil {
		enterContext(target + ".c4m")
		return
	}

	// Case 4: currently in c4m context — navigate within it
	if cur != nil {
		navigateWithin(cur, target)
		return
	}

	// Case 5: target is a real directory — pass through to builtin cd
	fmt.Printf("builtin cd %q\n", target)
}

// enterContext enters a c4m file at its root.
func enterContext(c4mFile string) {
	enterContextAt(c4mFile, "")
}

// enterContextAt sets up c4m context at a specific subpath.
func enterContextAt(c4mFile, subPath string) {
	abs, err := filepath.Abs(c4mFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "c4sh: cd: %v\n", err)
		return
	}

	// Verify the c4m file exists
	if _, err := os.Stat(abs); err != nil {
		fmt.Fprintf(os.Stderr, "c4sh: cd: %s: not found\n", c4mFile)
		return
	}

	// If a subpath is specified, verify it exists in the c4m
	if subPath != "" {
		m, err := loadManifest(abs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "c4sh: cd: %v\n", err)
			return
		}
		// Ensure directory path ends with /
		dirPath := subPath
		if !strings.HasSuffix(dirPath, "/") {
			dirPath += "/"
		}
		e := findEntry(m, dirPath)
		if e == nil || !e.IsDir() {
			name := strings.TrimSuffix(filepath.Base(abs), ".c4m")
			fmt.Fprintf(os.Stderr, "c4sh: cd: %s: no such directory in %s\n", subPath, name)
			return
		}
		subPath = dirPath
	}

	fmt.Printf("export C4_CONTEXT=%q\n", abs)
	fmt.Printf("export C4_CWD=%q\n", subPath)
}

// navigateWithin navigates within the current c4m context.
func navigateWithin(cur *ctx.Context, target string) {
	// Absolute filesystem path = exit context
	if filepath.IsAbs(target) {
		exitContext("")
		fmt.Printf("builtin cd %q\n", target)
		return
	}

	// Resolve the new path within the c4m
	newPath := cur.Resolve(target)

	// If resolved to root, just update CWD
	if newPath == "" {
		fmt.Printf("export C4_CWD=%q\n", "")
		return
	}

	// Verify the directory exists in the c4m
	m, err := loadManifest(cur.C4mPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "c4sh: cd: %v\n", err)
		return
	}

	// Ensure it ends with /
	dirPath := newPath
	if !strings.HasSuffix(dirPath, "/") {
		dirPath += "/"
	}
	e := findEntry(m, dirPath)
	if e == nil || !e.IsDir() {
		fmt.Fprintf(os.Stderr, "c4sh: cd: %s: no such directory in %s\n",
			target, cur.C4mName())
		return
	}

	fmt.Printf("export C4_CWD=%q\n", dirPath)
}

// exitContext clears c4m context.
func exitContext(realPath string) {
	fmt.Println("unset C4_CONTEXT C4_CWD")
	if realPath != "" {
		fmt.Printf("builtin cd %q\n", realPath)
	}
}
