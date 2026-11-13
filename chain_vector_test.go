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
// for the chain grammar (SPECIFICATION.md "No Closing Validator" /
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

// preTrailingIDPin is the c4 version c4sh requires until the suite's 1.1
// release. That version accepted a bare C4 ID at the end of a c4m file as
// a "closing validator"; from c4 1.1 it is a format error (ruled
// 2026-11-08). c4sh moves its c4 dependency at the 1.1 release, not
// before, so the refusal test below skips until go.mod moves.
const preTrailingIDPin = "v1.0.17-0.20260713200829-ae6f19045c62"

func TestChainVectorTrailingIDRefused(t *testing.T) {
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mod), "github.com/Avalanche-io/c4 "+preTrailingIDPin) {
		t.Skip("c4sh still requires c4 " + preTrailingIDPin + ", before the trailing-ID rule; this runs once the c4 dependency moves at the 1.1 release")
	}

	dir := filepath.Join("testdata", "chain-vector")
	data, err := os.ReadFile(filepath.Join(dir, "vector.c4m"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.ReadFile(filepath.Join(dir, "resolved-root-id.txt"))
	if err != nil {
		t.Fatal(err)
	}
	// The vector with its own resolved ID appended: the shape c4 diff >>
	// wrote through c4 1.0.26.
	p := filepath.Join(t.TempDir(), "trailing.c4m")
	if err := os.WriteFile(p, append(data, []byte(strings.TrimSpace(string(root))+"\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = loadManifest(p)
	if err == nil || !strings.Contains(err.Error(), "never ends with its own ID") {
		t.Fatalf("a c4m file ending with a bare C4 ID must be refused as a trailing ID, got %v", err)
	}
}
