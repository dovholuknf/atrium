//go:build integration

package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitTestRepo(t *testing.T, dir string, dirty bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	if dirty {
		if err := os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUninstallPurgeKeepsDirtyWorktreesAndTheBinary(t *testing.T) {
	state := filepath.Join(t.TempDir(), ".atrium")
	dirty := filepath.Join(state, "cards", "dirty", "work")
	clean := filepath.Join(state, "cards", "clean", "work")
	gitTestRepo(t, dirty, true)
	gitTestRepo(t, clean, false)
	other := filepath.Join(state, "hub", "atrium.db")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(state, "bin", "atrium")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	var calls []string
	var out bytes.Buffer
	o := uninstallOpts{purge: true, stateDir: state, exe: exe}
	if err := runUninstall(&out, o, fakeUninstallDeps(t, 0, &calls)); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(dirty, "wip.txt"), exe} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was deleted: %v", p, err)
		}
	}
	for _, p := range []string{clean, other} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s survived the purge", p)
		}
	}
	if !strings.Contains(out.String(), "keeping") {
		t.Errorf("kept paths not listed:\n%s", out.String())
	}
}
