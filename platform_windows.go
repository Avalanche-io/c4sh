//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// isBrokenPipe returns true if the error indicates a broken pipe.
// On Windows, EPIPE doesn't exist; check for "broken pipe" in the message.
func isBrokenPipe(err error) bool {
	return errors.Is(err, os.ErrClosed) || strings.Contains(err.Error(), "broken pipe")
}

// execCommand runs the given command as a child process and exits.
// On Windows, there's no syscall.Exec equivalent, so we run the
// command and propagate its exit code.
func execCommand(path string, argv []string) error {
	cmd := exec.Command(path, argv[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	os.Exit(0)
	return nil // unreachable
}

// findSystemCommand finds the real system command on Windows.
func findSystemCommand(cmd string) (string, error) {
	// On Windows, check common locations and PATH
	for _, ext := range []string{"", ".exe", ".cmd", ".bat"} {
		for _, dir := range []string{`C:\Windows\System32`, `C:\Windows`} {
			p := filepath.Join(dir, cmd+ext)
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}
	return exec.LookPath(cmd)
}
