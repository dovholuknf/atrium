//go:build integration

package address

import "testing"

var addressCases = []struct {
	in, name, room string
	bad            bool
}{
	{in: "sa1", name: "sa1"},
	{in: "@sa1", name: "sa1"},
	{in: "  sa1  ", name: "sa1"},
	{in: "sa1@room-a", name: "sa1", room: "room-a"},
	{in: "@sa1@room-a", name: "sa1", room: "room-a"},
	{in: "atrium-87300@claude-room-e", name: "atrium-87300", room: "claude-room-e"},
	{in: "a@b@room-a", name: "a@b", room: "room-a"},
	{in: "room-a~01KABC", name: "01KABC", room: "room-a"},
	{in: "01KABC@room-a", name: "01KABC", room: "room-a"},
	{in: "", bad: true},
	{in: "@", bad: true},
	{in: "sa1@", bad: true},
	{in: "@@room-a", bad: true},
	{in: "room-a~", bad: true},
}

func TestAddressGrammar(t *testing.T) {
	t.Parallel()
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
