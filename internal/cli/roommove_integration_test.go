//go:build integration

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/link"
	"github.com/dovholuknf/atrium/internal/store"
)

// oldRoom is a room laid out the old way: keys in one folder, the database and its neighbours in another, with
// two cards.
func oldRoom(t *testing.T, name string) (link.Keys, string) {
	t.Helper()
	base := t.TempDir()
	keys := link.Keys{Dir: filepath.Join(base, "room")}
	dbDir := filepath.Join(base, "data")
	for _, d := range []string{keys.Dir, filepath.Join(keys.Dir, "identities"), filepath.Join(dbDir, "scrollback"),
		filepath.Join(dbDir, "identities")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(keys.Dir, "room.json"):               `{"hub": "127.0.0.1:7779", "room": "` + name + `"}`,
		filepath.Join(keys.Dir, "room.key"):                "key",
		filepath.Join(keys.Dir, "identities", "link.json"): "link",
		filepath.Join(keys.Dir, "restart.log"):             "restarted",
		filepath.Join(keys.Dir, "daemon.json"):             "{}",
		filepath.Join(dbDir, "scrollback", "card.bin"):     "screen",
		filepath.Join(dbDir, "identities", "board.json"):   "board",
		filepath.Join(dbDir, "ptyhost.log"):                "host",
		filepath.Join(dbDir, "unrelated.txt"):              "not the room's",
	}
	for p, body := range files {
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	db := filepath.Join(dbDir, "atrium.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"one", "two"} {
		if _, _, err := st.Register(store.Observed{WireName: w, Worktree: filepath.ToSlash(t.TempDir()),
			Runner: "claude"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	return keys, db
}

func TestRoomMoveCopiesTheRoomIntoItsOwnFolder(t *testing.T) {
	keys, db := oldRoom(t, "room-a")
	lay, err := newRoomLayout(filepath.Join(t.TempDir(), "atrium"), "room-a")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := moveRoom(&out, keys, db, lay); err != nil {
		t.Fatalf("move: %v\n%s", err, out.String())
	}
	if n := daemon.CountCards(lay.db()); n != 2 {
		t.Errorf("the moved database holds %d cards, want 2", n)
	}
	for _, p := range []string{"room.json", "room.key", "identities/link.json", "identities/board.json",
		"scrollback/card.bin"} {
		if _, err := os.Stat(filepath.Join(lay.config(), p)); err != nil {
			t.Errorf("config lacks %s: %v", p, err)
		}
	}
	for _, p := range []string{"restart.log", "ptyhost.log"} {
		if _, err := os.Stat(filepath.Join(lay.logs(), p)); err != nil {
			t.Errorf("logs lacks %s: %v", p, err)
		}
	}
	for _, p := range []string{"daemon.json", "unrelated.txt", "restart.log"} {
		if _, err := os.Stat(filepath.Join(lay.config(), p)); err == nil {
			t.Errorf("config holds %s, which is not the room's to carry", p)
		}
	}
	if _, err := os.Stat(db); err != nil {
		t.Errorf("the old database is gone: %v", err)
	}
	if err := lay.checkJoined(link.Keys{Dir: lay.config()}); err != nil {
		t.Errorf("the moved keys do not start as room-a: %v", err)
	}
}

func TestRoomMoveRefusesARunningRoomAnotherRoomAndAFullTarget(t *testing.T) {
	keys, db := oldRoom(t, "room-a")
	root := filepath.Join(t.TempDir(), "atrium")

	other, _ := newRoomLayout(root, "room-b")
	if err := moveRoom(&bytes.Buffer{}, keys, db, other); err == nil || !strings.Contains(err.Error(), "room-a") {
		t.Errorf("moving room-a as room-b: %v", err)
	}

	lay, _ := newRoomLayout(root, "room-a")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := moveRoom(&bytes.Buffer{}, keys, db, lay); err == nil || !strings.Contains(err.Error(), "stop the room") {
		t.Errorf("moving an open database: %v", err)
	}
	st.Close()

	if err := os.MkdirAll(lay.config(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lay.config(), "room.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveRoom(&bytes.Buffer{}, keys, db, lay); err == nil || !strings.Contains(err.Error(), "already holds") {
		t.Errorf("moving onto a folder with files: %v", err)
	}
}

func TestARoomNameNeedsARootAndCannotClimbOutOfIt(t *testing.T) {
	t.Setenv(rootEnv, "")
	if _, err := newRoomLayout("", "room-a"); err == nil {
		t.Error("a name with no root was taken")
	}
	for _, bad := range []string{"..", "a/b", `a\b`, "room a"} {
		if _, err := newRoomLayout(filepath.Join(t.TempDir(), "atrium"), bad); err == nil {
			t.Errorf("%q was taken as a room name", bad)
		}
	}
	root := filepath.Join(t.TempDir(), "atrium")
	t.Setenv(rootEnv, root)
	lay, err := newRoomLayout("", "room-a")
	if err != nil || lay.db() != filepath.Join(root, "room-a", "config", "atrium.db") ||
		lay.cards() != filepath.Join(root, "room-a", "cards") {
		t.Fatalf("from %s: %+v, %v", rootEnv, lay, err)
	}
}
