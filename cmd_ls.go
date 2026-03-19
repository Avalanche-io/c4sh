package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Avalanche-io/c4/c4m"
	"github.com/Avalanche-io/c4sh/internal/ctx"
	"github.com/mattn/go-isatty"
)

// runLs implements "c4sh ls" — c4m-aware ls with fallthrough.
//
// When in a c4m context or targeting a c4m path, lists entries from the
// c4m text. Otherwise execs real ls.
func runLs(args []string) {
	// Parse flags and collect paths
	var longFormat, showAll, onePerLine bool
	var paths []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "--") {
			continue // skip long options like --color=auto
		}
		if strings.HasPrefix(arg, "-") && !strings.Contains(arg, ".c4m") {
			for _, ch := range arg[1:] {
				switch ch {
				case 'l':
					longFormat = true
				case 'a':
					showAll = true
				case '1':
					onePerLine = true
				}
			}
			continue
		}
		paths = append(paths, arg)
	}

	cur := ctx.Current()

	// No paths given
	if len(paths) == 0 {
		// In c4m context: list current directory within the c4m
		if cur != nil {
			m, err := loadManifest(cur.C4mPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "c4sh: ls: %v\n", err)
				os.Exit(1)
			}
			entries := entriesAtPath(m, cur.CWD)
			printEntries(entries, longFormat, showAll, onePerLine)
			return
		}
		// Not in context: fallthrough to real ls
		fallthrough_("ls", args)
		return
	}

	// Process each path
	for _, p := range paths {
		// Explicit c4m colon syntax (e.g., project.c4m:src/)
		if strings.Contains(p, ":") {
			c4mFile, subPath := splitC4mPath(p)
			abs, _ := filepath.Abs(c4mFile)
			m, err := loadManifest(abs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "c4sh: ls: %v\n", err)
				os.Exit(1)
			}
			listPath(m, subPath, longFormat, showAll, onePerLine)
			continue
		}

		// Bare .c4m file (e.g., "ls project.c4m")
		if strings.HasSuffix(p, ".c4m") {
			abs, _ := filepath.Abs(p)
			m, err := loadManifest(abs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "c4sh: ls: %v\n", err)
				os.Exit(1)
			}
			entries := m.Root()
			printEntries(entries, longFormat, showAll, onePerLine)
			continue
		}

		// Extension-free c4m reference (e.g., "ls project" where project.c4m exists)
		if _, err := os.Stat(p + ".c4m"); err == nil {
			abs, _ := filepath.Abs(p + ".c4m")
			m, err := loadManifest(abs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "c4sh: ls: %v\n", err)
				os.Exit(1)
			}
			entries := m.Root()
			printEntries(entries, longFormat, showAll, onePerLine)
			continue
		}

		// In c4m context: resolve relative to current CWD
		if cur != nil {
			m, err := loadManifest(cur.C4mPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "c4sh: ls: %v\n", err)
				os.Exit(1)
			}
			resolved := cur.Resolve(p)
			listPath(m, resolved, longFormat, showAll, onePerLine)
			continue
		}

		// Not in context, not a c4m path — fallthrough to real ls
		fallthrough_("ls", args)
		return
	}
}

// listPath lists entries at a given path within a manifest.
// If the path points to a file, shows that single entry.
// If the path points to a directory, shows its children.
func listPath(m *c4m.Manifest, subPath string, long, showAll, onePerLine bool) {
	if subPath == "" {
		printEntries(m.Root(), long, showAll, onePerLine)
		return
	}

	// Try as a directory first (with trailing /)
	dirPath := subPath
	if !strings.HasSuffix(dirPath, "/") {
		dirPath += "/"
	}
	dirEntry := findEntry(m, dirPath)
	if dirEntry != nil && dirEntry.IsDir() {
		entries := m.Children(dirEntry)
		printEntries(entries, long, showAll, onePerLine)
		return
	}

	// Try as a file (without trailing /)
	filePath := strings.TrimSuffix(subPath, "/")
	fileEntry := findEntry(m, filePath)
	if fileEntry != nil && !fileEntry.IsDir() {
		if long {
			printLongEntry(fileEntry)
		} else {
			fmt.Println(fileEntry.Name)
		}
		return
	}

	fmt.Fprintf(os.Stderr, "c4sh: ls: %s: no such file or directory\n", subPath)
	os.Exit(1)
}

// printEntries outputs a list of entries with the appropriate format.
func printEntries(entries []*c4m.Entry, long, showAll, onePerLine bool) {
	isTTY := isTerminal()
	for _, e := range entries {
		if !showAll && strings.HasPrefix(e.Name, ".") {
			continue
		}
		if long {
			printLongEntry(e)
		} else if onePerLine {
			fmt.Println(e.Name)
		} else {
			name := e.Name
			if isTTY && e.IsDir() {
				fmt.Printf("\033[1;34m%s\033[0m  ", name)
			} else if isTTY && e.Mode&0111 != 0 {
				fmt.Printf("\033[1;32m%s\033[0m  ", name)
			} else {
				fmt.Printf("%s  ", name)
			}
		}
	}
	if !long && !onePerLine && len(entries) > 0 {
		fmt.Println()
	}
}

// printLongEntry prints a single entry in long format matching c4m entry format:
// mode timestamp size name [-> target] c4id
func printLongEntry(e *c4m.Entry) {
	// Mode
	mode := e.Mode.String()
	if len(mode) > 10 {
		mode = mode[:10]
	}

	// Timestamp
	ts := "-"
	if !e.Timestamp.IsZero() && !e.Timestamp.Equal(c4m.NullTimestamp()) {
		now := time.Now()
		if now.Year() == e.Timestamp.Year() {
			ts = e.Timestamp.Local().Format("Jan _2 15:04")
		} else {
			ts = e.Timestamp.Local().Format("Jan _2  2006")
		}
	}

	// Size
	size := fmt.Sprintf("%d", e.Size)
	if e.Size < 0 {
		size = "-"
	}

	// Name (with symlink target if applicable)
	name := e.Name
	if e.IsSymlink() && e.Target != "" {
		name = name + " -> " + e.Target
	}

	// C4 ID
	c4id := "-"
	if !e.C4ID.IsNil() {
		c4id = e.C4ID.String()
	}

	fmt.Printf("%s  %8s %s %s %s\n", mode, size, ts, name, c4id)
}

// isTerminal returns true if stdout is a terminal.
func isTerminal() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}
