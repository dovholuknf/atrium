//go:build integration

package link

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// A say with wake to a parked card on another room resumes it there and delivers, and the
// sender hears delivered, not parked.
func TestAWakeSayResumesAParkedCardOnAnotherRoom(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.sg4.parked = map[string]bool{"s1": true}

	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelaySay, From: "sa1", Room: "sg4",
		To: "atrium-87300", Text: "wake up", Wake: true})
	if err != nil || !ans.OK || ans.Delivered != "queued" || ans.Card != "sg4~s1" {
		t.Fatalf("answer = %+v, %v, want delivered", ans, err)
	}
	if got := x.sg4.resumes(); len(got) != 1 || got[0] != "s1" {
		t.Fatalf("resumed = %v, want s1 resumed once", got)
	}
	if got := x.sg4.messages(); len(got) != 1 || got[0]["text"] != "wake up" || got[0]["from"] != "sa1@m1mini" {
		t.Fatalf("sg4 got %+v, want the message from sa1@m1mini", got)
	}
}

// Without wake the same card is refused as parked, as before.
func TestASayWithoutWakeToAParkedCardOnAnotherRoomIsStillParked(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.sg4.parked = map[string]bool{"s1": true}

	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelaySay, From: "sa1", Room: "sg4", To: "atrium-87300", Text: "x"})
	if err != nil || ans.Delivered != "parked" || len(x.sg4.resumes()) != 0 || len(x.sg4.messages()) != 0 {
		t.Fatalf("answer = %+v, %v, resumed %v, got %v", ans, err, x.sg4.resumes(), x.sg4.messages())
	}
}

// A card that is not parked is delivered to as it would be without wake: nothing is resumed.
func TestAWakeSayToACardThatIsNotParkedDoesNotResume(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelaySay, From: "sa1", Room: "sg4",
		To: "atrium-87300", Text: "x", Wake: true})
	if err != nil || !ans.OK || ans.Delivered != "queued" {
		t.Fatalf("answer = %+v, %v", ans, err)
	}
	if got := x.sg4.resumes(); len(got) != 0 {
		t.Fatalf("resumed = %v, want none", got)
	}
}

// A room that is not attached refuses, and nothing is resumed or queued.
func TestAWakeSayToAnUnattachedRoomIsRefused(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.sg4.parked = map[string]bool{"s1": true}

	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelaySay, From: "sa1", Room: "nowhere", To: "orch", Text: "x", Wake: true})
	if err != nil || ans.OK || ans.Code != http.StatusNotFound || !strings.Contains(ans.Error, "nowhere") {
		t.Fatalf("answer = %+v, %v, want the refusal every cross-room verb gives", ans, err)
	}
	if len(x.sg4.resumes()) != 0 || len(x.sg4.messages()) != 0 {
		t.Fatal("nothing should have been resumed or delivered")
	}
}

// A card that fails to resume is reported and not delivered.
func TestAWakeSayToACardThatFailsToResumeIsReportedNotDelivered(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.sg4.parked = map[string]bool{"s1": true}
	x.sg4.wakeCode = http.StatusInternalServerError

	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelaySay, From: "sa1", Room: "sg4",
		To: "atrium-87300", Text: "x", Wake: true})
	if err != nil || ans.OK || ans.Code != http.StatusInternalServerError || ans.Unconfirmed || ans.Unreachable {
		t.Fatalf("answer = %+v, %v, want a refusal", ans, err)
	}
	if len(x.sg4.messages()) != 0 {
		t.Fatal("nothing should have been delivered")
	}
}

// A resume that may have happened and was not answered is unconfirmed, never unreachable, so it
// is not repeated.
func TestAWakeSayWhoseResumeIsNotAnsweredIsUnconfirmed(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.sg4.parked = map[string]bool{"s1": true}
	x.sg4.wakeCode = http.StatusGatewayTimeout

	ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelaySay, From: "sa1", Room: "sg4",
		To: "atrium-87300", Text: "x", Wake: true})
	if err != nil || ans.OK || ans.Unreachable || !ans.Unconfirmed {
		t.Fatalf("answer = %+v, %v, want unconfirmed", ans, err)
	}
}

