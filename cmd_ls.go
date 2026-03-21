package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Avalanche-io/c4/c4m"
	"github.com/Avalanche-io/c4sh/internal/ctx"
	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

// runLs implements "c4sh ls" — c4m-aware ls with fallthrough.
//
// When in a c4m context or targeting a c4m path, lists entries from the
// c4m text. Otherwise execs real ls.
func runLs(args []string) {
	// Parse flags and collect paths
	var longFormat, showAll, onePerLine, showIDs bool
	var paths []string
	for _, arg := range args {
		if arg == "--id" || arg == "--ids" {
			showIDs = true
			continue
		}
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
				case 'i':
					showIDs = true
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
				osExit(1)
			}
			entries := entriesAtPath(m, cur.CWD)
			printEntries(entries, longFormat, showAll, onePerLine, showIDs)
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
				osExit(1)
			}
			listPath(m, subPath, longFormat, showAll, onePerLine, showIDs)
			continue
		}

		// Bare .c4m file (e.g., "ls project.c4m")
		if strings.HasSuffix(p, ".c4m") {
			abs, _ := filepath.Abs(p)
			m, err := loadManifest(abs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "c4sh: ls: %v\n", err)
				osExit(1)
			}
			entries := m.Root()
			printEntries(entries, longFormat, showAll, onePerLine, showIDs)
			continue
		}

		// Extension-free c4m reference (e.g., "ls project" where project.c4m exists)
		if _, err := os.Stat(p + ".c4m"); err == nil {
			abs, _ := filepath.Abs(p + ".c4m")
			m, err := loadManifest(abs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "c4sh: ls: %v\n", err)
				osExit(1)
			}
			entries := m.Root()
			printEntries(entries, longFormat, showAll, onePerLine, showIDs)
			continue
		}

		// In c4m context: resolve relative to current CWD
		if cur != nil {
			m, err := loadManifest(cur.C4mPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "c4sh: ls: %v\n", err)
				osExit(1)
			}
			resolved := cur.Resolve(p)
			listPath(m, resolved, longFormat, showAll, onePerLine, showIDs)
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
func listPath(m *c4m.Manifest, subPath string, long, showAll, onePerLine, showIDs bool) {
	if err := listPathTo(os.Stdout, m, subPath, long, showAll, onePerLine, showIDs); err != nil {
		fmt.Fprintf(os.Stderr, "c4sh: ls: %v\n", err)
		osExit(1)
	}
}

// listPathTo lists entries at a given path within a manifest, writing to w.
// Returns an error instead of calling os.Exit.
func listPathTo(w io.Writer, m *c4m.Manifest, subPath string, long, showAll, onePerLine, showIDs bool) error {
	if subPath == "" {
		printEntriesTo(w, m.Root(), long, showAll, onePerLine, false, showIDs)
		return nil
	}

	// Try as a directory first (with trailing /)
	dirPath := subPath
	if !strings.HasSuffix(dirPath, "/") {
		dirPath += "/"
	}
	dirEntry := findEntry(m, dirPath)
	if dirEntry != nil && dirEntry.IsDir() {
		entries := m.Children(dirEntry)
		printEntriesTo(w, entries, long, showAll, onePerLine, false, showIDs)
		return nil
	}

	// Try as a file (without trailing /)
	filePath := strings.TrimSuffix(subPath, "/")
	fileEntry := findEntry(m, filePath)
	if fileEntry != nil && !fileEntry.IsDir() {
		if long {
			printLongEntryTo(w, fileEntry, showIDs)
		} else {
			fmt.Fprintln(w, fileEntry.Name)
		}
		return nil
	}

	return fmt.Errorf("%s: no such file or directory", subPath)
}

// printEntries outputs a list of entries with the appropriate format.
func printEntries(entries []*c4m.Entry, long, showAll, onePerLine, showIDs bool) {
	printEntriesTo(os.Stdout, entries, long, showAll, onePerLine, isTerminal(), showIDs)
}

// printEntriesTo outputs a list of entries with the appropriate format to w.
func printEntriesTo(w io.Writer, entries []*c4m.Entry, long, showAll, onePerLine, isTTY, showIDs bool) {
	for _, e := range entries {
		if !showAll && strings.HasPrefix(e.Name, ".") {
			continue
		}
		if long {
			printLongEntryTo(w, e, showIDs)
		} else if onePerLine {
			fmt.Fprintln(w, e.Name)
		} else if !isTTY {
			// When piped, one entry per line (matches real ls behavior)
			fmt.Fprintln(w, e.Name)
		} else {
			name := e.Name
			if e.IsDir() {
				fmt.Fprintf(w, "\033[1;34m%s\033[0m  ", name)
			} else if e.Mode&0111 != 0 {
				fmt.Fprintf(w, "\033[1;32m%s\033[0m  ", name)
			} else {
				fmt.Fprintf(w, "%s  ", name)
			}
		}
	}
	if !long && !onePerLine && isTTY && len(entries) > 0 {
		fmt.Fprintln(w)
	}
}

// printLongEntry prints a single entry in long format to stdout.
func printLongEntry(e *c4m.Entry, showIDs bool) {
	printLongEntryTo(os.Stdout, e, showIDs)
}

// printLongEntryTo prints a single entry in long format to w.
//
// showIDs controls whether C4 IDs appear (requires -i flag).
// When piped with -i: canonical c4m entry format (parseable, round-trips).
// When TTY with -i: human-readable with C4 IDs right-aligned at terminal edge.
// Without -i: human-readable, no C4 IDs.
func printLongEntryTo(w io.Writer, e *c4m.Entry, showIDs bool) {
	isTTY := isTerminal()

	// Piped with -i: canonical c4m entry format
	if !isTTY && showIDs {
		fmt.Fprintln(w, e.Canonical())
		return
	}

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

	base := fmt.Sprintf("%s  %8s %s %s", mode, size, ts, name)

	// TTY with -i: right-align C4 IDs at terminal edge
	if showIDs && isTTY {
		c4id := "-"
		if !e.C4ID.IsNil() {
			c4id = e.C4ID.String()
		}
		width := terminalWidth()
		padding := width - len(base) - len(c4id)
		if padding < 2 {
			padding = 2
		}
		fmt.Fprintf(w, "%s%s%s\n", base, strings.Repeat(" ", padding), c4id)
		return
	}

	// No -i: no C4 IDs
	fmt.Fprintln(w, base)
}

// terminalWidth returns the current terminal width, defaulting to 120.
func terminalWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 120
	}
	return w
}

// isTerminal returns true if stdout is a terminal.
func isTerminal() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}
