package forge

import (
	"context"
	"net/http"
	"testing"
)

func TestTheReviewCopyAndTheRoomCallRefuseAnUnknownRoom(t *testing.T) {
	asked := false
	p := New(Calls{
		Has:        func(room string) bool { return room == "alpha" },
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
