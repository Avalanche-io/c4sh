//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// isBrokenPipe returns true if the error is a broken pipe (EPIPE).
func isBrokenPipe(err error) bool {
	return errors.Is(err, syscall.EPIPE)
}

// execCommand replaces the current process with the given command.
// On Unix, this uses syscall.Exec for a true process replacement.
func execCommand(path string, argv []string) error {
	return syscall.Exec(path, argv, os.Environ())
}

// findSystemCommand finds the real system command, skipping c4sh aliases.
func findSystemCommand(cmd string) (string, error) {
	for _, dir := range []string{"/usr/bin", "/bin", "/usr/local/bin"} {
		p := filepath.Join(dir, cmd)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return exec.LookPath(cmd)
}
