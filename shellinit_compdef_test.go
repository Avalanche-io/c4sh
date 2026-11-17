package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// zsh's compdef exists only after compinit. shell-init must load without a
// word on stderr in a zsh that never ran compinit, and must still register
// its completion in one that did.

// runZshWithShellInit loads shell-init in zsh -f (no user startup files),
// after the given setup lines, then runs probe. It returns stdout and
// stderr separately.
func runZshWithShellInit(t *testing.T, setup []string, probe string) (string, string) {
	t.Helper()
	bin, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed")
	}
	dir := t.TempDir()
	script, err := shellInitScript("zsh")
	if err != nil {
		t.Fatal(err)
	}
	initPath := filepath.Join(dir, "init.zsh")
	os.WriteFile(initPath, []byte(script), 0644)
	lines := append(append([]string{}, setup...), ". "+initPath, probe)
	s := filepath.Join(dir, "s.zsh")
	os.WriteFile(s, []byte(strings.Join(lines, "\n")+"\n"), 0644)

	cmd := exec.Command(bin, "-f", s)
	cmd.Env = []string{"HOME=" + dir, "PATH=" + os.Getenv("PATH")}
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("zsh: %v\nstderr: %s", err, errOut.String())
	}
	return out.String(), errOut.String()
}

func TestShellInitLoadsSilentlyInZshWithoutCompinit(t *testing.T) {
	out, errOut := runZshWithShellInit(t, nil, "whence -w __c4sh_complete")
	if errOut != "" {
		t.Errorf("loading shell-init printed on stderr: %q", errOut)
	}
	if !strings.Contains(out, "__c4sh_complete: function") {
		t.Errorf("shell-init did not load: %q", out)
	}
}

func TestShellInitRegistersCompletionInZshWithCompinit(t *testing.T) {
	out, errOut := runZshWithShellInit(t,
		[]string{"autoload -Uz compinit", "compinit -u -D"},
		`print -r -- "ls=${_comps[ls]} cd=${_comps[cd]}"`)
	if errOut != "" {
		t.Errorf("loading shell-init printed on stderr: %q", errOut)
	}
	if want := "ls=__c4sh_complete cd=__c4sh_complete"; !strings.Contains(out, want) {
		t.Errorf("completion registrations %q, want %q", strings.TrimSpace(out), want)
	}
}
