package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFreshRoomStateIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix modes")
	}
	dir := filepath.Join(t.TempDir(), "room")
	os.MkdirAll(dir, 0o755)
	db := filepath.Join(dir, "atrium.db")
	d, err := New(Options{DBPath: db})
	if err != nil {
		t.Fatal(err)
	}
	defer d.st.Close()
	check := func(p string, want os.FileMode) {
		fi, err := os.Stat(p)
		if os.IsNotExist(err) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s is %o, want %o", p, got, want)
		}
	}
	check(dir, 0o700)
	check(db, 0o600)
	check(db+"-wal", 0o600)
	check(db+"-shm", 0o600)
}
