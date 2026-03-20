package ctx

import (
	"os"
	"testing"
)

// ---------------------------------------------------------------------------
// Current
// ---------------------------------------------------------------------------

func TestCurrentNil(t *testing.T) {
	orig := os.Getenv("C4_CONTEXT")
	t.Cleanup(func() { os.Setenv("C4_CONTEXT", orig) })

	os.Setenv("C4_CONTEXT", "")
	if c := Current(); c != nil {
		t.Errorf("Current() = %+v, want nil when C4_CONTEXT is empty", c)
	}
}

func TestCurrentSet(t *testing.T) {
	origCtx := os.Getenv("C4_CONTEXT")
	origCwd := os.Getenv("C4_CWD")
	t.Cleanup(func() {
		os.Setenv("C4_CONTEXT", origCtx)
		os.Setenv("C4_CWD", origCwd)
	})

	os.Setenv("C4_CONTEXT", "/home/user/project.c4m")
	os.Setenv("C4_CWD", "src/internal")

	c := Current()
	if c == nil {
		t.Fatal("Current() = nil, want non-nil")
	}
	if c.C4mPath != "/home/user/project.c4m" {
		t.Errorf("C4mPath = %q, want %q", c.C4mPath, "/home/user/project.c4m")
	}
	if c.CWD != "src/internal" {
		t.Errorf("CWD = %q, want %q", c.CWD, "src/internal")
	}
}

// ---------------------------------------------------------------------------
// C4mName
// ---------------------------------------------------------------------------

func TestC4mName(t *testing.T) {
	tests := []struct {
		c4mPath string
		want    string
	}{
		{"/path/to/project.c4m", "project"},
		{"/path/to/my-film.c4m", "my-film"},
		{"simple.c4m", "simple"},
		{"noext", "noext"},
		{"/abs/path/x.c4m", "x"},
	}

	for _, tt := range tests {
		t.Run(tt.c4mPath, func(t *testing.T) {
			c := &Context{C4mPath: tt.c4mPath}
			got := c.C4mName()
			if got != tt.want {
				t.Errorf("C4mName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Resolve
// ---------------------------------------------------------------------------

func TestResolve(t *testing.T) {
	tests := []struct {
		name string
		cwd  string
		path string
		want string
	}{
		// Empty/dot inputs return CWD unchanged.
		{"empty at root", "", "", ""},
		{"dot at root", "", ".", ""},
		{"empty in subdir", "src", "", "src"},
		{"dot in subdir", "src", ".", "src"},

		// Relative paths resolve against CWD.
		{"relative file at root", "", "README.md", "README.md"},
		{"relative file in subdir", "src", "main.go", "src/main.go"},
		{"relative nested", "src", "internal/core.go", "src/internal/core.go"},
		{"relative dir", "src", "internal/", "src/internal"},

		// Dotdot navigation.
		{"dotdot from subdir", "src/internal", "..", "src"},
		{"dotdot from root", "", "..", ""},
		{"dotdot past root clamps", "src", "../../..", ""},
		{"dotdot then descend", "src/internal", "../docs", "src/docs"},

		// Absolute paths (leading /) are resolved from c4m root.
		{"absolute path", "src", "/docs/guide.md", "docs/guide.md"},
		{"absolute root", "src/internal", "/", ""},
		{"absolute dotdot clamps", "src", "/../..", ""},

		// Clean path normalization.
		{"double slash", "", "src//main.go", "src/main.go"},
		{"trailing dot", "", "src/.", "src"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Context{C4mPath: "/test.c4m", CWD: tt.cwd}
			got := c.Resolve(tt.path)
			if got != tt.want {
				t.Errorf("Resolve(%q) with CWD=%q => %q, want %q", tt.path, tt.cwd, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Resolve edge cases
// ---------------------------------------------------------------------------

func TestResolveMultipleDotDot(t *testing.T) {
	c := &Context{C4mPath: "/project.c4m", CWD: "a/b/c"}

	// Going up 3 levels from a/b/c should reach root (empty).
	got := c.Resolve("../../..")
	if got != "" {
		t.Errorf("Resolve(../../..) from a/b/c = %q, want empty (root)", got)
	}

	// Going up 2 levels should reach "a".
	got = c.Resolve("../..")
	if got != "a" {
		t.Errorf("Resolve(../..) from a/b/c = %q, want %q", got, "a")
	}
}

func TestResolveDotDotClamp(t *testing.T) {
	c := &Context{C4mPath: "/project.c4m", CWD: ""}

	// Already at root, dotdot should stay at root.
	got := c.Resolve("..")
	if got != "" {
		t.Errorf("Resolve(..) at root = %q, want empty", got)
	}

	// Multiple dotdots still clamp.
	got = c.Resolve("../../../../..")
	if got != "" {
		t.Errorf("Resolve(deep dotdot) at root = %q, want empty", got)
	}
}
