package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// An ask that reaches another session, and the answer that comes back.
//
// The load-bearing test in here is the first one: a routed question is QUEUED
// for the peer, never typed into its terminal, even when atrium owns one and
// could. `peers_test.go` pins the same refusal for `tell`. It is restated here
// because `ask --peer` is a second door onto the same room, and the tempting
// simplification is available at both.

func answerOf(t *testing.T, d *Daemon, from, to, text string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"from": from, "to": to, "text": text})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	d.handleAnswer(rec, httptest.NewRequest(http.MethodPost, "/answer", bytes.NewReader(raw)))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestAnAskToAPeerIsQueuedAndNeverTyped(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "asker")
	helper := peerCard(t, d, "helper")

	// Supervised, so the terminal branch exists and anything reusing the human
	// message path would take it.
	d.sup.mu.Lock()
	d.sup.runners[helper.ID] = &runner{}
	d.sup.mu.Unlock()
	if d.sup.get(helper.ID) == nil {
		t.Fatal("this test is not exercising a supervised peer")
	}

	rec, out := askOf(t, d, HelpRequest{
		Agent: "asker", Blocked: true, Peer: "helper",
		Ask: "which of these two schemas is authoritative",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("asking a peer answered %d: %s", rec.Code, rec.Body)
	}
	if out["peer"] != d.st.Qualify("helper") {
		t.Fatalf("the answer does not say who was asked: %v", out)
	}

	pending, err := d.st.PendingMessages(helper.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("an ask to a supervised peer queued %d messages, which means it "+
			"was typed instead", len(pending))
	}
	if pending[0].FromHuman() {
		t.Fatal("a routed ask is attributed to the human")
	}
	if !strings.Contains(pending[0].Text, "authoritative") {
		t.Fatalf("the question did not arrive: %q", pending[0].Text)
	}
	// The envelope has to say how to answer. Without it a well-behaved model
	// writes a good answer into its own transcript, where nobody reads it, and
	// the loop this exists to close stays open.
	if !strings.Contains(pending[0].Text, "atrium answer") {
		t.Fatalf("the peer was not told how to answer: %q", pending[0].Text)
	}
}

