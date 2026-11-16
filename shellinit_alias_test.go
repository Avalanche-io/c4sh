package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Outside a c4m, the cat wrapper runs the user's own cat alias. shell-init
// must capture that alias's exact value in every shell it supports. zsh
// prints aliases as name=value, bash as alias name='value' with embedded
// quotes escaped, so the capture reads the value from the shell itself
// rather than parsing either format.

// runCatUnderShellInit writes a script that defines def (if any), loads
// shell-init for sh, and cats a two-line file, and returns its output.
func runCatUnderShellInit(t *testing.T, sh, def string) string {
	t.Helper()
	bin, err := exec.LookPath(sh)
	if err != nil {
		t.Skipf("%s not installed", sh)
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "f.txt")
	os.WriteFile(f, []byte("one\ntwo\n"), 0644)
	script, err := shellInitScript(sh)
	if err != nil {
		t.Fatal(err)
	}
	initPath := filepath.Join(dir, "init."+sh)
	os.WriteFile(initPath, []byte(script), 0644)

	var lines []string
	args := []string{}
	if sh == "bash" {
		lines = append(lines, "shopt -s expand_aliases")
	} else {
		args = append(args, "-f") // no user startup files
	}
	if def != "" {
		lines = append(lines, def)
	}
	// zsh's compdef exists only after compinit; its absence is not what
	// this checks.
	lines = append(lines, "compdef() { :; }", ". "+initPath, "cat "+f)
	s := filepath.Join(dir, "s."+sh)
	os.WriteFile(s, []byte(strings.Join(lines, "\n")+"\n"), 0644)

	cmd := exec.Command(bin, append(args, s)...)
	cmd.Env = []string{"HOME=" + dir, "PATH=" + os.Getenv("PATH")}
	out, _ := cmd.CombinedOutput()
	return string(out)
}

func TestShellInitKeepsTheUsersCatAlias(t *testing.T) {
	for _, sh := range []string{"zsh", "bash"} {
		for _, tc := range []struct{ def, want string }{
			{"", "one\ntwo\n"},
			{`alias cat="head -n 1"`, "one\n"},
			{`alias cat=rev`, "eno\nowt\n"},
			{`alias cat="sed -e 's/o/0/'"`, "0ne\ntw0\n"},
		} {
			t.Run(sh+" "+tc.def, func(t *testing.T) {
				if got := runCatUnderShellInit(t, sh, tc.def); got != tc.want {
					t.Errorf("cat outside a c4m printed %q, want %q", got, tc.want)
				}
			})
		}
	}
}
