//go:build integration && windows

package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func denyList(t *testing.T, dir string) {
	t.Helper()
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	who := "*" + u.User.Sid.String()
	if out, err := exec.Command("icacls", dir, "/deny", who+":(RD)").CombinedOutput(); err != nil {
		t.Fatalf("icacls /deny: %v %s", err, out)
	}
	t.Cleanup(func() {
		if out, err := exec.Command("icacls", dir, "/remove:d", who).CombinedOutput(); err != nil {
			t.Errorf("icacls /remove:d: %v %s", err, out)
		}
	})
}

func junction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v %s", err, out)
	}
}

func TestAllowedSwapExeUnderAFolderThisUserCannotList(t *testing.T) {
	mid := filepath.Join(t.TempDir(), "mid")
	home, inst, elsewhere := filepath.Join(mid, "home"), filepath.Join(mid, "inst"), filepath.Join(mid, "elsewhere")
	for _, d := range []string{home, inst, elsewhere} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ok := filepath.Join(home, "atrium.new.exe")
	writeFile(t, ok, "x")
	writeFile(t, filepath.Join(inst, "atrium-zrok.new.exe"), "x")
	writeFile(t, filepath.Join(elsewhere, "atrium.new.exe"), "x")
	wants := map[string]string{}
	for _, p := range []string{ok, filepath.Join(inst, "atrium-zrok.new.exe")} {
		w, err := filepath.EvalSymlinks(p) // long form, resolved before the folder is unlistable
		if errors.Is(err, os.ErrPermission) {
			w, err = p, nil // an ancestor this user cannot list: t.TempDir is already the long form
		}
		if err != nil {
			t.Fatal(err)
		}
		wants[p] = w
	}
	denyList(t, mid)
	dirs := []string{home, inst}

	for p, want := range wants {
		got, err := allowedSwapExe(p, dirs)
		if err != nil {
			t.Errorf("%s refused under an unlistable folder: %v", p, err)
		} else if !strings.EqualFold(got, want) {
			t.Errorf("%s resolved to %s, want %s", p, got, want)
		}
	}
	if _, err := allowedSwapExe(filepath.Join(elsewhere, "atrium.new.exe"), dirs); err == nil {
		t.Error("a build outside home and the install dir allowed under an unlistable folder")
	}
}

func TestAllowedSwapExeRefusesAJunctionOutOfHome(t *testing.T) {
	home, elsewhere := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(elsewhere, "atrium.new.exe"), 0o700); err != nil {
		t.Fatal(err)
	}
	junction(t, filepath.Join(home, "atrium.new.exe"), filepath.Join(elsewhere, "atrium.new.exe"))
	writeFile(t, filepath.Join(elsewhere, "atrium-zrok.new.exe"), "x")
	escapes := []string{filepath.Join(home, "atrium.new.exe")}
	if os.Symlink(filepath.Join(elsewhere, "atrium-zrok.new.exe"), filepath.Join(home, "atrium-zrok.new.exe")) == nil {
		escapes = append(escapes, filepath.Join(home, "atrium-zrok.new.exe"))
	}
	for _, p := range escapes {
		if _, err := allowedSwapExe(p, []string{home}); err == nil {
			t.Errorf("%s allowed through a link out of home", p)
		}
	}

	inst := filepath.Join(t.TempDir(), "inst")
	junction(t, inst, elsewhere)
	if _, err := os.Stat(filepath.Join(inst, "atrium-zrok.new.exe")); err != nil {
		t.Skipf("this process may not follow a junction (redirection guard): %v", err)
	}
	if _, err := allowedSwapExe(escapes[0], []string{home}); err == nil || !strings.Contains(err.Error(), "not in the home folder") {
		t.Errorf("a junction out of home refused for the wrong reason: %v", err)
	}
	if _, err := allowedSwapExe(filepath.Join(inst, "atrium-zrok.new.exe"), []string{inst}); err != nil {
		t.Errorf("an install dir reached through a junction refused: %v", err)
	}
}
