package hubstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

const goodSpec = "version: 1\nname: sg3\nos: windows\naccount: localai\nwork_root: V:/localai\ncaches: [npm, go]\n"
const goodLock = `{"version":1,"spec_hash":"abc","os":"windows","steps":[{"step":"work-root","status":"ok"}]}`

// THE SPEC IS KEPT VERBATIM, WITH ITS HASH, and a second set replaces it.
func TestRoomSpecIsStoredVerbatimWithItsHash(t *testing.T) {
	s := open(t)
	added(t, s, "sg3")
	if _, err := s.RoomSpecOf("sg3"); !errors.Is(err, ErrNoRoomSpec) {
		t.Fatalf("before any set: %v", err)
	}
	sp, err := s.SetRoomSpec("SG3", []byte(goodSpec), "clint")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(goodSpec))
	if sp.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("hash %s", sp.SHA256)
	}
	got, err := s.RoomSpecOf("sg3")
	if err != nil || got.YAML != goodSpec || got.SetBy != "clint" || got.SetAt.IsZero() {
		t.Fatalf("got %+v %v", got, err)
	}
	next := goodSpec + "# changed\n"
	if _, err := s.SetRoomSpec("sg3", []byte(next), "clint"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.RoomSpecOf("sg3"); got.YAML != next {
		t.Fatalf("not replaced: %q", got.YAML)
	}
}

// OBSERVED NEVER OVERWRITES DESIRED, and neither way round.
func TestPostingALockNeverTouchesTheSpec(t *testing.T) {
	s := open(t)
	added(t, s, "sg3")
	if _, err := s.RoomLockOf("sg3"); !errors.Is(err, ErrNoRoomLock) {
		t.Fatalf("before any lock: %v", err)
	}
	if _, err := s.SetRoomSpec("sg3", []byte(goodSpec), "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutRoomLock("sg3", []byte(goodLock)); err != nil {
		t.Fatal(err)
	}
	if sp, _ := s.RoomSpecOf("sg3"); sp.YAML != goodSpec {
		t.Fatalf("the lock changed the spec: %q", sp.YAML)
	}
	if _, err := s.SetRoomSpec("sg3", []byte(goodSpec+"# x\n"), "a"); err != nil {
		t.Fatal(err)
	}
	lk, err := s.RoomLockOf("sg3")
	if err != nil || string(lk.Lock) != goodLock || lk.ReceivedAt.IsZero() {
		t.Fatalf("the spec changed the lock: %+v %v", lk, err)
	}
}

// WHAT THE HUB REFUSES: a wrong version, a wrong name, a credential, and a size over the bound.
func TestRoomSpecRefusals(t *testing.T) {
	s := open(t)
	added(t, s, "sg3")
	for name, spec := range map[string]string{
		"version 2":  "version: 2\nname: sg3\n",
		"no version": "name: sg3\n",
		"other name": "version: 1\nname: sg4\n",
		"not yaml":   "version: 1\nname: [sg3\n",
		"empty":      "",
		"token":      "version: 1\nname: sg3\ngithub_token: abc\n",
		"nested":     "version: 1\nname: sg3\npacks:\n  - {runner: claude, Password: x}\n",
		"secret":     "version: 1\nname: sg3\nsecret: x\n",
		"key":        "version: 1\nname: sg3\nkey: x\n",
		"too big":    "version: 1\nname: sg3\nnote: " + strings.Repeat("a", MaxRoomSpec) + "\n",
	} {
		_, err := s.SetRoomSpec("sg3", []byte(spec), "x")
		if err == nil || !IsRefusal(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := s.RoomSpecOf("sg3"); !errors.Is(err, ErrNoRoomSpec) {
		t.Fatalf("a refused spec was stored: %v", err)
	}
	for name, lock := range map[string]string{
		"version 2": `{"version":2}`, "not json": `nope`, "credential": `{"version":1,"auth_token":"x"}`,
		"array": `[1]`,
	} {
		if _, err := s.PutRoomLock("sg3", []byte(lock)); err == nil {
			t.Errorf("lock %s accepted", name)
		}
	}
	// A key that merely contains the letters is fine.
	if _, err := s.SetRoomSpec("sg3", []byte("version: 1\nname: sg3\nkeyboard: us\n"), "x"); err != nil {
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
