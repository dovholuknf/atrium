package forge

import (
	"context"
	"net/http"
	"testing"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

func TestTheRoomHostComesFromTheAttachedRecord(t *testing.T) {
	p := New(Calls{Rooms: func() []wire.Attached { return []wire.Attached{{Name: "Alpha"}} }})
	if h, ok := p.attachedHost("alpha"); !ok || h != wire.HostFor("Alpha") {
		t.Errorf("attached host = %q, %v", h, ok)
	}
	for _, bad := range []string{"", "alpha@evil.test", "alpha/../x", "alpha:80"} {
		if h, ok := p.attachedHost(bad); ok || h != "" {
			t.Errorf("%q was given a host %q", bad, h)
		}
	}
}

func TestTheReviewCopyAndTheRoomCallRefuseAnUnknownRoom(t *testing.T) {
	asked := false
	p := New(Calls{
		Has:        func(room string) bool { return room == "alpha" },
		Rooms:      func() []wire.Attached { return []wire.Attached{{Name: "alpha"}} },
		RoomClient: func(string) *http.Client { asked = true; return http.DefaultClient },
	})
	if _, _, err := p.copyReview(context.Background(), "k", "alpha", "ghost"); err == nil {
		t.Error("a copy to an unknown room was not refused")
	}
	if err := p.roomJSON(context.Background(), "ghost/../x", http.MethodGet, "/v1/prs", nil, nil); err == nil {
		t.Error("a call to an unknown room was not refused")
	}
	if asked {
		t.Error("a client was built for an unknown room")
	}
}