// The hub-side tool hands wake to the sender's room, which relays it.
func TestAHubSideWakeSayPassesWakeToTheSendersRoom(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	if _, _, err := x.control.sayHandler(relayCtx(t), ctlReq("sa1", "m1mini"),
		sayInput{To: "atrium-87300@sg4", Text: "hello", Wake: true}); err != nil {
		t.Fatalf("say: %v", err)
	}
	if said := x.mini.says(); len(said) != 1 || said[0]["wake"] != "true" {
		t.Fatalf("m1mini's /v1/say got %+v, want wake=true", said)
	}
	if _, _, err := x.control.sayHandler(relayCtx(t), ctlReq("sa1", "m1mini"),
		sayInput{To: "atrium-87300@sg4", Text: "again"}); err != nil {
		t.Fatalf("say: %v", err)
	}
	if said := x.mini.says(); len(said) != 2 || said[1]["wake"] != "" {
		t.Fatalf("a say without wake carried one: %+v", said)
	}
}

// A sender's room older than /v1/say: the hub delivers it itself, and the wake still resumes.
func TestAHubSideWakeSayFromAnOldRoomStillResumes(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.mini.mu.Lock()
	x.mini.noSay = true
	x.mini.mu.Unlock()
	x.sg4.parked = map[string]bool{"s1": true}

	_, out, err := x.control.sayHandler(relayCtx(t), ctlReq("sa1", "m1mini"),
		sayInput{To: "atrium-87300@sg4", Text: "hello", Wake: true})
	if err != nil || out.Delivered != "queued" || len(x.sg4.resumes()) != 1 {
		t.Fatalf("out = %+v, %v, resumed %v", out, err, x.sg4.resumes())
	}
}

// The wake parameter no longer says it is local only.
func TestTheWakeParameterIsNotDescribedAsLocalOnly(t *testing.T) {
	f, ok := reflect.TypeOf(sayInput{}).FieldByName("Wake")
	if !ok {
		t.Fatal("sayInput has no Wake")
	}
	if tag := f.Tag.Get("jsonschema"); tag == "" || strings.Contains(tag, "local only") {
		t.Fatalf("jsonschema = %q", tag)
	}
}

// M1 of the review. The sender's room refuses a wake say it could not relay with a 424 and says
// nothing was sent. The hub's own atrium_say must hand that to the caller as an error, in those
// words, and not as `unconfirmed`, which tells it to wait and ask a card that is parked.
func TestAHubSideWakeSayTheSendersRoomRefusesIsAnErrorNotUnconfirmed(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.mini.mu.Lock()
	x.mini.sayCode = http.StatusFailedDependency
	x.mini.sayAnswer = map[string]any{"error": "could not reach sg4 (not attached). nothing was sent or held, " +
		"since a held message cannot wake a card. send it again with wake=true when the room is back"}
	x.mini.mu.Unlock()

	_, out, err := x.control.sayHandler(relayCtx(t), ctlReq("sa1", "m1mini"),
		sayInput{To: "atrium-87300@sg4", Text: "hello", Wake: true})
	if err == nil || out.Delivered == "unconfirmed" || strings.Contains(err.Error(), "ask whether it arrived") {
		t.Fatalf("out = %+v, err = %v, want an error that says nothing was sent", out, err)
	}
	if !strings.Contains(err.Error(), "nothing was sent or held") {
		t.Fatalf("err = %q, want the room's own wording", err)
	}
}

// The same answer as a 503 is what the hub reads as in flight. This pins the reason the refusal is
// not one.
func TestAHubSideSayReadsA503FromTheSendersRoomAsUnconfirmed(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	x.mini.mu.Lock()
	x.mini.sayCode = http.StatusServiceUnavailable
	x.mini.sayAnswer = map[string]any{"error": "x"}
	x.mini.mu.Unlock()

	_, out, err := x.control.sayHandler(relayCtx(t), ctlReq("sa1", "m1mini"),
		sayInput{To: "atrium-87300@sg4", Text: "hello", Wake: true})
	if err != nil || out.Delivered != "unconfirmed" {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
}
