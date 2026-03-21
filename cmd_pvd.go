package main

import (
	"fmt"
	"os"

	"github.com/Avalanche-io/c4sh/internal/ctx"
)

// runPvd implements "c4sh pvd" — print virtual directory.
//
// Always works:
//   - In c4m context: prints the full resolvable c4m path (e.g., /tmp/c4-test/c4.c4m:src/)
//   - Outside c4m context: prints the real working directory (same as pwd)
func runPvd() {
	cur := ctx.Current()
	if cur == nil {
		// Not in c4m context — behave like pwd
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "c4sh: pvd: %v\n", err)
			osExit(1)
		}
		fmt.Println(wd)
		return
	}

	// In c4m context — full resolvable path with leading /
	fmt.Printf("%s:/%s\n", cur.C4mPath, cur.CWD)
}
