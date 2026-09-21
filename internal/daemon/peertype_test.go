package daemon

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Whether one session may type into another's terminal, and what it does about
// Enter when it does.
//
// This reverses a refusal that was argued at length, so the tests are about
// the thing the refusal was protecting: a line the operator is part way
// through is still never typed into.

// typedRunner is a supervised runner with a terminal that records what was
// written to it. A real pseudo terminal would make this a test of ConPTY.
func typedRunner(t *testing.T, d *Daemon, taskID string) (*runner, *fakePTY) {
	t.Helper()
	f := newFakePTY()
	r := &runner{
		taskID:   taskID,
		pty:      f,
		started:  time.Now(),
		buf:      newRing(1<<16, 80),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	d.sup.add(r)
	t.Cleanup(func() { f.Close() })
	return r, f
}

// A card with a terminal, ready to be told something.
func peerPair(t *testing.T, d *Daemon) (*store.Task, *runner, *fakePTY) {
	t.Helper()
	target := cardFor(t, d, "listener")
	r, f := typedRunner(t, d, target.ID)
	fresh, err := d.st.Get(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	return fresh, r, f
}

// NOBODY AT THE KEYBOARD, which is the case this feature exists for: an agent
// talking to an agent while the operator is asleep. Typed AND submitted,
// because a message that does not submit does nothing.
func TestAPeerMessageIsTypedAndSentWhenNobodyIsTyping(t *testing.T) {
	d := testDaemon(t)
	target, _, f := peerPair(t, d)

	typed, how := d.tellByTyping(target, "sg4/builder", "the migration is ready")
	if !typed {
		t.Fatal("queued it when the terminal was free")
	}
	got := f.written()
	if !strings.Contains(got, "the migration is ready") {
		t.Fatalf("the message never reached the terminal: %q", got)
	}
	if !strings.Contains(got, "sg4/builder") {
		t.Fatalf("nothing said who it was from, so it reads as the operator: %q", got)
	}
	if !strings.Contains(got, "\r") {
		t.Fatalf("never pressed Enter, so it sits in the prompt doing nothing: %q", got)
	}
	if !strings.Contains(how, "sent") {
		t.Fatalf("the answer does not say it was sent: %q", how)
	}
}

// THE CASE THE OLD REFUSAL PROTECTED, and it stays protected. There is no way
// to insert into a line somebody is halfway through without wrecking it.
func TestAPeerMessageIsNeverTypedIntoAPartWrittenLine(t *testing.T) {
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	r.noteOperatorTyped([]byte("git comm"))

	typed, _ := d.tellByTyping(target, "sg4/builder", "stop what you are doing")
	if typed {
		t.Fatalf("typed into a half written command: %q", f.written())
	}
	if strings.Contains(f.written(), "stop what you are doing") {
		t.Fatalf("wrote it anyway: %q", f.written())
	}
	// And nothing at all reached the terminal, so the operator's part written
	// line was neither added to nor submitted. A single carriage return here
	// would be an Enter pressed under his hands.
	if f.written() != "" {
		t.Fatalf("wrote into a part written line: %q", f.written())
	}
}

// THE BANNER CAN NEVER PRESS ENTER. It is written into the operator's prompt,
// so a carriage return in it submits whatever he had already typed. That is
// what split his line: the banner used to open and close with `\r\n`.
func TestThePeerBannerNeverSubmitsALine(t *testing.T) {
	if strings.ContainsAny(peerBanner("sg4/builder"), "\r\n") {
		t.Fatalf("the banner carries a line ending, so it can submit the operator's line: %q",
			peerBanner("sg4/builder"))
	}
}

// THE BUG CLINT HIT, end to end through the peer bus. He was composing a line
// in a supervised session when a peer `atrium_say` arrived. It must not reach
// the pty at all, and it must fall to the queue so nothing is lost.
func TestAPeerTellWhileTypingQueuesAndLeavesThePtyAlone(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "sender")
	target, r, f := peerPair(t, d) // wire name "listener", with a fakePTY

	// A part written line, the moment the peer message lands.
	r.noteOperatorTyped([]byte("make peer message a bit"))

	out, code := tell(t, d, "sender", "listener", "make progress on the redo")
	if code != http.StatusOK {
		t.Fatalf("the peer bus answered %d: %v", code, out)
	}
	if f.written() != "" {
		t.Fatalf("a peer message reached the terminal while the operator was typing: %q", f.written())
	}
	if out["queued"] != true {
		t.Fatalf("the message was not queued for later delivery: %v", out)
	}
	pending, err := d.st.PendingMessages(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("the deferred message was not queued: %d pending", len(pending))
	}
	if !strings.Contains(pending[0].FromPeer, "sender") {
		t.Fatalf("the queued message lost its sender: %q", pending[0].FromPeer)
	}
}

// THE RELAY PATH IS PEER TEXT TOO. A message posted with a `from`, as the hub's
// atrium_say queues one, must not type into a line the operator is composing any
// more than the bus may. Same guard, same fallback to the queue.
func TestARelayPeerMessageWhileTypingQueuesAndLeavesThePtyAlone(t *testing.T) {
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	r.noteOperatorTyped([]byte("git comm"))

	if code := message(t, d, target.ID, map[string]string{
		"text": "look at the redo", "from": "expose-board-redo",
	}); code != http.StatusOK {
		t.Fatalf("the relay message answered %d", code)
	}
	if f.written() != "" {
		t.Fatalf("a relay peer message typed into a part written line: %q", f.written())
	}
	pending, err := d.st.PendingMessages(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("the deferred relay message was not queued: %d pending", len(pending))
	}
	if pending[0].FromHuman() || !strings.Contains(pending[0].FromPeer, "expose-board-redo") {
		t.Fatalf("the queued relay message lost its peer sender: from %q", pending[0].FromPeer)
	}
}

// And into a clear terminal the relay path still types, so the guard is a defer
// and not a refusal.
func TestARelayPeerMessageTypesIntoAFreeTerminal(t *testing.T) {
	d := testDaemon(t)
	target, _, f := peerPair(t, d)

	if code := message(t, d, target.ID, map[string]string{
		"text": "the build is green", "from": "ci-green",
	}); code != http.StatusOK {
		t.Fatalf("the relay message answered %d", code)
	}
	if !strings.Contains(f.written(), "the build is green") {
		t.Fatalf("a relay peer message never reached a free terminal: %q", f.written())
	}
	if !strings.Contains(f.written(), "ci-green") {
		t.Fatalf("the banner did not name the sender: %q", f.written())
	}
	pending, err := d.st.PendingMessages(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("a typed relay message was also queued: %d pending", len(pending))
	}
}

// And the line ending releases it. Submitting or abandoning a line both end
// it, which is what makes the deferral temporary rather than a session that
// can never be told anything again.
func TestSubmittingTheLineMakesTheTerminalAvailableAgain(t *testing.T) {
	for _, end := range []struct {
		name string
		key  []byte
	}{
		{"enter", []byte("\r")},
		{"newline", []byte("\n")},
		{"control-c", []byte{0x03}},
		{"control-u", []byte{0x15}},
	} {
		t.Run(end.name, func(t *testing.T) {
			d := testDaemon(t)
			target, r, _ := peerPair(t, d)
			r.noteOperatorTyped([]byte("git comm"))
			if r.howBusy() != peerMidLine {
				t.Fatal("a part written line was not noticed")
			}
			r.noteOperatorTyped(end.key)
			if r.howBusy() == peerMidLine {
				t.Fatalf("%s did not end the line", end.name)
			}
			_ = target
		})
	}
}

// ATTACHED AND WATCHING. The pty is shared so the text goes in, and Enter is
// not pressed: putting words in front of somebody is a different act from
// submitting under their hands.
func TestAPeerMessageIsNotSubmittedWhileTheOperatorIsAround(t *testing.T) {
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	// A line that was finished a moment ago. Nothing is part written, but
	// somebody is clearly there.
	r.noteOperatorTyped([]byte("ls\r"))

	typed, how := d.tellByTyping(target, "sg4/builder", "have a look at this")
	if !typed {
		t.Fatal("refused to type at all, which is the old behaviour")
	}
	got := f.written()
	if !strings.Contains(got, "have a look at this") {
		t.Fatalf("the message never reached the terminal: %q", got)
	}
	if strings.Contains(got, "have a look at this\r") {
		t.Fatalf("submitted it under the operator's hands: %q", got)
	}
	if !strings.Contains(how, "without sending") {
		t.Fatalf("the answer does not say it was left unsent: %q", how)
	}
}

// A card can refuse on its own account. A lent card is the case: the guest
// holds that terminal and was handed exactly one session.
func TestACardCanRefusePeerTyping(t *testing.T) {
	d := testDaemon(t)
	target, _, f := peerPair(t, d)
	if err := d.st.SetPeerTyping(target.ID, false); err != nil {
		t.Fatal(err)
	}
	off, err := d.st.Get(target.ID)
	if err != nil {
		t.Fatal(err)
	}

	if typed, _ := d.tellByTyping(off, "sg4/builder", "anything"); typed {
		t.Fatalf("typed into a card that refuses it: %q", f.written())
	}
}

// A card with no terminal atrium owns cannot be typed into at all, and that is
// the ordinary case for a window mode runner rather than an error.
func TestACardWithNoTerminalFallsBackToTheQueue(t *testing.T) {
	d := testDaemon(t)
	target := cardFor(t, d, "elsewhere")
	if typed, _ := d.tellByTyping(target, "sg4/builder", "anything"); typed {
		t.Fatal("claimed to type into a card with no runner")
	}
}

// ONLY THE OPERATOR'S KEYSTROKES COUNT. `Say` and this very feature also reach
// `Write`, and counting those would have atrium reading its own typing as the
// person being busy, so a second message would always defer behind the first.
func TestAtriumTypingDoesNotCountAsTheOperatorTyping(t *testing.T) {
	d := testDaemon(t)
	target, r, _ := peerPair(t, d)

	if typed, _ := d.tellByTyping(target, "sg4/builder", "first"); !typed {
		t.Fatal("the first message did not go")
	}
	if r.howBusy() == peerMidLine {
		t.Fatal("atrium's own typing left the terminal looking mid-line")
	}
	if typed, how := d.tellByTyping(target, "sg4/builder", "second"); !typed {
		t.Fatalf("the second message deferred behind the first: %q", how)
	}
}

// The window is what separates "watching" from "away", so it has to expire.
func TestTheTerminalIsFreeAgainOnceTheOperatorHasStopped(t *testing.T) {
	d := testDaemon(t)
	_, r, _ := peerPair(t, d)
	r.noteOperatorTyped([]byte("ls\r"))
	if r.howBusy() != peerWatching {
		t.Fatal("somebody who just typed is not being treated as present")
	}
	r.mu.Lock()
	r.lastTyped = time.Now().Add(-peerQuiet - time.Second)
	r.mu.Unlock()
	if r.howBusy() != peerFree {
		t.Fatal("the terminal never becomes free again")
	}
}
