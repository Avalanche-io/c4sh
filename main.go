// c4sh — Content-Addressed Shell
//
// c4sh makes c4m files behave as directories. cd into a c4m file,
// ls, cat, cp, mv, rm, mkdir — filesystem commands operate on c4m
// entries. Content lives in the store; c4m manipulation is instant.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Avalanche-io/c4sh/internal/ctx"
)

const version = "1.0.5"

func main() {
	if len(os.Args) < 2 {
		usage()
		osExit(0)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "shell-init":
		runShellInit(args)
	case "version":
		fmt.Printf("c4sh %s\n", version)

	// Navigation
	case "cd":
		runCd(args)

	// Inspection
	case "ls":
		runLs(args)
	case "cat":
		runCat(args)

	// Editing
	case "cp":
		runCp(args)
	case "mv":
		runMv(args)
	case "rm":
		runRm(args)
	case "mkdir":
		runMkdir(args)

	// Distribution
	case "pool":
		runPool(args)
	case "ingest":
		runIngest(args)
	case "rsync":
		runRsync(args)

	// Completions (internal)
	case "--complete":
		runComplete(args)

	case "help", "--help", "-h":
		usage()

	default:
		// If in c4m context and command is unknown, fall through to system
		if isInC4mContext() {
			fallthrough_(cmd, args)
		} else {
			fmt.Fprintf(os.Stderr, "c4sh: unknown command %q\n", cmd)
			fmt.Fprintf(os.Stderr, "Run 'c4sh help' for usage.\n")
			osExit(1)
		}
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `c4sh — Content-Addressed Shell

Usage:
  c4sh shell-init              Output shell integration script
  c4sh cd <file.c4m>           Enter a c4m file
  c4sh cd <path>               Navigate within a c4m
  c4sh ls [-l] [-a] [path]     List entries
  c4sh cat <path>              Output file content from store
  c4sh cp <src...> <dst>       Copy (real↔c4m, c4m↔c4m, multi-dest)
  c4sh mv <src> <dst>          Move/rename entries
  c4sh rm [-rf] <path...>      Remove entries
  c4sh mkdir [-p] <path...>    Create directory entries
  c4sh pool <c4m> <dir>        Bundle c4m + referenced store objects
  c4sh ingest <bundle>         Absorb a pool into local store
  c4sh rsync <c4m> <remote:>   Push c4m + store objects to remote
  c4sh rsync <remote:c4m> .    Pull c4m + store objects from remote

Setup:
  eval "$(c4sh shell-init)"    Add to .bashrc/.zshrc
`)
}

// fallthrough execs the real system command. On Unix this replaces the
// process; on Windows it runs the command and propagates the exit code.
func fallthrough_(cmd string, args []string) {
	path, err := findSystemCommand(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "c4sh: %s: command not found\n", cmd)
		osExit(127)
	}
	argv := append([]string{cmd}, args...)
	if err := execCommand(path, argv); err != nil {
		fmt.Fprintf(os.Stderr, "c4sh: exec %s: %v\n", cmd, err)
		osExit(1)
	}
}

// isInC4mContext returns true if the user is inside a c4m context.
func isInC4mContext() bool {
	return os.Getenv("C4_CONTEXT") != ""
}

// isC4mPath returns true if the path references a c4m file (has colon or .c4m extension).
func isC4mPath(p string) bool {
	if strings.Contains(p, ":") {
		return true
	}
	// Check if path or path.c4m exists as a c4m file
	if strings.HasSuffix(p, ".c4m") {
		return true
	}
	if _, err := os.Stat(p + ".c4m"); err == nil {
		return true
	}
	return false
}

// splitC4mPath splits "file.c4m:path/inside" into ("file.c4m", "path/inside").
// If there's no colon, the subpath is empty.
// Resolves missing .c4m extension: "project:src/" → "project.c4m", "src/"
func splitC4mPath(p string) (c4mFile string, subPath string) {
	if i := strings.Index(p, ":"); i >= 0 {
		c4mFile = p[:i]
		subPath = p[i+1:]
	} else {
		c4mFile = p
	}
	// Resolve missing .c4m extension
	if !strings.HasSuffix(c4mFile, ".c4m") {
		if _, err := os.Stat(c4mFile + ".c4m"); err == nil {
			c4mFile = c4mFile + ".c4m"
		}
	}
	subPath = strings.TrimPrefix(subPath, "/")
	return
}

// resolveInContext converts a bare path to a c4m colon path when inside
// a c4m context. For example, "." becomes "project.c4m:", "src/" becomes
// "project.c4m:src/". Paths that already contain ":" or ".c4m", or that
// start with "/" (absolute), are returned unchanged.
func resolveInContext(p string, cur *ctx.Context) string {
	// Already explicit c4m syntax
	if strings.Contains(p, ":") || strings.HasSuffix(p, ".c4m") {
		return p
	}
	// Absolute path — refers to real filesystem
	if strings.HasPrefix(p, "/") {
		return p
	}
	// "." or "" means c4m context root
	if p == "." || p == "" {
		return cur.C4mPath + ":"
	}
	// Relative path inside the c4m context
	resolved := cur.Resolve(p)
	return cur.C4mPath + ":" + resolved
}
