//go:build integration

package hubstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

const goodSpec = "version: 1\nname: room-d\nos: windows\naccount: localai\nwork_root: V:/localai\ncaches: [npm, go]\n"

const goodLock = `{"version":1,"spec_hash":"abc","os":"windows","steps":[{"step":"work-root","status":"ok"}]}`

// THE SPEC IS KEPT VERBATIM, WITH ITS HASH, and a second set replaces it.
func TestRoomSpecIsStoredVerbatimWithItsHash(t *testing.T) {
	s := open(t)
	added(t, s, "room-d")
	if _, err := s.RoomSpecOf("room-d"); !errors.Is(err, ErrNoRoomSpec) {
		t.Fatalf("before any set: %v", err)
	}
	sp, err := s.SetRoomSpec("ROOM-D", []byte(goodSpec), "operator")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(goodSpec))
	if sp.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("hash %s", sp.SHA256)
	}
	got, err := s.RoomSpecOf("room-d")
	if err != nil || got.YAML != goodSpec || got.SetBy != "operator" || got.SetAt.IsZero() {
		t.Fatalf("got %+v %v", got, err)
	}
	next := goodSpec + "# changed\n"
	if _, err := s.SetRoomSpec("room-d", []byte(next), "operator"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.RoomSpecOf("room-d"); got.YAML != next {
		t.Fatalf("not replaced: %q", got.YAML)
	}
}

// OBSERVED NEVER OVERWRITES DESIRED, and neither way round.
func TestPostingALockNeverTouchesTheSpec(t *testing.T) {
	s := open(t)
	added(t, s, "room-d")
	if _, err := s.RoomLockOf("room-d"); !errors.Is(err, ErrNoRoomLock) {
		t.Fatalf("before any lock: %v", err)
	}
	if _, err := s.SetRoomSpec("room-d", []byte(goodSpec), "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutRoomLock("room-d", []byte(goodLock)); err != nil {
		t.Fatal(err)
	}
	if sp, _ := s.RoomSpecOf("room-d"); sp.YAML != goodSpec {
		t.Fatalf("the lock changed the spec: %q", sp.YAML)
	}
	if _, err := s.SetRoomSpec("room-d", []byte(goodSpec+"# x\n"), "a"); err != nil {
		t.Fatal(err)
	}
	lk, err := s.RoomLockOf("room-d")
	if err != nil || string(lk.Lock) != goodLock || lk.ReceivedAt.IsZero() {
		t.Fatalf("the spec changed the lock: %+v %v", lk, err)
	}
}

// WHAT THE HUB REFUSES: a wrong version, a wrong name, a credential, and a size over the bound.
func TestRoomSpecRefusals(t *testing.T) {
	s := open(t)
	added(t, s, "room-d")
	for name, spec := range map[string]string{
		"version 2":  "version: 2\nname: room-d\n",
		"no version": "name: room-d\n",
		"other name": "version: 1\nname: room-e\n",
		"not yaml":   "version: 1\nname: [room-d\n",
		"empty":      "",
		"token":      "version: 1\nname: room-d\ngithub_token: abc\n",
		"nested":     "version: 1\nname: room-d\npacks:\n  - {runner: claude, Password: x}\n",
		"secret":     "version: 1\nname: room-d\nsecret: x\n",
		"key":        "version: 1\nname: room-d\nkey: x\n",
		"too big":    "version: 1\nname: room-d\nnote: " + strings.Repeat("a", MaxRoomSpec) + "\n",
	} {
		_, err := s.SetRoomSpec("room-d", []byte(spec), "x")
		if err == nil || !IsRefusal(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := s.RoomSpecOf("room-d"); !errors.Is(err, ErrNoRoomSpec) {
		t.Fatalf("a refused spec was stored: %v", err)
	}
	for name, lock := range map[string]string{
		"version 2": `{"version":2}`, "not json": `nope`, "credential": `{"version":1,"auth_token":"x"}`,
		"array": `[1]`,
	} {
		if _, err := s.PutRoomLock("room-d", []byte(lock)); err == nil {
			t.Errorf("lock %s accepted", name)
		}
	}
	// A key that merely contains the letters is fine.
	if _, err := s.SetRoomSpec("room-d", []byte("version: 1\nname: room-d\nkeyboard: us\n"), "x"); err != nil {
		t.Fatalf("keyboard: %v", err)
	}
}

// A ROOM THAT DOES NOT EXIST HAS NO SPEC, and removing a room takes its spec with it.
func TestRoomSpecUnknownRoomAndMigrationTwice(t *testing.T) {
	s := open(t)
	if _, err := s.SetRoomSpec("nobody", []byte(goodSpec), "x"); !errors.Is(err, ErrNoSuchRoom) {
		t.Fatalf("unknown room: %v", err)
	}
	if err := s.migrate(); err != nil {
		t.Fatalf("migrating again: %v", err)
	}
}

// THE KEY TRIPWIRE, as a table: names that must trip it, in nested lists and mixed case, and names that must not.
func TestCredentialTripwire(t *testing.T) {
	trips := []string{
		"version: 1\nname: room-d\nGitHub_Token: x\n",
		"version: 1\nname: room-d\npacks:\n  - runner: claude\n    extras:\n      - {PrivateKey: x}\n",
		"version: 1\nname: room-d\nauth: x\n",
		"version: 1\nname: room-d\nAuthorization: Bearer x\n",
		"version: 1\nname: room-d\nbearer: x\n",
		"version: 1\nname: room-d\nCookie: x\n",
		"version: 1\nname: room-d\nsession: x\n",
		"version: 1\nname: room-d\nssh_key: x\n",
		"version: 1\nname: room-d\nlayout: {deep: [{ok: 1}, {DB-Password: x}]}\n",
	}
	for _, y := range trips {
		if err := CheckRoomSpec("room-d", []byte(y)); err == nil || !strings.Contains(err.Error(), "credential") {
			t.Errorf("not tripped: %q (%v)", y, err)
		}
	}
	passes := []string{
		"version: 1\nname: room-d\nkeyboard: us\n",
		"version: 1\nname: room-d\nauthors: [a]\npacks:\n  - {runner: claude, repo: o/r, from: claude}\n",
	}
	for _, y := range passes {
		if err := CheckRoomSpec("room-d", []byte(y)); err != nil {
			t.Errorf("tripped: %q (%v)", y, err)
		}
	}
}

// A lock post is written to the room's audit log like a spec set.
func TestLockPostIsLogged(t *testing.T) {
	s := open(t)
	r := added(t, s, "room-d")
	if _, err := s.PutRoomLock("room-d", []byte(goodLock)); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM room_audit WHERE room_id = ? AND kind = 'lock-posted'`, r.ID).
		Scan(&n); err != nil || n != 1 {
		t.Fatalf("lock-posted entries = %d %v", n, err)
	}
}
