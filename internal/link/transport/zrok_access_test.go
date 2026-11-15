package transport

import (
	"regexp"
	"strings"
	"testing"
)

func zrokEntry(id, serves, mode string) Entry {
	return Entry{ID: id, Name: id, Kind: "zrok", Serves: serves, Settings: map[string]string{ZrokSettingMode: mode}}
}

func TestZrokKindRefusesWhatOneShareCannotCarry(t *testing.T) {
	k, ok := AccessKindFor("zrok")
	if !ok {
		t.Fatal("zrok is not registered")
	}
	for _, c := range []struct {
		e    Entry
		want string
	}{
		{zrokEntry("a", ServesBoth, "private"), "not both"},
		{zrokEntry("b", "", "private"), "not both"},
		{zrokEntry("c", ServesLink, "public"), "never on a public share"},
		{zrokEntry("d", ServesBoard, "open"), "private or public"},
	} {
		if _, err := k.Hub(c.e); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: %v, want %q", c.e.ID, err, c.want)
		}
	}
	for _, e := range []Entry{zrokEntry("p", ServesBoard, "public"), zrokEntry("q", ServesBoard, ""), zrokEntry("r", ServesLink, "private")} {
		if h, err := k.Hub(e); err != nil || h == nil {
			t.Fatalf("%s: %v", e.ID, err)
		}
	}
	if r, err := k.Room(zrokEntry("b", ServesBoard, "private")); err != nil || r != nil {
		t.Fatalf("a board entry has no room half: %v %v", r, err)
	}
	r, err := k.Room(zrokEntry("l", ServesLink, "private"))
	if err != nil || r == nil || r.Mutual() {
		t.Fatalf("link room half: %v %v", r, err)
	}
	if r.(*Zrok).Token != ZrokEntryToken("l") {
		t.Fatal("the room dials a token the hub does not share")
	}
}

func TestZrokEntryTokenAndNameAreStableAndValid(t *testing.T) {
	token := regexp.MustCompile(`^[a-z0-9-]{3,32}$`)
	name := regexp.MustCompile(`^atrium-[a-z0-9-]{3,56}$`)
	a, b := ZrokEntryToken("one"), ZrokEntryToken("two")
	if a != ZrokEntryToken("one") || a == b || !token.MatchString(a) || !token.MatchString(b) {
		t.Fatalf("tokens %q %q", a, b)
	}
	e := zrokEntry("pub", ServesBoard, "public")
	if n := ZrokEntryName(e); !name.MatchString(n) || n != ZrokEntryName(e) {
		t.Fatalf("derived name %q", n)
	}
	e.Settings[ZrokSettingName] = "Board"
	if n := ZrokEntryName(e); n != "atrium-board" {
		t.Fatalf("typed name %q", n)
	}
	e.Settings[ZrokSettingName] = "atrium-x1"
	if n := ZrokEntryName(e); n != "atrium-x1" {
		t.Fatalf("prefixed name %q", n)
	}
}

func TestZrokPublicEntryHasNoGrants(t *testing.T) {
	k, _ := AccessKindFor("zrok")
	h, _ := k.Hub(zrokEntry("p", ServesBoard, "public"))
	if err := h.(*zrokHub).Grant([]string{"x@example.com"}); err == nil || !strings.Contains(err.Error(), "sign-in") {
		t.Fatalf("grant on public: %v", err)
	}
	if s := h.State(); s != "off" {
		t.Fatalf("state before listen %q", s)
	}
}
