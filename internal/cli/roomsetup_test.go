package cli

import (
	"bytes"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/roomspec"
	"github.com/dovholuknf/atrium/internal/store"
)

func runSetup(t *testing.T, stdin string, args ...string) (int, string) {
	t.Helper()
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(stdin))
	return runRoot(root, append([]string{"room", "setup"}, args...)), out.String()
}

func TestRoomSetupArgsAreExit1(t *testing.T) {
	for _, args := range [][]string{{}, {"--spec", "-"}, {"--spec", "-", "--plan", "--apply"}, {"--plan"}} {
		if code, out := runSetup(t, "", args...); code != 1 {
			t.Errorf("%v: exit %d: %s", args, code, out)
		}
	}
	if code, _ := runSetup(t, "version: 1\nbogus: 2\n", "--spec", "-", "--plan"); code != 1 {
		t.Errorf("a bad spec is exit 1, got %d", code)
	}
}

func TestRoomSetupPlanSaysWhatItWouldDoAndWritesNothing(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.ToSlash(filepath.Join(tmp, "work", "localai"))
	spec := "version: 1\nname: t\nos: " + runtime.GOOS + "\naccount: '" + roomspec.OSFS{}.Login() + "'\nwork_root: '" + root + "'\n"
	code, out := runSetup(t, spec, "--spec", "-", "--plan", "--json", "--db", filepath.Join(tmp, "no.db"))
	if code != 0 && code != 13 {
		t.Fatalf("exit %d: %s", code, out)
	}
	lk, err := roomspec.ParseLock([]byte(out))
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if lk.AppliedAt != "" || lk.WorkRoot != root {
		t.Errorf("%+v", lk)
	}
	if _, err := (roomspec.OSFS{}).Lstat(filepath.FromSlash(root)); err == nil {
		t.Error("a plan made the work root")
	}
	if code, out := runSetup(t, spec, "--spec", "-", "--plan", "--db", filepath.Join(tmp, "no.db")); !strings.Contains(out, "setup work-root ") {
		t.Errorf("text rows: %d %s", code, out)
	}
}

func TestExitCodeErrorIsHonouredAndSilent(t *testing.T) {
	var e error = exitCodeError{Code: 13}
	if !errors.Is(e, errAlreadySaid) {
		t.Fatal("an exit code has already said why")
	}
}

func TestRoomSettingsMapsTheStoreErrors(t *testing.T) {
	db := filepath.Join(t.TempDir(), "room.db")
	s := roomSettings{db: db}
	if v, err := s.Get("git_root"); err != nil || v != "" {
		t.Errorf("a room that has not run: %q %v", v, err)
	}
	if err := s.Set("git_root", "relative"); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("set is checked as `room set` checks: %v", err)
	}
	_ = store.ErrDatabaseInUse
}
