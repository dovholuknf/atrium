package link

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/base/seenhost"
)

func TestPageOf(t *testing.T) {
	room, card, rest, ok := pageOf("/room/r1/c1/files/mock/a.html")
	if !ok || room != "r1" || card != "c1" || rest != "mock/a.html" {
		t.Fatalf("got %q %q %q %v", room, card, rest, ok)
	}
	for _, p := range []string{"/room/r1/c1/other/x", "/r1/preview/80/", "/room/r1", "/v1/tasks"} {
		if _, _, _, ok := pageOf(p); ok {
			t.Fatalf("%q read as a page", p)
		}
	}
}

func TestTheHubHandsItsSeenHostDownToTheRoom(t *testing.T) {
	seenhost.Reset()
	t.Cleanup(seenhost.Reset)
	seenhost.NoteBase("http://atrium.ziti")
	if got := seenhost.Base("x"); got != "http://atrium.ziti" {
		t.Fatalf("Base = %q", got)
	}
	seenhost.NoteBase("javascript:x")
	if got := seenhost.Base("x"); got != "http://atrium.ziti" {
		t.Fatalf("a non-http base was taken: %q", got)
	}
}
