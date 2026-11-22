//go:build integration

package link

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

// f-010. atrium_cull takes a card on another room and reaches THAT room, with the
// proof body exactly as the caller gave it.
func TestACullReachesTheOwningRoomWithItsProofUntouched(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	for _, card := range []string{"atrium-87300@room-e", "orch@room-e", "room-e~s1"} {
		_, out, err := x.control.cullHandler(relayCtx(t), ctlReq("sa1", "room-a"),
			cullInput{Card: card, Into: "claude/ui", Tip: "0123abc"})
		if err != nil {
			t.Fatalf("cull %q: %v", card, err)
		}
		if !strings.EqualFold(out.Card, "room-e~s1") || !out.Exited {
			t.Fatalf("cull %q = %+v, want room-e~s1 named across and the room's answer", card, out)
		}
	}
	got := x.roomE.posted("/v1/tasks/s1/cull")
	if len(got) != 3 {
		t.Fatalf("room-e got %d culls, want 3", len(got))
	}
	for _, body := range got {
		if body != `{"into":"claude/ui","tip":"0123abc"}` {
			t.Fatalf("room-e got body %s, want into and tip flat and unchanged", body)
		}
	}
	if len(x.mini.posted("/v1/tasks/s1/cull"))+len(x.mini.posted("/v1/tasks/m1/cull")) != 0 {
		t.Fatal("a cull for room-e reached room-a")
	}
}

// A cull with no proof sends none, so a room reads it as a proof made here.
func TestACullWithoutAProofCarriesNone(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	if _, _, err := x.control.cullHandler(relayCtx(t), ctlReq("sa1", "room-a"), cullInput{Card: "room-e~s1"}); err != nil {
		t.Fatal(err)
	}
	if got := x.roomE.posted("/v1/tasks/s1/cull"); len(got) != 1 || got[0] != `{"into":""}` {
		t.Fatalf("room-e got %v", got)
	}
}

// The hold r-019 adds to cullHandler rides the same scope, so it is routed by
// the resolver alone. Here the route itself, through the hub, by the room header.
func TestTheHoldMarkAndPreflightGoToTheOneRoomNamed(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	do := func(method, path, room, body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequestWithContext(relayCtx(t), method, x.board+path, strings.NewReader(body))
		req.Header.Set(wire.RoomHeader, room)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := res.Body.Read(buf)
			sb.Write(buf[:n])
			if err != nil {
				break
			}
		}
		return res.StatusCode, sb.String()
	}
	if code, _ := do(http.MethodPost, "/v1/tasks/s1/cull/hold", "room-e", `{"by":"orch","hold":true}`); code != 200 {
		t.Fatalf("hold = %d", code)
	}
	if code, _ := do(http.MethodPost, "/v1/merged", "room-e", `{"into":"claude/ui"}`); code != 200 {
		t.Fatalf("mark = %d", code)
	}
	if code, _ := do(http.MethodGet, "/v1/preflight", "room-e", ""); code != 200 {
		t.Fatalf("preflight = %d", code)
	}
	if got := x.roomE.posted("/v1/tasks/s1/cull/hold"); len(got) != 1 || got[0] != `{"by":"orch","hold":true}` {
		t.Fatalf("room-e hold = %v", got)
	}
	if got := x.roomE.posted("/v1/merged"); len(got) != 1 || got[0] != `{"into":"claude/ui"}` {
		t.Fatalf("room-e mark = %v", got)
	}
	if x.roomE.preflights != 1 || x.mini.preflights != 0 || len(x.mini.posted("/v1/merged")) != 0 {
		t.Fatalf("preflight and mark reached the wrong room: room-e %d, mini %d", x.roomE.preflights, x.mini.preflights)
	}
	// Two rooms attached and none named: never merged across rooms, a question.
	for _, path := range []string{"/v1/preflight", "/v1/merged"} {
		req, _ := http.NewRequestWithContext(relayCtx(t), http.MethodGet, x.board+path, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusConflict {
			t.Fatalf("%s with no room named = %d, want the room question", path, res.StatusCode)
		}
	}
	if x.roomE.preflights != 1 || x.mini.preflights != 0 {
		t.Fatal("an unnamed preflight reached a room")
	}
}

// A room too old for the cull answers a bare 404, and the hub says so with the
// room's build, on the cull, the hold and the mark alike.
func TestARoomThatPredatesCullSaysSo(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.roomE.noCull = true

	_, _, err := x.control.cullHandler(relayCtx(t), ctlReq("sa1", "room-a"), cullInput{Card: "room-e~s1"})
	want := "room-e build b-room-e predates cull (needs " + cullSince + "). Update the room."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	for _, c := range []struct{ path, body string }{
		{"/v1/merged", `{"into":"claude/ui"}`},
		{"/v1/tasks/s1/cull/hold", `{"hold":true}`},
	} {
		req, _ := http.NewRequestWithContext(relayCtx(t), http.MethodPost, x.board+c.path, strings.NewReader(c.body))
		req.Header.Set(wire.RoomHeader, "room-e")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 4096)
		n, _ := res.Body.Read(buf)
		res.Body.Close()
		if !strings.Contains(string(buf[:n]), want) {
			t.Fatalf("%s answered %d %q, want the update sentence", c.path, res.StatusCode, buf[:n])
		}
	}
}

// A worker on another room cannot cull itself: the card it names is on its own
// room the moment the address names its own room, and the room it is on is the
// one that counts.
func TestAWorkerOnAnotherRoomCannotCullItself(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	// sa1 is on room-a and culls itself, named with its own room.
	for _, card := range []string{"sa1", "sa1@room-a", "room-a~m1"} {
		_, _, err := x.control.cullHandler(relayCtx(t), ctlReq("sa1", "room-a"), cullInput{Card: card})
		if err == nil || !strings.Contains(err.Error(), "cannot cull itself") {
			t.Fatalf("cull %q: err = %v, want a self-cull refused", card, err)
		}
	}
	if len(x.mini.posted("/v1/tasks/m1/cull")) != 0 {
		t.Fatal("a self-cull reached the room")
	}
	// A different worker on another room, asked from room-a, is not itself.
	if _, _, err := x.control.cullHandler(relayCtx(t), ctlReq("sa1", "room-a"), cullInput{Card: "room-e~s1"}); err != nil {
		t.Fatalf("cull of another room's card: %v", err)
	}
}
