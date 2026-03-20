package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Avalanche-io/c4"
	c4store "github.com/Avalanche-io/c4/store"
)

// runIngest implements "c4sh ingest <bundle>" — absorb a pool's content
// into the local store. Copies all store objects from the bundle into
// the configured C4_STORE, making the bundle's c4m files locally usable.
func runIngest(args []string) {
	if len(args) < 1 {
		die("ingest: usage: c4sh ingest <bundle-dir>")
	}

	bundleDir := args[0]

	// Find the pool store inside the bundle
	poolStorePath := filepath.Join(bundleDir, "store")
	if _, err := os.Stat(poolStorePath); err != nil {
		die("ingest: no store/ directory in %s", bundleDir)
	}

	poolStore, err := c4store.NewTreeStore(poolStorePath)
	if err != nil {
		die("ingest: open pool store: %v", err)
	}

	// Open local store
	localStore, err := openStore()
	if err != nil {
		die("ingest: %v\nSet C4_STORE or run: c4 id -s (to create a store)", err)
	}

	// Walk pool store and copy missing objects
	var copied, skipped, errors int
	walkTreeStore(poolStorePath, func(id c4.ID) {
		if localStore.Has(id) {
			skipped++
			return
		}

		rc, err := poolStore.Open(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "c4sh: ingest: read %s: %v\n", id, err)
			errors++
			return
		}
		defer rc.Close()

		if _, err := localStore.Put(rc); err != nil {
			fmt.Fprintf(os.Stderr, "c4sh: ingest: store %s: %v\n", id, err)
			errors++
			return
		}
		copied++
	})

	// Copy any c4m files from the bundle to current directory
	var manifests []string
	entries, _ := os.ReadDir(bundleDir)
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".c4m") {
			src := filepath.Join(bundleDir, e.Name())
			dst := e.Name()
			if err := copyFile(src, dst); err != nil {
				fmt.Fprintf(os.Stderr, "c4sh: ingest: copy %s: %v\n", e.Name(), err)
			} else {
				manifests = append(manifests, dst)
			}
		}
	}

	fmt.Printf("Ingested: %d objects copied, %d already present", copied, skipped)
	if errors > 0 {
		fmt.Printf(", %d errors", errors)
	}
	fmt.Println()

	if len(manifests) > 0 {
		for _, m := range manifests {
			fmt.Printf("  %s ready\n", m)
		}
	}

	if errors > 0 {
		osExit(1)
	}
}

// walkTreeStore walks a TreeStore directory, calling fn for each C4 ID found.
// Handles the adaptive trie layout: walks 2-char prefix subdirectories
// recursively, yielding files whose names parse as valid C4 IDs.
func walkTreeStore(root string, fn func(c4.ID)) {
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		// Skip temp files
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}
		// Try to parse filename as C4 ID
		id, err := c4.Parse(info.Name())
		if err != nil {
			return nil
		}
		fn(id)
		return nil
	})
}
