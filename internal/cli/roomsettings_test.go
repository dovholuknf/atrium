package cli

import (
	"bytes"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func roomSettingRun(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	c := roomCmd()
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(args)
	err := c.Execute()
	return out.String(), err
}

func absRoot() string {
	if runtime.GOOS == "windows" {
		return `C:\Users\Public\atrium`
	}
	return "/srv/atrium"
}

func TestRoomSetGetGitRoot(t *testing.T) {
	db := filepath.Join(t.TempDir(), "room.db")
	s, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	if _, err := roomSettingRun(t, "set", "git_root", absRoot(), "--db", db); err != nil {
		t.Fatalf("set: %v", err)
	}
	out, err := roomSettingRun(t, "get", "git_root", "--db", db)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if strings.TrimSpace(out) != absRoot() {
		t.Fatalf("get = %q, want %q", out, absRoot())
	}
}

// A new machine has no database before its room first runs, and set makes it.
func TestRoomSetMakesTheDatabase(t *testing.T) {
	db := filepath.Join(t.TempDir(), "atrium.db")
	if _, err := roomSettingRun(t, "set", "git_root", absRoot(), "--db", db); err != nil {
		t.Fatalf("set: %v", err)
	}
	if out, err := roomSettingRun(t, "get", "git_root", "--db", db); err != nil || strings.TrimSpace(out) != absRoot() {
		t.Fatalf("get = %q, %v", out, err)
	}
}

func TestRoomSetRefusals(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "room.db")

	if _, err := roomSettingRun(t, "get", "git_root", "--db", db); err == nil ||
		!strings.Contains(err.Error(), "has not run here") {
		t.Fatalf("get on a missing db = %v", err)
	}
	if _, err := roomSettingRun(t, "set", "nonsense", "x", "--db", db); err == nil ||
		!strings.Contains(err.Error(), "unknown room setting") {
		t.Fatalf("unknown key = %v", err)
	}
	if _, err := roomSettingRun(t, "set", "git_root", "relative/dir", "--db", db); err == nil ||
		!strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative path = %v", err)
	}
}

func TestRoomSetRefusesRunningRoom(t *testing.T) {
	db := filepath.Join(t.TempDir(), "room.db")
	s, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// A room reads and writes while it runs; do the same so it holds its locks.
	if _, err := s.List(); err != nil {
		t.Fatal(err)
	}

	_, err = roomSettingRun(t, "set", "git_root", absRoot(), "--db", db)
	if err == nil || !strings.Contains(err.Error(), "stop it first") {
		t.Fatalf("set on an open store = %v", err)
	}
	if errors.Is(err, store.ErrDatabaseInUse) {
		t.Fatalf("the verb should word it, not pass the store error through: %v", err)
	}
	if v, _ := s.Setting("git_root"); v != "" {
		t.Fatalf("git_root was written to a held store: %q", v)
	}
}
