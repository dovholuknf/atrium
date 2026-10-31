//go:build integration

package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dovholuknf/atrium/internal/daemon/hublink"
	"net/http"
	"strings"
	"testing"
	"time"
)

func roomsOf(t *testing.T, d *Daemon) []hublink.Room {
	t.Helper()
	got, ok := d.Rooms().(map[string]any)
	if !ok {
		t.Fatalf("Rooms answered %T", d.Rooms())
	}
	list, ok := got["rooms"].([]hublink.Room)
	if !ok {
		t.Fatalf("rooms is %T", got["rooms"])
	}
	return list
}

// A room's cards are REPLACED on every check-in, never merged. A merge would
// mean a card deleted on the room lives forever on the hub, and the point of
// holding nothing durable is that the room is the truth about itself.
func TestACheckInReplacesTheCards(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	if _, err := d.RoomCheckIn(hublink.RoomReport{Name: "one", Cards: []hublink.RoomCard{
		{ID: "a", Title: "first"}, {ID: "b", Title: "second"},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.RoomCheckIn(hublink.RoomReport{Name: "one", Cards: []hublink.RoomCard{
		{ID: "b", Title: "second"},
	}}); err != nil {
		t.Fatal(err)
	}
	list := roomsOf(t, d)
	if len(list) != 1 {
		t.Fatalf("%d rooms", len(list))
	}
	if len(list[0].Cards) != 1 || list[0].Cards[0].ID != "b" {
		t.Fatalf("a card deleted on the room survived on the hub: %+v", list[0].Cards)
	}
}

// A room that stops talking is SAID to be stale rather than dropped. A machine
// that died and one that was never there look the same otherwise, and the first
// is the one worth noticing.
func TestAQuietRoomGoesStaleBeforeItGoes(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	if _, err := d.RoomCheckIn(hublink.RoomReport{Name: "quiet"}); err != nil {
		t.Fatal(err)
	}
	if roomsOf(t, d)[0].Stale {
		t.Fatal("a room that just checked in was reported stale")
	}

	// Reach in and age it, rather than sleeping for a minute.
	d.RoomTable.Mu.Lock()
	d.RoomTable.All["quiet"].LastSeen = time.Now().Add(-hublink.RoomStale - time.Second)
	d.RoomTable.Mu.Unlock()

	list := roomsOf(t, d)
	if len(list) != 1 {
		t.Fatalf("a stale room was dropped rather than reported: %d rooms", len(list))
	}
	if !list[0].Stale {
		t.Fatal("a room that has missed its heartbeats is not reported stale")
	}
}

// And a room nobody has heard from in long enough disappears, because listing
// it is a claim rather than a memory.
func TestAForgottenRoomIsGone(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	if _, err := d.RoomCheckIn(hublink.RoomReport{Name: "gone"}); err != nil {
		t.Fatal(err)
	}
	d.RoomTable.Mu.Lock()
	d.RoomTable.All["gone"].LastSeen = time.Now().Add(-hublink.RoomForget - time.Second)
	d.RoomTable.Mu.Unlock()

	if list := roomsOf(t, d); len(list) != 0 {
		t.Fatalf("a room nobody has heard from in %s is still listed: %+v", hublink.RoomForget, list)
	}
}

// The waiting count is what makes a remote room actionable rather than
// informational, and it is computed on read because it is a fact about now.
func TestWaitingIsCountedFromTheCards(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	if _, err := d.RoomCheckIn(hublink.RoomReport{Name: "busy", Cards: []hublink.RoomCard{
		{ID: "a", Status: "running"},
		{ID: "b", Status: "needs-input"},
		{ID: "c", Status: "needs-permission"},
		{ID: "d", Status: "done"},
	}}); err != nil {
		t.Fatal(err)
	}
	if got := roomsOf(t, d)[0].Waiting; got != 2 {
		t.Fatalf("waiting is %d, wanted 2", got)
	}
}

// A room has to say what it is called, because the name is the key. Two rooms
// with one name are one room that flaps, and an unnamed one would be every
// room at once.
func TestARoomHasToBeNamed(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	if _, err := d.RoomCheckIn(hublink.RoomReport{Name: "  "}); err == nil {
		t.Fatal("a room with no name was accepted")
	}
	if _, err := d.RoomCheckIn(hublink.RoomReport{Name: string(make([]byte, 200))}); err == nil {
		t.Fatal("a room name too long to draw was accepted")
	}
}

// Checking in twice is one room, not two. The second is an update.
func TestCheckingInTwiceIsOneRoom(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	for i := 0; i < 5; i++ {
		if _, err := d.RoomCheckIn(hublink.RoomReport{Name: "same", Host: "h"}); err != nil {
			t.Fatal(err)
		}
	}
	if list := roomsOf(t, d); len(list) != 1 {
		t.Fatalf("five check-ins produced %d rooms", len(list))
	}
}

func requestsOf(t *testing.T, d *Daemon, room string) []hublink.RoomRequest {
	t.Helper()
	for _, r := range roomsOf(t, d) {
		if r.Name == room {
			return r.Requests
		}
	}
	t.Fatalf("no room called %q is listed", room)
	return nil
}

func reportAsking(t *testing.T, d *Daemon, room, perm string) []hublink.RoomDecision {
	t.Helper()
	out, err := d.RoomCheckIn(hublink.RoomReport{Name: room, Perms: []hublink.RoomPerm{
		{ID: perm, Tool: "Bash", Command: "rm -rf build", Agent: "a card", Waiting: 30},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A decision leaves ON THE ANSWER TO A CHECK-IN and there is no other way out.
// A room dials out, so nothing here can dial back into it, and a hub that
// believed it could would leave the agent frozen while reporting success.
func TestADecisionLeavesOnTheNextCheckIn(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	reportAsking(t, d, "leaf", "p1")

	if err := d.RoomDecide("leaf", "p1", hublink.RoomDecision{Decision: "approve"}); err != nil {
		t.Fatal(err)
	}
	got := reportAsking(t, d, "leaf", "p1")
	if len(got) != 1 {
		t.Fatalf("the check-in carried %d decision(s), wanted 1", len(got))
	}
	if got[0].Perm != "p1" || got[0].Decision != "approve" {
		t.Fatalf("the wrong decision came back: %+v", got[0])
	}
}

// And it is handed over ONCE. Collected twice means the room applies it twice,
// and for an approval that means running the command a second time.
func TestADecisionIsHandedOverOnce(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	reportAsking(t, d, "leaf", "p1")
	if err := d.RoomDecide("leaf", "p1", hublink.RoomDecision{Decision: "approve"}); err != nil {
		t.Fatal(err)
	}
	reportAsking(t, d, "leaf", "p1")

	// The room took it and its next report no longer mentions the request,
	// which is the only signal that anything happened.
	again, err := d.RoomCheckIn(hublink.RoomReport{Name: "leaf"})
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("a decision already taken was handed over again: %+v", again)
	}
}

// A request that has been answered is NOT DRAWN AGAIN while the answer is in
// flight. The room reports the same pending request for the second or two
// before it collects the decision, and offering the buttons again in that
// window is how one approval becomes two.
func TestAnAnsweredRequestIsNotOfferedAgain(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	reportAsking(t, d, "leaf", "p1")
	if got := requestsOf(t, d, "leaf"); len(got) != 1 {
		t.Fatalf("a pending request was not drawn: %+v", got)
	}
	if err := d.RoomDecide("leaf", "p1", hublink.RoomDecision{Decision: "approve"}); err != nil {
		t.Fatal(err)
	}
	if got := requestsOf(t, d, "leaf"); len(got) != 0 {
		t.Fatalf("a request with an answer waiting for it was offered again: %+v", got)
	}
	// Still not, after the room has reported it as pending once more.
	reportAsking(t, d, "leaf", "p1")
	if got := requestsOf(t, d, "leaf"); len(got) != 0 {
		t.Fatalf("the request came back while its answer was in flight: %+v", got)
	}
}

// AND IT COMES BACK IF THE ANSWER DID NOT TAKE. Nothing acknowledges: the room
// may refuse the decision because its own board answered first, or because its
// store halted. A request hidden forever behind a decision that failed is an
// agent frozen on another machine with nothing on any board to say so, which is
// the exact failure this feature exists to remove.
func TestAnAnswerThatDidNotTakeExpires(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	reportAsking(t, d, "leaf", "p1")
	if err := d.RoomDecide("leaf", "p1", hublink.RoomDecision{Decision: "approve"}); err != nil {
		t.Fatal(err)
	}
	reportAsking(t, d, "leaf", "p1")
	if got := requestsOf(t, d, "leaf"); len(got) != 0 {
		t.Fatalf("hidden while in flight is the point: %+v", got)
	}

	// Reach in and age the answer, rather than waiting two minutes.
	d.RoomTable.Mu.Lock()
	d.RoomTable.All["leaf"].Answers["p1"].At = time.Now().Add(-hublink.RoomDecisionTTL - time.Second)
	d.RoomTable.Mu.Unlock()

	reportAsking(t, d, "leaf", "p1")
	got := requestsOf(t, d, "leaf")
	if len(got) != 1 {
		t.Fatalf("a request still being asked about %s after being answered is not being "+
			"asked again: %+v", hublink.RoomDecisionTTL, got)
	}
	if err := d.RoomDecide("leaf", "p1", hublink.RoomDecision{Decision: "block"}); err != nil {
		t.Fatalf("and it could not be answered a second time: %v", err)
	}
}

// An answer for a request the room has stopped asking about is DROPPED rather
// than delivered. The room answered it on its own board, so delivering would
// answer whatever that id means next.
func TestAnAnswerForARequestThatWentAwayIsDropped(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	reportAsking(t, d, "leaf", "p1")
	if err := d.RoomDecide("leaf", "p1", hublink.RoomDecision{Decision: "approve"}); err != nil {
		t.Fatal(err)
	}
	// The room's next report does not mention it, which means it is answered
	// there.
	out, err := d.RoomCheckIn(hublink.RoomReport{Name: "leaf"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("an answer for a request that is gone was handed over: %+v", out)
	}
	d.RoomTable.Mu.Lock()
	held := len(d.RoomTable.All["leaf"].Answers)
	d.RoomTable.Mu.Unlock()
	if held != 0 {
		t.Fatalf("%d answer(s) are still being held for a request that is gone", held)
	}
}

// Answering something this hub can no longer address is REFUSED, and refused
// as the same event the local queue reports with a conflict: a button drawn
// from a poll one moment out of date.
func TestAStaleDecisionIsRefused(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	reportAsking(t, d, "leaf", "p1")

	for _, c := range []struct {
		why, room, perm string
	}{
		{"a room that is not reporting in", "nowhere", "p1"},
		{"a request that room is not asking about", "leaf", "p2"},
	} {
		err := d.RoomDecide(c.room, c.perm, hublink.RoomDecision{Decision: "approve"})
		if err == nil {
			t.Fatalf("%s was accepted", c.why)
		}
		if !errors.Is(err, hublink.ErrRoomStaleDecision) {
			t.Fatalf("%s was refused as something other than stale: %v", c.why, err)
		}
	}

	// And answering one twice while the first is still in flight.
	if err := d.RoomDecide("leaf", "p1", hublink.RoomDecision{Decision: "approve"}); err != nil {
		t.Fatal(err)
	}
	err := d.RoomDecide("leaf", "p1", hublink.RoomDecision{Decision: "block"})
	if !errors.Is(err, hublink.ErrRoomStaleDecision) {
		t.Fatalf("answering a request that already has an answer in flight: %v", err)
	}
}

// A decision is approve or block, and a STANDING answer needs the scope it
// covers. Both are refused here rather than sent on: a rule with no scope
// matches everything, and finding that out on the other machine means finding
// out after it was written into that machine's rule table.
func TestARoomDecisionIsChecked(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	reportAsking(t, d, "leaf", "p1")

	if err := d.RoomDecide("leaf", "p1", hublink.RoomDecision{Decision: "maybe"}); err == nil {
		t.Fatal("a decision that is neither approve nor block was accepted")
	}
	if err := d.RoomDecide("leaf", "p1", hublink.RoomDecision{
		Decision: "approve", Forever: true, Prefix: "  ",
	}); err == nil {
		t.Fatal("a standing answer with no scope was accepted, so a rule matching " +
			"everything would have been written on another machine")
	}
	// The scope makes it acceptable, and it carries.
	if err := d.RoomDecide("leaf", "p1", hublink.RoomDecision{
		Decision: "approve", Forever: true, Prefix: "go build",
	}); err != nil {
		t.Fatal(err)
	}
	got := reportAsking(t, d, "leaf", "p1")
	if len(got) != 1 || !got[0].Forever || got[0].Prefix != "go build" {
		t.Fatalf("the standing answer did not travel intact: %+v", got)
	}
}

// THE WAIT IS SENT AS SECONDS AND TURNED INTO A TIME HERE. The room measured it
// against the clock that recorded the request. Sending the timestamp instead
// would have it read against this machine's clock, and two machines that
// disagree by a minute would draw a request as frozen before it was made.
func TestTheWaitIsReadAgainstThisClock(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	if _, err := d.RoomCheckIn(hublink.RoomReport{Name: "leaf", Perms: []hublink.RoomPerm{
		{ID: "p1", Tool: "Bash", Command: "go build", Waiting: 90},
		// A room whose clock ran backwards. Nothing sensible can be drawn from
		// a request made in the future.
		{ID: "p2", Tool: "Bash", Command: "go test", Waiting: -500},
	}}); err != nil {
		t.Fatal(err)
	}
	got := requestsOf(t, d, "leaf")
	if len(got) != 2 {
		t.Fatalf("%d requests", len(got))
	}
	at, err := time.Parse(time.RFC3339, got[0].RequestedAt)
	if err != nil {
		t.Fatalf("%q is not a time the board can read: %v", got[0].RequestedAt, err)
	}
	if d := time.Since(at); d < 89*time.Second || d > 95*time.Second {
		t.Fatalf("ninety seconds of waiting became %s", d)
	}
	future, err := time.Parse(time.RFC3339, got[1].RequestedAt)
	if err != nil {
		t.Fatal(err)
	}
	if future.After(time.Now().Add(time.Second)) {
		t.Fatalf("a request was drawn as made in the future: %s", got[1].RequestedAt)
	}
}

// A report is BOUNDED, because it is untrusted input that has to fit in one
// POST. The room cuts it first and this is the guard against a room that did
// not: a browser asked to draw a thousand permission cards stops responding,
// which is worse than a diff that ends early.
func TestAReportIsBounded(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	many := make([]hublink.RoomPerm, hublink.RoomPermsMax+20)
	for i := range many {
		many[i] = hublink.RoomPerm{
			ID: fmt.Sprintf("p%03d", i), Tool: "Edit",
			Command: "main.go", Details: strings.Repeat("x", hublink.RoomDetailsMax*2),
		}
	}
	if _, err := d.RoomCheckIn(hublink.RoomReport{Name: "loud", Perms: many}); err != nil {
		t.Fatal(err)
	}
	got := requestsOf(t, d, "loud")
	if len(got) != hublink.RoomPermsMax {
		t.Fatalf("%d requests got through a cap of %d", len(got), hublink.RoomPermsMax)
	}
	for _, q := range got {
		if len(q.Details) > hublink.RoomDetailsMax {
			t.Fatalf("a diff of %d bytes got through a cap of %d", len(q.Details), hublink.RoomDetailsMax)
		}
	}
}

// The hub holds NO RECORD of what it decided. The decision, the rule it may
// create and the history it lands in all belong to the machine that was asked,
// and a copy here would be a second answer to "was this approved" that is wrong
// whenever the room refused it.
func TestTheHubKeepsNoRecordOfADecision(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	reportAsking(t, d, "leaf", "p1")
	if err := d.RoomDecide("leaf", "p1", hublink.RoomDecision{
		Decision: "approve", Reason: "go on then",
	}); err != nil {
		t.Fatal(err)
	}
	reportAsking(t, d, "leaf", "p1")
	// Collected, and then the room stops asking.
	if _, err := d.RoomCheckIn(hublink.RoomReport{Name: "leaf"}); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(d.Rooms())
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"approve", "go on then", "p1"} {
		if strings.Contains(string(body), gone) {
			t.Fatalf("the hub is still publishing %q after the room took the decision: %s",
				gone, body)
		}
	}
}

// FORGETTING IS NOT A BLOCK LIST. This is the way the button gets broken: it
// looks like a delete, so somebody makes it stick, and the hub now holds a
// durable record of a refusal, which is the second source of truth this whole
// design exists to not have. A machine still running `atrium room` must come
// straight back.
func TestForgettingARoomDoesNotKeepItAway(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	if _, err := d.RoomCheckIn(hublink.RoomReport{Name: "leaf"}); err != nil {
		t.Fatal(err)
	}
	had, err := d.RoomForget("leaf")
	if err != nil {
		t.Fatal(err)
	}
	if !had {
		t.Fatal("forgetting a listed room said it was not listed")
	}
	if list := roomsOf(t, d); len(list) != 0 {
		t.Fatalf("a forgotten room is still listed: %+v", list)
	}

	if _, err := d.RoomCheckIn(hublink.RoomReport{Name: "leaf"}); err != nil {
		t.Fatal(err)
	}
	if list := roomsOf(t, d); len(list) != 1 {
		t.Fatal("a forgotten room that checked in again was refused, so the hub is holding a " +
			"durable opinion about it")
	}
}

// Forgetting something that is not there is not a failure. Two presses, or a
// room that aged out between the draw and the click, mean the same thing to
// whoever pressed it.
func TestForgettingWhatIsNotThere(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	had, err := d.RoomForget("never-existed")
	if err != nil {
		t.Fatalf("forgetting an unknown room was an error: %v", err)
	}
	if had {
		t.Fatal("a room that was never listed was reported as having been")
	}
	if _, err := d.RoomForget("   "); err == nil {
		t.Fatal("forgetting an unnamed room was accepted, which is every room at once")
	}
}

// A stale room says WHEN it goes, not just that it is stale. Stale on its own
// is a state, and the board needs a deadline to tell somebody whether to wait
// or go and look at that machine.
func TestAStaleRoomSaysWhenItGoes(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	if _, err := d.RoomCheckIn(hublink.RoomReport{Name: "fading"}); err != nil {
		t.Fatal(err)
	}
	fresh := roomsOf(t, d)[0].ForgetIn
	if fresh <= 0 {
		t.Fatalf("a room that just checked in is already out of time: %ds", fresh)
	}

	d.RoomTable.Mu.Lock()
	d.RoomTable.All["fading"].LastSeen = time.Now().Add(-hublink.RoomStale - time.Second)
	d.RoomTable.Mu.Unlock()

	stale := roomsOf(t, d)[0]
	if !stale.Stale {
		t.Fatal("this test is not exercising a stale room")
	}
	if stale.ForgetIn <= 0 || stale.ForgetIn >= fresh {
		t.Fatalf("a quiet room's remaining time did not shrink: %ds, was %ds",
			stale.ForgetIn, fresh)
	}
}

// The join instructions are READ. Nothing about them creates a pending room,
// because there is no such thing: a room exists when it checks in and the hub
// holds nothing else. This is how that gets broken: somebody makes the dialog
// register the room it is about to describe.
func TestAskingHowToJoinAddsNoRoom(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	for i := 0; i < 3; i++ {
		if _, ok := d.RoomJoin().(hublink.RoomJoinInfo); !ok {
			t.Fatalf("RoomJoin answered %T", d.RoomJoin())
		}
	}
	if list := roomsOf(t, d); len(list) != 0 {
		t.Fatalf("asking how to add a room added one: %+v", list)
	}
}

// A private zrok share is a TOKEN, not a URL. Handing it over as `--hub` makes
// a room that retries forever against something that was never an address, and
// the failure is on the far machine where nobody is looking.
func TestJoinOffersNoAddressWhenNothingIsShared(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	info, ok := d.RoomJoin().(hublink.RoomJoinInfo)
	if !ok {
		t.Fatalf("RoomJoin answered %T", d.RoomJoin())
	}
	if info.Share != "" {
		t.Fatalf("a board that is not sharing offered %q as a hub address", info.Share)
	}
	if info.Heartbeat != int(hublink.RoomHeartbeat/time.Second) {
		t.Fatalf("the board would be told the heartbeat is %ds", info.Heartbeat)
	}
	// The timings are reported so the page does not carry its own copy of them
	// and drift. Stale before forget, or the board describes an order that
	// never happens.
	if !(info.Stale < info.Forget) {
		t.Fatalf("stale at %ds and forgotten at %ds is not an order rooms go through",
			info.Stale, info.Forget)
	}
}

// The routes, over HTTP, because that is where this breaks: `join` is one path
// segment away from being read as a room called join, and a board pressing
// forget would get back the join instructions instead.
func TestTheRoomRoutesDoNotOverlap(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	base := "http://" + d.opts.HumanAddr

	if _, err := d.RoomCheckIn(hublink.RoomReport{Name: "join"}); err != nil {
		t.Fatal(err)
	}

	res, err := http.Get(base + "/v1/rooms/join")
	if err != nil {
		t.Fatal(err)
	}
	var info hublink.RoomJoinInfo
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		t.Fatalf("GET /v1/rooms/join did not answer join instructions: %v", err)
	}
	res.Body.Close()
	if info.Heartbeat == 0 {
		t.Fatal("GET /v1/rooms/join answered something that was not the join instructions")
	}
	if list := roomsOf(t, d); len(list) != 1 {
		t.Fatal("reading the join instructions disturbed a room actually called join")
	}

	req, err := http.NewRequest(http.MethodDelete, base+"/v1/rooms/join", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("forgetting a room answered %s", res.Status)
	}
	if list := roomsOf(t, d); len(list) != 0 {
		t.Fatalf("the room survived being forgotten over HTTP: %+v", list)
	}
}
