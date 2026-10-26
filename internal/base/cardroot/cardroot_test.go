package cardroot

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func envAt(home, goos string) Env {
	return Env{GOOS: goos, HomeDir: func() (string, error) { return home, nil }}
}

func TestCheckRefusesWhatARemovalCouldHarm(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	state := filepath.Join(base, "state")
	scm := filepath.Join(base, "scm")
	env := envAt(home, "linux")

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
		if _, err := env.Check(path, state, scm); err == nil {
			t.Errorf("%s: %s was accepted", name, path)
		}
	}
	if _, err := env.Check(filepath.Join(home, "atrium", "cards"), state, scm); err != nil {
		t.Errorf("a folder of its own under home was refused: %v", err)
	}
}

func TestResolveDefaults(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	env := envAt(home, "linux")

	t.Setenv("WORKTREE_ROOT", filepath.Join(base, "wt"))
	got, err := env.Resolve("", "")
	if err != nil || got != filepath.Join(base, "wt", "atrium-cards") {
		t.Fatalf("with WORKTREE_ROOT: %q, %v", got, err)
	}

	t.Setenv("WORKTREE_ROOT", "")
	if _, err := envAt(home, "windows").Resolve("", ""); err != ErrNone {
		t.Fatalf("windows with nothing set: %v, want ErrNone", err)
	}
	got, err = env.Resolve("", "")
	if err != nil || got != filepath.Join(base, "home", "atrium", "cards") {
		t.Fatalf("off windows: %q, %v", got, err)
	}

	set := filepath.Join(base, "mine")
	if got, err = env.Resolve(set, ""); err != nil || got != set {
		t.Fatalf("a set root: %q, %v", got, err)
	}
	if _, err := env.Resolve(home, ""); err == nil || !strings.Contains(err.Error(), "home") {
		t.Fatalf("the home folder was accepted: %v", err)
	}
}

func TestCheckRefusesARootWithAnUnreadableAncestor(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs a folder this process cannot search")
	}
	base := t.TempDir()
	locked := filepath.Join(base, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	env := envAt(filepath.Join(base, "home"), "linux")
	if _, err := env.Check(filepath.Join(locked, "cards"), filepath.Join(base, "state"), ""); err == nil {
		t.Fatal("a root under an unreadable folder was accepted")
	}
}
