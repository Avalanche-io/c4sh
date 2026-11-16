package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// With no store configured, every cp that needs one says how to configure
// it: C4_STORE, or a store line in ~/.c4/config. c4 has no init verb.

func TestCpWithNoStoreNamesTheRealConfiguration(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	os.MkdirAll(src, 0755)
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0644)
	c4mPath := filepath.Join(dir, "x.c4m")
	os.WriteFile(c4mPath, nil, 0644)

	cases := map[string][]string{
		"capture":    {src + "/", c4mPath + ":"},
		"extract":    {c4mPath + ":", filepath.Join(dir, "out") + "/"},
		"multi-dest": {src + "/", c4mPath + ":", filepath.Join(dir, "copy") + "/"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("C4_STORE", "")
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("C4_CONTEXT", "")
			withTestExit(t)

			r, w, _ := os.Pipe()
			orig := os.Stderr
			os.Stderr = w
			code := catchExit(func() { runCp(args) })
			w.Close()
			os.Stderr = orig
			msg, _ := io.ReadAll(r)

			if code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
			for _, want := range []string{"C4_STORE", "~/.c4/config"} {
				if !strings.Contains(string(msg), want) {
					t.Errorf("message %q does not name %s", msg, want)
				}
			}
			if strings.Contains(string(msg), "c4 init") {
				t.Errorf("message %q names c4 init, which does not exist", msg)
			}
		})
	}
}

// No c4sh message may tell a user to run a c4 command that doesn't exist.
// The 1.0.26 "run c4 init" hints sat behind a nil-store check that
// openStore already makes, so no run could reach them; this checks the
// source, where they were.
func TestNoMessageNamesC4Init(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "c4 init") {
			t.Errorf("%s names \"c4 init\", which is not a c4 command", f)
		}
	}
}
