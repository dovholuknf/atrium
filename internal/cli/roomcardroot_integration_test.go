//go:build integration

package cli

import (
	"path/filepath"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/cardroot"
	"github.com/dovholuknf/atrium/internal/store"
)

func TestTheFirstWindowsCardRootIsKeptWhenADriveAppears(t *testing.T) {
	t.Setenv("WORKTREE_ROOT", "")
	base := t.TempDir()
	st, err := store.Open(filepath.Join(base, "atrium.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	local := filepath.Join(base, "Local")
	win := cardroot.Env{GOOS: "windows", HomeDir: func() (string, error) { return filepath.Join(base, "home"), nil },
		LocalAppData: func() string { return local }}
	if err := pinCardRoot(st, win); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(local, "atrium", "cards")
	win.DataDrives = func() []string { return []string{filepath.Join(base, "D")} }
	if err := pinCardRoot(st, win); err != nil {
		t.Fatal(err)
	}
	set, _ := st.Setting(cardroot.Setting)
	if got, err := win.Resolve(set, ""); err != nil || got != want {
		t.Fatalf("after a drive appeared the card root is %q, %v, want the first one %s", got, err, want)
	}
	mine := filepath.ToSlash(filepath.Join(base, "mine"))
	if err := st.SetSetting(cardroot.Setting, mine); err != nil {
		t.Fatal(err)
	}
	if err := pinCardRoot(st, win); err != nil {
		t.Fatal(err)
	}
	if set, _ := st.Setting(cardroot.Setting); set != mine {
		t.Fatalf("a card_root somebody set became %q", set)
	}
}
