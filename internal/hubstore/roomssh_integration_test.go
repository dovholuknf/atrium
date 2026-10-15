//go:build integration

package hubstore

import (
	"encoding/base64"
	"testing"
)

var (
	keyA = "ssh-ed25519 " + base64.StdEncoding.EncodeToString([]byte("key body of room a"))
	keyB = "ssh-ed25519 " + base64.StdEncoding.EncodeToString([]byte("key body of room b"))
)

// A ROOM PUBLISHES, THE SET HOLDS IT, and a second publish replaces the first.
func TestRoomSSHIsPublishedAndReplaced(t *testing.T) {
	s := open(t)
	added(t, s, "sg3")
	if err := s.PutRoomSSH("sg3", "claude", keyA+" comment", 22, true); err != nil {
		t.Fatal(err)
	}
	set, err := s.RoomSSHSet()
	if err != nil || len(set) != 1 || set[0].Room != "sg3" || set[0].User != "claude" || set[0].Key != keyA || !set[0].On {
		t.Fatalf("%+v %v", set, err)
	}
	if err := s.PutRoomSSH("sg3", "localai", keyB, 2222, false); err != nil {
		t.Fatal(err)
	}
	set, _ = s.RoomSSHSet()
	if len(set) != 1 || set[0].User != "localai" || set[0].Key != keyB || set[0].Port != 2222 || set[0].On {
		t.Fatalf("not replaced: %+v", set)
	}
}

// A ROOM THE HUB DOES NOT KNOW CANNOT PUBLISH, and a removed room's key leaves the set.
func TestRoomSSHOnlyForKnownRoomsAndGoesWithTheRoom(t *testing.T) {
	s := open(t)
	if err := s.PutRoomSSH("ghost", "claude", keyA, 22, true); err == nil {
		t.Fatal("an unknown room published a key")
	}
	r := added(t, s, "sg3")
	added(t, s, "sg4")
	s.PutRoomSSH("sg3", "claude", keyA, 22, true)
	s.PutRoomSSH("sg4", "claude", keyB, 22, true)
	if err := s.Remove(r.ID, "gone"); err != nil {
		t.Fatal(err)
	}
	set, _ := s.RoomSSHSet()
	if len(set) != 1 || set[0].Room != "sg4" {
		t.Fatalf("the removed room's key is still in the set: %+v", set)
	}
}

// WHAT IS STORED IS WRITTEN INTO OTHER MACHINES' authorized_keys, so it is checked hard.
func TestRoomSSHRefusesWhatCouldBeAnInjection(t *testing.T) {
	s := open(t)
	added(t, s, "sg3")
	for name, c := range map[string]struct{ user, key string }{
		"options":  {"claude", `command="x" ` + keyA},
		"newline":  {"claude", keyA + "\n" + keyB},
		"rsa":      {"claude", "ssh-rsa AAAAB3NzaC1yc2E"},
		"not b64":  {"claude", "ssh-ed25519 !!!"},
		"bad user": {"cl aude", keyA},
		"empty":    {"", keyA},
		"dashes":   {"claude\nHost *", keyA},
	} {
		if err := s.PutRoomSSH("sg3", c.user, c.key, 22, true); err == nil {
			t.Fatalf("%s: accepted", name)
		} else if !IsRefusal(err) {
			t.Fatalf("%s: not a refusal: %v", name, err)
		}
	}
	if set, _ := s.RoomSSHSet(); len(set) != 0 {
		t.Fatalf("something was stored: %+v", set)
	}
}
