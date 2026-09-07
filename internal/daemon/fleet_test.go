package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Which of these want me.
//
// FOUR STATES THAT WERE REPEATEDLY CONFUSED, and every test in here is one of
// them or the line between two. A session that finished and filed a recap, one
// that stopped and asked, one that asked while carrying on, and one that has
// gone quiet with nothing recorded. Reading any of these as another is how
// sixteen agents ran for hours with nobody able to say which wanted anything.

// finishOf calls the handler directly. `finish_test.go` has a helper that goes
// over the wire, and that one needs a listening daemon; nothing here does.
func finishOf(t *testing.T, d *Daemon, in FinishRequest) {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	d.handleFinish(rec, httptest.NewRequest(http.MethodPost, "/finish", bytes.NewReader(raw)))
	if rec.Code != http.StatusOK {
		t.Fatalf("finish answered %d: %s", rec.Code, rec.Body)
	}
}

// fleetCard registers a card with a worktree of its own.
//
// `cardFor` in help_test.go uses one fixed path, and Register matches an
// existing card by worktree, so a second call there returns the SAME card
// wearing a new name. Fine for a test with one session in it and wrong for
// every test in here, which is about telling several of them apart.
func fleetCard(t *testing.T, d *Daemon, name string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{
		WireName: name, Worktree: "d:/git/atrium/" + name, Runner: "claude", PID: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func rosterOf(t *testing.T, d *Daemon, fleet bool) []Peer {
	t.Helper()
	list, err := d.roster("", fleet)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func peerNamed(t *testing.T, list []Peer, handle string) Peer {
	t.Helper()
	for _, p := range list {
		if strings.HasSuffix(p.Handle, handle) {
			return p
		}
	}
	t.Fatalf("%s is not in the list: %v", handle, handles(list))
	return Peer{}
}

func handles(list []Peer) []string {
	out := make([]string, 0, len(list))
	for _, p := range list {
		out = append(out, p.Handle+"="+p.Want)
	}
	return out
}

// finished. `atrium finish` with an account of itself.
func TestAFinishedSessionReadsAsFinishedAndCarriesItsRecap(t *testing.T) {
	d := testDaemon(t)
	fleetCard(t, d, "shipper")
	finishOf(t, d, FinishRequest{Agent: "shipper", Recap: "wired the reaper to the pty"})

	p := peerNamed(t, rosterOf(t, d, true), "shipper")
	if p.Want != WantFinished {
		t.Fatalf("a finished session reads as %q (%s)", p.Want, p.Note)
	}
	if !strings.Contains(p.Recap, "reaper") {
		t.Fatalf("the recap did not come with it: %q", p.Recap)
	}
}

// finished, and it said nothing. Still finished, and worth seeing that it is
// the kind with nothing to read.
func TestAFinishedSessionWithNoRecapSaysSo(t *testing.T) {
	d := testDaemon(t)
	fleetCard(t, d, "silent-shipper")
	finishOf(t, d, FinishRequest{Agent: "silent-shipper"})

	p := peerNamed(t, rosterOf(t, d, true), "silent-shipper")
	if p.Want != WantFinished {
		t.Fatalf("reads as %q", p.Want)
	}
	if p.Recap != "" {
		t.Fatalf("invented a recap: %q", p.Recap)
	}
	if !strings.Contains(p.Note, "said nothing") {
		t.Fatalf("does not say the account is missing: %q", p.Note)
	}
}

// THE OTHER REFUSAL, and the one real data found. A card in `done` is not the
// same thing as a session that finished.
//
// Work dragged across by hand, adopted cards, anything the sweep tidied: all
// `done`, none of them a session that ended and left something to read. The
// first board this was pointed at had nineteen real recaps buried under twenty
// five rows of those.
func TestACardInDoneThatNoSessionClaimedIsNotListed(t *testing.T) {
	d := testDaemon(t)
	task := fleetCard(t, d, "dragged")
	if err := d.st.SetStatus(task.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}

	for _, p := range rosterOf(t, d, true) {
		if strings.HasSuffix(p.Handle, "dragged") {
			t.Fatalf("a card nobody finished is listed as finished: %+v", p)
		}
	}
}

// Work that ended before the window is not what a dispatcher is asking about.
//
// The window applies to FINISHED WORK ONLY. A session that is blocked has been
// blocked for however long it has been blocked, and dropping it because it has
// been waiting since yesterday is exactly the failure this whole list exists
// to fix.
func TestTheWindowDropsOldFinishedWorkAndNothingElse(t *testing.T) {
	d := testDaemon(t)
	fleetCard(t, d, "shipped")
	finishOf(t, d, FinishRequest{Agent: "shipped", Recap: "landed it"})
	fleetCard(t, d, "still-stuck")
	askOf(t, d, HelpRequest{Agent: "still-stuck", Blocked: true, Ask: "which one"})

	// A window narrow enough that work finished a moment ago is already past
	// it, which is the same test as a day without having to fake a clock.
	narrow, err := d.rosterWithin("", true, time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range narrow {
		if strings.HasSuffix(p.Handle, "shipped") {
			t.Fatalf("finished work outside the window is still listed: %+v", p)
		}
	}
	if _, ok := findPeer(narrow, "still-stuck"); !ok {
		t.Fatalf("the window dropped a blocked session: %v", handles(narrow))
	}

	// And the default window has it, so the bound is a window and not a second
	// opinion about what counts as finished.
	wide := rosterOf(t, d, true)
	if _, ok := findPeer(wide, "shipped"); !ok {
		t.Fatalf("finished work inside the window is missing: %v", handles(wide))
	}
}

func findPeer(list []Peer, handle string) (Peer, bool) {
	for _, p := range list {
		if strings.HasSuffix(p.Handle, handle) {
			return p, true
		}
	}
	return Peer{}, false
}

// THE REFUSAL. A finished session is not addressable, so it stays out of the
// peer list however useful it is to a dispatcher. Telling a model it can
// message a session that has ended wastes a turn and produces a message
// nobody reads.
func TestAFinishedSessionIsNotOfferedAsSomebodyToTell(t *testing.T) {
	d := testDaemon(t)
	fleetCard(t, d, "gone")
	finishOf(t, d, FinishRequest{Agent: "gone", Recap: "done"})

	for _, p := range rosterOf(t, d, false) {
		if strings.HasSuffix(p.Handle, "gone") {
			t.Fatalf("a finished session was offered as a peer to tell: %v", p)
		}
	}
}

// blocked. `atrium ask`, stopped, with the words on the card.
func TestASessionThatStoppedAndAskedReadsAsBlockedAndShowsTheQuestion(t *testing.T) {
	d := testDaemon(t)
	fleetCard(t, d, "stuck")
	askOf(t, d, HelpRequest{
		Agent: "stuck", Blocked: true,
		Ask: "which of these two schemas is authoritative",
	})

	p := peerNamed(t, rosterOf(t, d, false), "stuck")
	if p.Want != WantBlocked {
		t.Fatalf("a stopped session that asked reads as %q (%s)", p.Want, p.Note)
	}
	if !p.Blocked {
		t.Fatal("it is not marked as having stopped")
	}
	if !strings.Contains(p.Ask, "authoritative") {
		t.Fatalf("the question is not on the row: %q", p.Ask)
	}
	if !strings.Contains(p.Note, "authoritative") {
		t.Fatalf("the printed line does not carry the question: %q", p.Note)
	}
}

// A blocked ask records WHY it is waiting, not just that it is.
//
// Without this the card lands in needs-input with an empty waiting reason,
// which reads as "a turn ended", and a session that stopped on purpose to ask
// something is indistinguishable from one that merely ran out of things to do.
func TestABlockedAskRecordsThatItIsAQuestion(t *testing.T) {
	d := testDaemon(t)
	task := fleetCard(t, d, "asker")
	askOf(t, d, HelpRequest{Agent: "asker", Blocked: true, Ask: "which branch"})

	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.WaitingReason != store.WaitingAsked {
		t.Fatalf("waiting for reason %q, so it reads as a turn that ended", got.WaitingReason)
	}
}

// question. `atrium ask --continue`: still going, and it would like an answer.
//
// The line that matters: it must NOT read as blocked, and it must not have
// moved the card. A working session filed as waiting makes the count that
// drives every alert lie.
func TestASessionThatAskedWhileWorkingIsNotFiledAsBlocked(t *testing.T) {
	d := testDaemon(t)
	task := fleetCard(t, d, "curious")
	askOf(t, d, HelpRequest{
		Agent: "curious", Blocked: false,
		Ask: "do you want the postgres path stubbed",
	})

	p := peerNamed(t, rosterOf(t, d, false), "curious")
	if p.Want != WantQuestion {
		t.Fatalf("a working session that asked reads as %q (%s)", p.Want, p.Note)
	}
	if p.Blocked {
		t.Fatal("a session that never stopped is marked as stopped")
	}
	if !strings.Contains(p.Note, "still working") {
		t.Fatalf("the line does not say it is still going: %q", p.Note)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == store.StatusNeedsInput {
		t.Fatal("asking while working moved the card into the waiting column")
	}
}

// quiet. THE ONE NOBODY LOOKS AT. Nothing recorded, nothing heard, and no
// question to bring it to anybody's attention.
func TestASessionWithNothingRecordedReadsAsQuiet(t *testing.T) {
	d := testDaemon(t)
	task := fleetCard(t, d, "vanished")
	if err := d.st.SetStatus(task.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := d.st.BackdateActivity(task.ID, time.Hour); err != nil {
		t.Fatal(err)
	}

	p := peerNamed(t, rosterOf(t, d, false), "vanished")
	if p.Want != WantQuiet {
		t.Fatalf("a session nobody has heard from reads as %q (%s)", p.Want, p.Note)
	}
	if !strings.Contains(p.Note, "no word") {
		t.Fatalf("the line does not say nothing has been heard: %q", p.Note)
	}
}

// A session that is plainly moving is not quiet, however long the card has
// been open. Activity is the answer here and it comes out of memory, never the
// database.
func TestASessionThatIsMovingIsNotQuiet(t *testing.T) {
	d := testDaemon(t)
	task := fleetCard(t, d, "busy")
	if err := d.st.SetStatus(task.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := d.st.BackdateActivity(task.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	d.act.set(task.ID, ActivityTool, "Bash")

	p := peerNamed(t, rosterOf(t, d, false), "busy")
	if p.Want != WantWorking {
		t.Fatalf("a session running a tool reads as %q (%s)", p.Want, p.Note)
	}
	if !strings.Contains(p.Note, "Bash") {
		t.Fatalf("the line does not say what it is doing: %q", p.Note)
	}
}

// A turn that ended and said nothing is also nothing recorded. It is in the
// waiting column and atrium cannot say what for, which is the same problem
// wearing a different status.
func TestATurnThatEndedWithNothingSaidReadsAsQuiet(t *testing.T) {
	d := testDaemon(t)
	task := fleetCard(t, d, "trailed-off")
	if err := d.st.SetStatus(task.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}

	p := peerNamed(t, rosterOf(t, d, false), "trailed-off")
	if p.Want != WantQuiet {
		t.Fatalf("a turn that ended silently reads as %q (%s)", p.Want, p.Note)
	}
}

// THE ORDER IS THE FEATURE. A dispatcher reads the top of this list and stops,
// so anything that wants a human has to be above anything that does not.
func TestTheMostWantingComeFirst(t *testing.T) {
	d := testDaemon(t)

	working := fleetCard(t, d, "z-working")
	if err := d.st.SetStatus(working.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	d.act.set(working.ID, ActivityThinking, "")

	quiet := fleetCard(t, d, "y-quiet")
	if err := d.st.SetStatus(quiet.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := d.st.BackdateActivity(quiet.ID, time.Hour); err != nil {
		t.Fatal(err)
	}

	fleetCard(t, d, "x-finished")
	finishOf(t, d, FinishRequest{Agent: "x-finished", Recap: "landed it"})

	fleetCard(t, d, "w-asked")
	askOf(t, d, HelpRequest{Agent: "w-asked", Blocked: true, Ask: "which one"})

	var got []string
	for _, p := range rosterOf(t, d, true) {
		got = append(got, p.Want)
	}
	want := []string{WantBlocked, WantFinished, WantQuiet, WantWorking}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ranked %v, wanted %v", got, want)
		}
	}
}

// Within a bucket, the one that has wanted somebody longest comes first. How
// long a thing has waited is a fact, not a judgement, which is the rule the
// Stack view already follows.
func TestWithinABucketTheLongestWaitComesFirst(t *testing.T) {
	d := testDaemon(t)
	old := fleetCard(t, d, "old-quiet")
	recent := fleetCard(t, d, "recent-quiet")
	for _, c := range []*store.Task{old, recent} {
		if err := d.st.SetStatus(c.ID, store.StatusRunning); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.st.BackdateActivity(old.ID, 4*time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := d.st.BackdateActivity(recent.ID, 20*time.Minute); err != nil {
		t.Fatal(err)
	}

	list := rosterOf(t, d, false)
	if len(list) < 2 || !strings.HasSuffix(list[0].Handle, "old-quiet") {
		t.Fatalf("the longest quiet is not first: %v", handles(list))
	}
}

// The endpoint answers both questions, and the fleet one is the only one that
// includes work that is over.
func TestTheEndpointOnlyIncludesFinishedWorkWhenAsked(t *testing.T) {
	d := testDaemon(t)
	fleetCard(t, d, "over")
	finishOf(t, d, FinishRequest{Agent: "over", Recap: "done"})

	for _, tc := range []struct {
		query string
		want  bool
	}{{"", false}, {"&fleet=1", true}} {
		rec := httptest.NewRecorder()
		d.handlePeers(rec, httptest.NewRequest(http.MethodGet, "/peers?me=nobody"+tc.query, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("answered %d", rec.Code)
		}
		var body struct {
			Peers []Peer `json:"peers"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, p := range body.Peers {
			if strings.HasSuffix(p.Handle, "over") {
				found = true
			}
		}
		if found != tc.want {
			t.Fatalf("with query %q the finished card present=%v", tc.query, found)
		}
	}
}
