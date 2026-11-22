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
		"climbing":     base + "/cards/../other",
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
	homeless := Env{GOOS: "windows", HomeDir: func() (string, error) { return "", os.ErrNotExist }}
	if _, err := homeless.Resolve("", ""); err != ErrNone {
		t.Fatalf("windows with nothing set: %v, want ErrNone", err)
	}
	_, err = homeless.Resolve("", "")
	for _, want := range []string{"atrium room set card_root", "D:/", "C:", "board"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the no-card-root refusal does not say %q: %v", want, err)
		}
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

func TestWindowsDefaultsToADataDriveElseLocalAppData(t *testing.T) {
	base := t.TempDir()
	t.Setenv("WORKTREE_ROOT", "")
	env := envAt(filepath.Join(base, "home"), "windows")
	d, e := filepath.Join(base, "D"), filepath.Join(base, "E")
	local := filepath.Join(base, "home", "AppData", "Local")
	env.DataDrives = func() []string { return []string{d, e} }
	env.LocalAppData = func() string { return local }

	if got, err := env.Resolve("", ""); err != nil || got != filepath.Join(d, "atrium", "cards") {
		t.Fatalf("with data drives: %q, %v, want the first one's atrium/cards", got, err)
	}
	env.DataDrives = func() []string { return nil }
	if got, err := env.Resolve("", ""); err != nil || got != filepath.Join(local, "atrium", "cards") {
		t.Fatalf("with only the system drive: %q, %v, want LOCALAPPDATA's atrium/cards", got, err)
	}
	env.LocalAppData = nil
	if got, err := env.Resolve("", ""); err != nil || got != filepath.Join(base, "home", "atrium", "cards") {
		t.Fatalf("with no LOCALAPPDATA: %q, %v, want the home folder's atrium/cards", got, err)
	}
	set := filepath.Join(base, "mine")
	if got, err := env.Resolve(set, ""); err != nil || got != set {
		t.Fatalf("the setting lost to the windows default: %q, %v", got, err)
	}
}

func TestARoomStartedByNameDefaultsToItsOwnCardsFolder(t *testing.T) {
	base := t.TempDir()
	env := envAt(filepath.Join(base, "home"), "windows")
	own := filepath.Join(base, "atrium", "room-a", "cards")
	SetRoomDefault(own)
	t.Cleanup(func() { SetRoomDefault("") })

	t.Setenv("WORKTREE_ROOT", filepath.Join(base, "wt"))
	if got, err := env.Resolve("", ""); err != nil || got != own {
		t.Fatalf("unset with a room default: %q, %v, want %q", got, err, own)
	}
	set := filepath.Join(base, "mine")
	if got, err := env.Resolve(set, ""); err != nil || got != set {
		t.Fatalf("the setting lost to the room default: %q, %v", got, err)
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
