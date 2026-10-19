package cardroot

import (
	"path/filepath"
	"strings"
	"testing"
)

func fakeHome(t *testing.T, home string) {
	t.Helper()
	old := homeDir
	homeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { homeDir = old })
}

func TestCheckRefusesWhatARemovalCouldHarm(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	state := filepath.Join(base, "state")
	scm := filepath.Join(base, "scm")
	fakeHome(t, home)

	cases := map[string]string{
		"a drive root": filepath.VolumeName(base) + string(filepath.Separator),
		"the home":     home,
		"above home":   base,
		"the state":    state,
		"in the state": filepath.Join(state, "cards"),
		"above state":  filepath.Dir(state),
		"in scm_root":  filepath.Join(scm, "cards"),
		"relative":     "cards",
	}
	for name, path := range cases {
		if _, err := Check(path, state, scm); err == nil {
			t.Errorf("%s: %s was accepted", name, path)
		}
	}
	if _, err := Check(filepath.Join(home, "atrium", "cards"), state, scm); err != nil {
		t.Errorf("a folder of its own under home was refused: %v", err)
	}
}

func TestResolveDefaults(t *testing.T) {
	base := t.TempDir()
	fakeHome(t, filepath.Join(base, "home"))

	t.Setenv("WORKTREE_ROOT", filepath.Join(base, "wt"))
	got, err := Resolve("", "")
	if err != nil || got != filepath.Join(base, "wt", "atrium-cards") {
		t.Fatalf("with WORKTREE_ROOT: %q, %v", got, err)
	}

	t.Setenv("WORKTREE_ROOT", "")
	oldOS := goos
	t.Cleanup(func() { goos = oldOS })

	goos = "windows"
	if _, err := Resolve("", ""); err != ErrNone {
		t.Fatalf("windows with nothing set: %v, want ErrNone", err)
	}
	goos = "linux"
	got, err = Resolve("", "")
	if err != nil || got != filepath.Join(base, "home", "atrium", "cards") {
		t.Fatalf("off windows: %q, %v", got, err)
	}

	set := filepath.Join(base, "mine")
	if got, err = Resolve(set, ""); err != nil || got != set {
		t.Fatalf("a set root: %q, %v", got, err)
	}
	if _, err := Resolve(filepath.Join(base, "home"), ""); err == nil || !strings.Contains(err.Error(), "home") {
		t.Fatalf("the home folder was accepted: %v", err)
	}
}
