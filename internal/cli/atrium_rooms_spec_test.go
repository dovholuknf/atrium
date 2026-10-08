package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
)

const cliSpec = "version: 1\nname: sg3\nos: windows\naccount: localai\n"

// spec set, spec get and lock get round trip through the store, and a bad spec is refused with a sentence.
func TestRoomsSpecSetGetAndLockGet(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "hub.db")
	st, err := hubstore.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Add("sg3", hubstore.TransportDirect); err != nil {
		t.Fatal(err)
	}
	st.Close()

	file := filepath.Join(dir, "room.yaml")
	if err := os.WriteFile(file, []byte(cliSpec), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := runRooms(t, "spec", "get", "sg3", "--atrium-db", db); err == nil {
		t.Fatalf("get with none: %s", out)
	}
	if out, err := runRooms(t, "spec", "set", "sg3", file, "--atrium-db", db); err != nil || !strings.Contains(out, "sha256") {
		t.Fatalf("set: %v %s", err, out)
	}
	if out, err := runRooms(t, "spec", "get", "sg3", "--atrium-db", db); err != nil || out != cliSpec {
		t.Fatalf("get: %v %q", err, out)
	}
	bad := filepath.Join(dir, "bad.yaml")
	os.WriteFile(bad, []byte("version: 1\nname: sg3\npassword: x\n"), 0o600)
	if out, err := runRooms(t, "spec", "set", "sg3", bad, "--atrium-db", db); err == nil || !strings.Contains(err.Error(), "credential") {
		t.Fatalf("a credential was accepted: %v %s", err, out)
	}
	if out, err := runRooms(t, "spec", "set", "nobody", file, "--atrium-db", db); err == nil || !strings.Contains(err.Error(), "sg3") {
		t.Fatalf("unknown room: %v %s", err, out)
	}

	if out, err := runRooms(t, "lock", "get", "sg3", "--atrium-db", db); err == nil {
		t.Fatalf("lock with none: %s", out)
	}
	st, _ = hubstore.Open(db)
	if _, err := st.PutRoomLock("sg3", []byte(`{"version":1,"os":"windows"}`)); err != nil {
		t.Fatal(err)
	}
	st.Close()
	if out, err := runRooms(t, "lock", "get", "sg3", "--atrium-db", db); err != nil || !strings.Contains(out, `"os":"windows"`) {
		t.Fatalf("lock get: %v %s", err, out)
	}
}

// add --spec refuses a spec for another name before the room is made, so nothing is half made.
func TestRoomsAddWithAMismatchedSpecMakesNothing(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "hub.db")
	file := filepath.Join(dir, "room.yaml")
	os.WriteFile(file, []byte("version: 1\nname: other\n"), 0o600)
	if out, err := runRooms(t, "add", "sg3", "--spec", file, "--atrium-db", db, "--link-advertise", "127.0.0.1:1"); err == nil {
		t.Fatalf("accepted: %s", out)
	}
	st, err := hubstore.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if rs, _ := st.Rooms(); len(rs) != 0 {
		t.Fatalf("a room was made: %v", rs)
	}
}

// add --spec stores the spec with the room.
func TestRoomsAddWithASpecStoresIt(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "hub.db")
	file := filepath.Join(dir, "room.yaml")
	os.WriteFile(file, []byte(cliSpec), 0o600)
	if err := (link.Keys{Dir: dir}).EnsureCA([]string{"127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	if out, err := runRooms(t, "add", "sg3", "--spec", file, "--atrium-db", db, "--atrium-dir", dir,
		"--link-advertise", "127.0.0.1:7000"); err != nil {
		t.Fatalf("add: %v %s", err, out)
	}
	st, err := hubstore.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if sp, err := st.RoomSpecOf("sg3"); err != nil || sp.YAML != cliSpec {
		t.Fatalf("spec = %+v %v", sp, err)
	}
}

// The pulled spec is written 0600, atomically, and replaces what was there.
func TestWriteFileAtomicIsPrivateAndReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "room.yaml")
	os.WriteFile(path, []byte("old"), 0o644)
	if err := writeFileAtomic(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	fi, _ := os.Stat(path)
	if string(b) != "new" || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%q %v", b, fi.Mode())
	}
	if es, _ := os.ReadDir(filepath.Dir(path)); len(es) != 1 {
		t.Fatalf("a temp file was left: %v", es)
	}
}
