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
	// Check for --powershell flag
	var psMode bool
	var filtered []string
	for _, a := range args {
		if a == "--powershell" {
			psMode = true
		} else {
			filtered = append(filtered, a)
		}
	}
	if psMode {
		shellFormat = "powershell"
	}

	target := ""
	if len(filtered) > 0 {
		target = filtered[0]
	}

	cur := ctx.Current()

	// cd with no args: if in context, exit context; otherwise go home
	if target == "" || target == "~" {
		if cur != nil {
			exitContext("")
		} else {
			home, _ := os.UserHomeDir()
			var b strings.Builder
			writeCd(&b, home)
			fmt.Print(b.String())
		}
		return
	}

	// cd - = return to previous directory (also exits c4m context)
	if target == "-" {
		var b strings.Builder
		writeUnsetEnv(&b, "C4_CONTEXT", "C4_CWD")
		writeCd(&b, "-")
		fmt.Print(b.String())
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
	var b strings.Builder
	writeCd(&b, target)
	fmt.Print(b.String())
}

// enterContext enters a c4m file at its root.
func enterContext(c4mFile string) {
	enterContextAt(c4mFile, "")
}

// enterContextAt sets up c4m context at a specific subpath.
func enterContextAt(c4mFile, subPath string) {
	cmds, err := enterContextCmds(c4mFile, subPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "c4sh: cd: %v\n", err)
		osExit(1)
	}
	fmt.Print(cmds)
}

// enterContextCmds returns shell commands to enter a c4m context.
// Returns the shell eval string or an error.
func enterContextCmds(c4mFile, subPath string) (string, error) {
	abs, err := filepath.Abs(c4mFile)
	if err != nil {
		return "", err
	}

	// Verify the c4m file exists
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("%s: not found", c4mFile)
	}

	// If a subpath is specified, verify it exists in the c4m
	if subPath != "" {
		m, err := loadManifest(abs)
		if err != nil {
			return "", err
		}
		// Ensure directory path ends with /
		dirPath := subPath
		if !strings.HasSuffix(dirPath, "/") {
			dirPath += "/"
		}
		e := findEntry(m, dirPath)
		if e == nil || !e.IsDir() {
			name := strings.TrimSuffix(filepath.Base(abs), ".c4m")
			return "", fmt.Errorf("%s: no such directory in %s", subPath, name)
		}
		subPath = dirPath
	}

	var b strings.Builder
	writeSetEnv(&b, "C4_CONTEXT", abs)
	writeSetEnv(&b, "C4_CWD", subPath)
	return b.String(), nil
}

// navigateWithin navigates within the current c4m context.
func navigateWithin(cur *ctx.Context, target string) {
	cmds, err := navigateWithinCmds(cur, target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "c4sh: cd: %v\n", err)
		osExit(1)
	}
	fmt.Print(cmds)
}

// navigateWithinCmds returns shell commands to navigate within a c4m context.
// Returns the shell eval string or an error.
func navigateWithinCmds(cur *ctx.Context, target string) (string, error) {
	// Absolute filesystem path = exit context
	if filepath.IsAbs(target) {
		var b strings.Builder
		b.WriteString(exitContextCmds(""))
		writeCd(&b, target)
		return b.String(), nil
	}

	// Resolve the new path within the c4m
	newPath := cur.Resolve(target)

	// If resolved to root, just update CWD
	if newPath == "" {
		var b strings.Builder
		writeSetEnv(&b, "C4_CWD", "")
		return b.String(), nil
	}

	// Verify the directory exists in the c4m
	m, err := loadManifest(cur.C4mPath)
	if err != nil {
		return "", err
	}

	// Ensure it ends with /
	dirPath := newPath
	if !strings.HasSuffix(dirPath, "/") {
		dirPath += "/"
	}
	e := findEntry(m, dirPath)
	if e == nil || !e.IsDir() {
		return "", fmt.Errorf("%s: no such directory in %s", target, cur.C4mName())
	}

	var b strings.Builder
	writeSetEnv(&b, "C4_CWD", dirPath)
	return b.String(), nil
}

// exitContext clears c4m context.
func exitContext(realPath string) {
	fmt.Print(exitContextCmds(realPath))
}

// exitContextCmds returns shell commands to exit the c4m context.
func exitContextCmds(realPath string) string {
	var b strings.Builder
	writeUnsetEnv(&b, "C4_CONTEXT", "C4_CWD")
	if realPath != "" {
		writeCd(&b, realPath)
	}
	return b.String()
}

// shellFormat controls output syntax. Default is "bash"; set to "powershell"
// when --powershell flag is passed.
var shellFormat = "bash"

// writeSetEnv writes a shell-appropriate environment variable assignment.
func writeSetEnv(b *strings.Builder, key, value string) {
	if shellFormat == "powershell" {
		fmt.Fprintf(b, "$env:%s = %q\n", key, value)
	} else {
		fmt.Fprintf(b, "export %s=%q\n", key, value)
	}
}

// writeUnsetEnv writes a shell-appropriate environment variable removal.
func writeUnsetEnv(b *strings.Builder, keys ...string) {
	if shellFormat == "powershell" {
		for _, k := range keys {
			fmt.Fprintf(b, "Remove-Item Env:\\%s -ErrorAction SilentlyContinue\n", k)
		}
	} else {
		fmt.Fprintf(b, "unset %s\n", strings.Join(keys, " "))
	}
}

// writeCd writes a shell-appropriate change-directory command.
func writeCd(b *strings.Builder, path string) {
	if shellFormat == "powershell" {
		fmt.Fprintf(b, "Set-Location %q\n", path)
	} else {
		fmt.Fprintf(b, "builtin cd %q\n", path)
	}
}
