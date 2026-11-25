//go:build integration

package runnersetup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodexCodeModeHostIsLookedForBesideCodex(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	exe := filepath.Join(dir, "codex.exe")
	if err := os.WriteFile(exe, []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	env := Env{Exe: exe, GOOS: "windows"}
	if r := codexCodeModeHostCheck(env); r.State != Fail || r.Fix != FixExplain {
		t.Fatalf("no host: %+v", r)
	}
	if err := os.WriteFile(filepath.Join(dir, "codex-code-mode-host.exe"), []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if r := codexCodeModeHostCheck(env); r.State != OK {
		t.Fatalf("host beside codex: %+v", r)
	}
	shim := filepath.Join(dir, "codex.cmd")
	if err := os.WriteFile(shim, []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if r := codexCodeModeHostCheck(Env{Exe: shim, GOOS: "windows"}); r.State != NA {
		t.Fatalf("npm shim: %+v", r)
	}
}
