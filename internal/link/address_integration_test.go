//go:build integration

package link

import "testing"

// THE SAME TABLE AS internal/daemon/address_test.go. The two parsers are
// copies because neither package imports the other, and this table is what
// keeps them the same. Change one, change both.
var addressCases = []struct {
	in, name, room string
	bad            bool
}{
	{in: "sa1", name: "sa1"},
	{in: "@sa1", name: "sa1"},
	{in: "  sa1  ", name: "sa1"},
	{in: "sa1@m1mini", name: "sa1", room: "m1mini"},
	{in: "@sa1@m1mini", name: "sa1", room: "m1mini"},
	{in: "atrium-87300@claude-sg4", name: "atrium-87300", room: "claude-sg4"},
	{in: "a@b@m1mini", name: "a@b", room: "m1mini"},
	{in: "m1mini~01KABC", name: "01KABC", room: "m1mini"},
	{in: "01KABC@m1mini", name: "01KABC", room: "m1mini"},
	{in: "", bad: true},
	{in: "@", bad: true},
	{in: "sa1@", bad: true},
	{in: "@@m1mini", bad: true},
	{in: "m1mini~", bad: true},
}

func TestAddressGrammar(t *testing.T) {
	for _, c := range addressCases {
		name, room, err := SplitAddress(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("%q parsed as (%q, %q), want a refusal", c.in, name, room)
			}
			continue
		}
		if err != nil || name != c.name || room != c.room {
			t.Errorf("%q = (%q, %q, %v), want (%q, %q)", c.in, name, room, err, c.name, c.room)
		}
	}
}

func TestOwnRoomIsNoRoom(t *testing.T) {
	if otherRoom("SG4", "sg4") != "" || otherRoom("", "sg4") != "" || otherRoom("m1mini", "sg4") != "m1mini" {
		t.Fatal("a room part naming the caller's own room, in any case, is the same as none")
	}
}
