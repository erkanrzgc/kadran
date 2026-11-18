package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestNoGitCredentialsFlag, --git-credentials bayrağının kalktığını doğrular:
// bayrak ayrıştırması root denetiminden önce koşar ve bilinmeyen bayrakta çıkar.
func TestNoGitCredentialsFlag(t *testing.T) {
	if os.Getenv("KADRAN_EXEC_FLAG_TEST") == "1" {
		os.Args = []string{"kadran-exec", "--git-credentials", "x"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestNoGitCredentialsFlag$")
	cmd.Env = append(os.Environ(), "KADRAN_EXEC_FLAG_TEST=1")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "flag provided but not defined: -git-credentials") {
		t.Fatalf("flag still accepted: err=%v out=%s", err, out)
	}
}
