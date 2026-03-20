// Package ctx manages c4sh shell context via environment variables.
//
// C4_CONTEXT: absolute path to the c4m file being edited
// C4_CWD: current path within the c4m (empty = root)
package ctx

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Context represents the current c4sh session state.
type Context struct {
	C4mPath string // Absolute path to the c4m file
	CWD     string // Current path within the c4m (empty = root)
}

// Current returns the active c4m context, or nil if not in one.
func Current() *Context {
	c4mPath := os.Getenv("C4_CONTEXT")
	if c4mPath == "" {
		return nil
	}
	return &Context{
		C4mPath: c4mPath,
		CWD:     os.Getenv("C4_CWD"),
	}
}

// C4mName returns the base name of the c4m file without extension.
func (c *Context) C4mName() string {
	base := filepath.Base(c.C4mPath)
	return strings.TrimSuffix(base, ".c4m")
}

// Resolve resolves a relative path against the current CWD within the c4m.
// Returns the normalized subpath (no leading slash). Paths that traverse
// above the c4m root are clamped to root (empty string).
func (c *Context) Resolve(p string) string {
	if p == "" || p == "." {
		return c.CWD
	}
	var resolved string
	if strings.HasPrefix(p, "/") {
		resolved = path.Clean(p)
	} else {
		resolved = path.Clean(path.Join(c.CWD, p))
	}
	resolved = strings.TrimPrefix(resolved, "/")
	if resolved == "." || resolved == ".." || strings.HasPrefix(resolved, "../") {
		return ""
	}
	return resolved
}
