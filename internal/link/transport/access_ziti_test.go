package transport

import (
	"strings"
	"testing"
)

func TestZitiKindIsRegisteredAndEntriesAreIndependent(t *testing.T) {
	t.Parallel()
	k, ok := AccessKindFor("ziti")
	if !ok {
		t.Fatal("ziti is not a registered access kind")
	}
	a := Entry{ID: "a", Name: "one", Kind: "ziti", Settings: map[string]string{
		"controller": "https://c1:1280", "identity": "/k/a.json", "service": "atrium-board"}}
	b := Entry{ID: "b", Name: "two", Kind: "ziti", Settings: map[string]string{
		"controller": "https://c2:1280", "identity": "/k/b.json", "service": "atrium-board"}}
	if err := ValidateEntries([]Entry{a, b}); err != nil {
		t.Fatal(err)
	}
	ha, _ := k.Hub(a)
	hb, _ := k.Hub(b)
	ja, _ := ha.Join("r")
	jb, _ := hb.Join("r")
	if ja == jb || !strings.HasPrefix(ja, "ziti://c1:1280/") {
		t.Fatalf("join strings %q %q", ja, jb)
	}
	if !strings.HasSuffix(ja, "/atrium-board?room=r") {
		t.Fatalf("the join string does not name the room: %q", ja)
	}
	if ha.State() != "not bound" {
		t.Fatalf("state %q", ha.State())
	}
	if _, err := k.Room(Entry{Kind: "ziti"}); err == nil {
		t.Fatal("an entry with no identity built a room half")
	}
}
