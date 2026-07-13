package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Avalanche-io/c4/c4m"
)

// TestChainVectorConformance exercises the cross-implementation chain vector
// for the chain-grammar erratum (SPECIFICATION.md "The Closing Validator" /
// C4M-STANDARD §10.5) through c4sh's own c4m loading path (loadManifest ->
// c4m.NewDecoder().Decode()). The fixtures under testdata/chain-vector are
// byte-identical copies of the shared conformance contract in the c4 module;
// they are the contract and must never be regenerated.
func TestChainVectorConformance(t *testing.T) {
	dir := filepath.Join("testdata", "chain-vector")

	t.Run("vector resolves to root id", func(t *testing.T) {
		want, err := os.ReadFile(filepath.Join(dir, "resolved-root-id.txt"))
		if err != nil {
			t.Fatalf("reading resolved-root-id.txt: %v", err)
		}
		wantID := strings.TrimSpace(string(want))

		m, err := loadManifest(filepath.Join(dir, "vector.c4m"))
		if err != nil {
			t.Fatalf("vector.c4m must load, got error: %v", err)
		}
		got := m.ComputeC4ID().String()
		if got != wantID {
			t.Fatalf("vector.c4m resolved to %s, want %s", got, wantID)
		}
	})

	t.Run("bad-validator is rejected", func(t *testing.T) {
		_, err := loadManifest(filepath.Join(dir, "bad-validator.c4m"))
		if err == nil {
			t.Fatal("bad-validator.c4m must be rejected, got nil error")
		}
		if !errors.Is(err, c4m.ErrPatchIDMismatch) {
			t.Fatalf("bad-validator.c4m rejected with %v, want ErrPatchIDMismatch", err)
		}
	})

	t.Run("bad-checkpoint is rejected", func(t *testing.T) {
		_, err := loadManifest(filepath.Join(dir, "bad-checkpoint.c4m"))
		if err == nil {
			t.Fatal("bad-checkpoint.c4m must be rejected, got nil error")
		}
		if !errors.Is(err, c4m.ErrPatchIDMismatch) {
			t.Fatalf("bad-checkpoint.c4m rejected with %v, want ErrPatchIDMismatch", err)
		}
	})
}