// The card says WHO it asked. A session stopped on another session must not
// read as one stopped on you, and the peer's name is the only thing that tells
// those apart.
func TestACardSaysWhichPeerItAsked(t *testing.T) {
	d := testDaemon(t)
	asker := peerCard(t, d, "asker")
	peerCard(t, d, "helper")

	askOf(t, d, HelpRequest{Agent: "asker", Blocked: true, Peer: "helper", Ask: "which branch"})

	got, err := d.st.Get(asker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.AskedAPeer() || got.AskPeer != d.st.Qualify("helper") {
		t.Fatalf("the card does not say who it asked: ask=%q peer=%q", got.Ask, got.AskPeer)
	}
	// And it still moved, because it has genuinely stopped. A stopped session
	// hidden from the board because somebody else owes it an answer is worse
	// than one shown as waiting on a named peer.
	if got.Status != store.StatusNeedsInput {
		t.Fatalf("a blocked session that asked a peer is filed as %q", got.Status)
	}
}

// A HANDLE NOBODY HAS REFUSES, AND RECORDS NOTHING.
//
// Falling back to the board would be atrium deciding who answers, and the card
// would claim a peer was on the hook when nobody was. The refusal carries the
// handles that would have worked, which is the only discovery a bare
// subcommand can offer.
func TestAnAskToAnUnknownPeerIsRefusedAndTeaches(t *testing.T) {
	d := testDaemon(t)
	asker := peerCard(t, d, "asker")
	peerCard(t, d, "helper")

	rec, out := askOf(t, d, HelpRequest{
		Agent: "asker", Blocked: true, Peer: "nobody", Ask: "anybody there",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown handle answered %d", rec.Code)
	}
	peers, ok := out["peers"].([]any)
	if !ok || len(peers) == 0 {
		t.Fatalf("the refusal did not offer the list: %v", out)
	}

	got, err := d.st.Get(asker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Asking() {
		t.Fatalf("a refused route still wrote %q to the card", got.Ask)
	}
	if got.Status == store.StatusNeedsInput {
		t.Fatal("a refused route filed the card as waiting anyway")
	}
}

// A session cannot route a question to itself, which is a loop with extra
// steps and queues a message it then has to read.
func TestASessionCannotAskItself(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "alone")
	rec, _ := askOf(t, d, HelpRequest{Agent: "alone", Peer: "alone", Ask: "well?"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a session asked itself, answering %d", rec.Code)
	}
}

// A peer that has ended cannot answer anything, and saying so beats a card
// claiming it is waiting on a dead session.
func TestAskingAnEndedPeerIsRefused(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "asker")
	gone := peerCard(t, d, "gone")
	if err := d.st.SetStatus(gone.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	rec, _ := askOf(t, d, HelpRequest{Agent: "asker", Peer: "gone", Ask: "hello"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("asking a dead peer answered %d", rec.Code)
	}
}

// The flood limit belongs to the peer bus, not to `tell`. A model in a loop
// asking twenty questions a minute is the same failure as one sending twenty
// messages, and a limit that only two of three doors enforce is not a limit.
func TestRoutedAsksAreBoundedByTheSameLimit(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "asker")
	peerCard(t, d, "helper")

	for i := 0; i < peerSendsPerMinute; i++ {
		rec, _ := askOf(t, d, HelpRequest{Agent: "asker", Peer: "helper", Ask: "again"})
		if rec.Code != http.StatusOK {
			t.Fatalf("ask %d was refused early with %d", i+1, rec.Code)
		}
	}
	rec, _ := askOf(t, d, HelpRequest{Agent: "asker", Peer: "helper", Ask: "one too many"})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the limit did not hold for routed asks: answered %d", rec.Code)
	}
}

// An answer lands the same way the ask did: queued on the same bus, carried by
// whichever hook fires first, and it takes the question off the card.
func TestAnAnswerIsQueuedForTheAskerAndClearsTheCard(t *testing.T) {
	d := testDaemon(t)
	asker := peerCard(t, d, "asker")
	peerCard(t, d, "helper")

	askOf(t, d, HelpRequest{
		Agent: "asker", Blocked: true, Peer: "helper", Ask: "which schema is authoritative",
	})
	rec, out := answerOf(t, d, "helper", "asker", "the one in migrations, 0031 replaced it")
	if rec.Code != http.StatusOK {
		t.Fatalf("answering answered %d: %s", rec.Code, rec.Body)
	}
	if out["answered"] != true {
		t.Fatalf("the answer did not settle the question: %v", out)
	}

	pending, err := d.st.PendingMessages(asker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("%d messages are waiting for the asker", len(pending))
	}
	if pending[0].FromHuman() {
		t.Fatal("a peer's answer is attributed to the human")
	}
	// The question is quoted back, because a session that asked an hour ago
	// may have compacted the context it asked from.
	if !strings.Contains(pending[0].Text, "authoritative") {
		t.Fatalf("the answer does not carry the question: %q", pending[0].Text)
	}
	if !strings.Contains(pending[0].Text, "0031") {
		t.Fatalf("the answer itself did not arrive: %q", pending[0].Text)
	}

	// AND THE CARD STOPS ASKING. That is the only signal anybody has that the
	// ask is settled, and a board still showing an answered question makes it
	// worth nothing.
	got, err := d.st.Get(asker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Asking() {
		t.Fatalf("the card is still asking after being answered: %q", got.Ask)
	}
	if got.AskPeer != "" {
		t.Fatalf("the card still names %q as owing it an answer", got.AskPeer)
	}
}

// The answer does NOT move the asker's status. Delivery does that, when a hook
// hands the message over. Moving it here would put the card in `running` while
// the session is still sitting at its prompt.
func TestAnAnswerDoesNotMoveTheCardBackByItself(t *testing.T) {
	d := testDaemon(t)
	asker := peerCard(t, d, "asker")
	peerCard(t, d, "helper")

	askOf(t, d, HelpRequest{Agent: "asker", Blocked: true, Peer: "helper", Ask: "which schema"})
	answerOf(t, d, "helper", "asker", "the one in migrations")

	got, err := d.st.Get(asker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusNeedsInput {
		t.Fatalf("answering moved the card to %q before the session heard anything", got.Status)
	}
}

// Answering something nobody asked still arrives, and says nothing was
// cleared. Refusing would fail a session for being helpful, and saying nothing
// would leave it believing it had settled a card.
func TestAnAnswerToASessionWithNoQuestionSaysSo(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "asker")
	peerCard(t, d, "helper")

	rec, out := answerOf(t, d, "helper", "asker", "here is a thing you did not ask for")
	if rec.Code != http.StatusOK {
		t.Fatalf("answering answered %d", rec.Code)
	}
	if out["answered"] != false {
		t.Fatalf("it claimed to settle a question nobody asked: %v", out)
	}
}

// THE OPERATOR ANSWERING IS THE SAME ACT THROUGH THE OTHER CHANNEL.
//
// A card left holding a question that was answered minutes ago makes the one
// field that says "somebody still owes this session something" mean nothing.
func TestAMessageFromTheOperatorAlsoSettlesTheAsk(t *testing.T) {
	d := testDaemon(t)
	asker := peerCard(t, d, "asker")

	askOf(t, d, HelpRequest{Agent: "asker", Blocked: true, Ask: "which branch is base"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+asker.ID+"/message",
		strings.NewReader(`{"text":"base is main"}`))
	req.SetPathValue("id", asker.ID)
	d.handleMessage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("saying something to the card answered %d: %s", rec.Code, rec.Body)
	}

	got, err := d.st.Get(asker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Asking() {
		t.Fatalf("the operator answered and the card is still asking: %q", got.Ask)
	}
}

// A session whose work is over is not waiting on an answer. Otherwise a card
// that got unstuck on its own and finished sits in `done` still asking, and
// the field collects cards nobody owes anything to.
func TestFinishingTakesTheQuestionOffTheCard(t *testing.T) {
	d := testDaemon(t)
	asker := peerCard(t, d, "asker")
	askOf(t, d, HelpRequest{Agent: "asker", Blocked: true, Ask: "which branch is base"})

	raw, err := json.Marshal(FinishRequest{Agent: "asker", Recap: "worked it out, it was main"})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	d.handleFinish(rec, httptest.NewRequest(http.MethodPost, "/finish", bytes.NewReader(raw)))
	if rec.Code != http.StatusOK {
		t.Fatalf("finishing answered %d", rec.Code)
	}

	got, err := d.st.Get(asker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Asking() {
		t.Fatalf("a finished card is still asking: %q", got.Ask)
	}
}

// A PEER'S ANSWER SETTLES WHAT THE PEER WAS ASKED, AND NOTHING ELSE.
//
// A card can be waiting on a peer for one thing and on you for another.
// `ask_peer` is per question, so a reply from one peer cannot have answered a
// question that was put to a human, and clearing the card wholesale would take
// that question off the board without anybody ever answering it.
func TestAPeerAnswerLeavesTheHumanQuestionStanding(t *testing.T) {
	d := testDaemon(t)
	asker := peerCard(t, d, "asker")
	peerCard(t, d, "helper")

	askOf(t, d, HelpRequest{
		Agent: "asker", Peer: "helper", Ask: "which of these two schemas is authoritative",
	})
	askOf(t, d, HelpRequest{
		Agent: "asker", Blocked: true, Ask: "do you want the postgres path stubbed",
	})

	_, out := answerOf(t, d, "helper", "asker", "the one in migrations, 0031 replaced it")
	if out["answered"] != true {
		t.Fatalf("the peer's answer did not settle the question it was asked: %v", out)
	}

	outstanding, err := d.st.OpenAsks(asker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(outstanding) != 1 || !strings.Contains(outstanding[0].Text, "postgres") {
		t.Fatalf("a peer's answer took the human's question with it: %v", outstanding)
	}

	got, err := d.st.Get(asker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Asking() {
		t.Fatal("the card stopped asking while it was still waiting on a human")
	}
	if got.AskPeer != "" {
		t.Fatalf("the card still names %q as owing it an answer", got.AskPeer)
	}

	// And the peer is quoted ITS OWN question, not whatever the card is
	// drawing. An answer stapled to somebody else's question is a puzzle.
	pending, err := d.st.PendingMessages(asker.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := pending[len(pending)-1]
	if !strings.Contains(last.Text, "authoritative") {
		t.Fatalf("the answer quoted back the wrong question: %q", last.Text)
	}
}

// The operator saying something to the card settles EVERY question on it. That
// is the broad door on purpose: a message is not addressed to one question,
// and a card left holding an answered one makes the field that says "somebody
// still owes this session something" mean nothing.
func TestAMessageFromTheOperatorSettlesEveryQuestion(t *testing.T) {
	d := testDaemon(t)
	asker := peerCard(t, d, "asker")
	peerCard(t, d, "helper")

	askOf(t, d, HelpRequest{Agent: "asker", Peer: "helper", Ask: "which schema is authoritative"})
	askOf(t, d, HelpRequest{Agent: "asker", Blocked: true, Ask: "which branch is base"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+asker.ID+"/message",
		strings.NewReader(`{"text":"base is main, and the migrations one is authoritative"}`))
	req.SetPathValue("id", asker.ID)
	d.handleMessage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("saying something to the card answered %d: %s", rec.Code, rec.Body)
	}

	outstanding, err := d.st.OpenAsks(asker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(outstanding) != 0 {
		t.Fatalf("the operator answered and %d question(s) are still outstanding", len(outstanding))
	}
}
