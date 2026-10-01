package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Sessions that can address each other.
//
// `CLAUDE.md` says atrium has no answer for this at all, and `docs/rnd/charon.md`
// ranks it first of six things worth taking, with the instruction: adopt the
// shape, do not port the code, and do not port the delivery mechanism.
//
// Everything needed already existed. The handle is `wire_name`, which is
// unique and qualified per machine. The transport is the agent listener that
// every hook already posts to. The delivery is the `message` table, drained by
// the permission hook and the Stop hook, so a peer message lands whether the
// target is working or idle.
//
// Three things ride this bus now, and they are one mechanism: `tell` says
// something, `ask --peer` routes a question, and `answer` carries the reply
// back. The last two are in `help.go`, beside the verb they belong to, and use
// the resolution and the limit below so that a handle, a refusal and a flood
// mean the same thing whichever of the three is being attempted.
//
// A PEER MESSAGE IS TYPED INTO THE TERMINAL WHEN THE TERMINAL IS FREE, and
// queued when it is not. This reversed a refusal, so the reasoning on both
// sides is worth having.
//
// What it used to say: Charon's injection types into a session as though the
// human had, which works there because its sessions are SDK turns with nobody
// at a keyboard, while atrium owns a real terminal a person may be mid-command
// in. So a peer message was queued ALWAYS, even when atrium owned the terminal
// and could type it.
//
// The operator overruled that, in these words: "i want agents to be able to
// talk to one another without me here but i want to see when the agent does
// it. i consider the pty shared between me and all agents so PUT THE FUCKING
// TEXT INTO THE STREAM."
//
// Two things make that more than a preference:
//
//   - **The reframing.** The refusal rested on atrium owning the terminal FOR
//     the human. If the pty is shared between the human and the agents, the
//     problem stops being ownership and becomes contention. Ownership is
//     principled and contention is solvable.
//   - **The risk was already being taken everywhere else.** `Say` types text,
//     pauses, and presses Enter, and every other way of speaking to a session
//     goes through it: a message, a note, an action's prompt. This was not the
//     one safe path, it was the one path pretending the risk was unacceptable.
//
// So the objection is ANSWERED rather than dropped, by `runner.howBusy`. It
// reads a record rather than a guess: atrium is the only way the operator can
// type into a supervised session, so the daemon has seen every keystroke and
// knows whether a line is part written. A part written line is still never
// typed into. See `tellByTyping` for the three states and what each does about
// Enter.
//
// MID-TURN IS NOT A REASON TO WAIT, unless the sender asks. A message is typed
// the moment the line is empty and no dialog is open, whether or not the
// runner is working, because Claude Code queues a line typed mid-turn and
// reads it at its next step. `when: "done"` holds one message for the turn to
// end, and a runner set not to take input mid-turn holds all of them. See
// saywhen.go.
//
// `CLAUDE.md` still lists injecting prompts into a running session as out of
// scope and names this bus. It is a symlink into another repository and is not
// ours to edit, so it disagrees with this file until somebody there fixes it.

const (
	// maxPeerMessage bounds one peer message.
	//
	// Eight thousand characters is several paragraphs. A session that needs to
	// hand over more than that is handing over a document, and the way to do
	// that is to write a file and say where it is.
	maxPeerMessage = 8000

	// peerSendsPerMinute bounds how often one session may message others.
	//
	// A model in a loop is the failure this exists for: without a limit, one
	// confused session can fill every other session's queue faster than any of
	// them drains it. Twenty a minute is far more than deliberate use and far
	// less than a loop.
	peerSendsPerMinute = 20

	// peerWindow is the period that limit is measured over.
	peerWindow = time.Minute
)

// peerLimiter counts recent sends per sender.
//
// In memory, and it dies with the daemon. That is right: the thing being
// bounded is a runaway session, and a session does not survive the daemon
// either. Charon's equivalent leaks across restarts because it was persisted,
// which is the failure `docs/rnd/charon.md` catalogs at length.
type peerLimiter struct {
	mu   sync.Mutex
	sent map[string][]time.Time
	now  func() time.Time
}

func newPeerLimiter() *peerLimiter {
	return &peerLimiter{sent: map[string][]time.Time{}, now: time.Now}
}

// allow records a send and reports whether it is within the limit.
func (l *peerLimiter) allow(from string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := l.now().Add(-peerWindow)
	keep := l.sent[from][:0]
	for _, t := range l.sent[from] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	l.sent[from] = keep
	if len(keep) >= peerSendsPerMinute {
		return false
	}
	l.sent[from] = append(l.sent[from], l.now())
	return true
}

// Peer is one session another session can address.
type Peer struct {
	// Handle is what to address it as, which is the wire name.
	Handle string `json:"handle"`
	// Alias is the short name the operator gave it, accepted anywhere Handle
	// is. Empty when none is set.
	Alias  string `json:"alias,omitempty"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Runner string `json:"runner,omitempty"`
	// Worktree is where it is working, which is usually the fastest way for a
	// model to tell two sessions of the same repo apart.
	Worktree string `json:"worktree,omitempty"`
	// Why is what the operator said this card is for. The one field that says
	// what a session is DOING rather than where it is.
	Why string `json:"why,omitempty"`
	// Parked is true when the card is idle with no process. A say to it is refused
	// unless it asks to wake it. See park.go.
	Parked bool `json:"parked,omitempty"`
	// Waiting is how many messages are queued for it and undelivered. A peer
	// with a pile already waiting is one to leave alone.
	Waiting int `json:"waiting,omitempty"`

	// What this card wants from a human, and how to read it. See fleet.go for
	// the buckets and why they are in that order.
	//
	// Want is one of the Want constants. Note is the same thing in a sentence,
	// which is what a list prints.
	Want string `json:"want,omitempty"`
	Note string `json:"note,omitempty"`
	// Ask is the question a session asked, in its own words, and Blocked is
	// whether it stopped to wait for the answer. Empty when nobody asked
	// anything, which is not the same as a `Why` the operator typed.
	Ask     string `json:"ask,omitempty"`
	Blocked bool   `json:"blocked,omitempty"`
	// AskPeer is who it asked, when the question was routed to another session
	// rather than left on the card for a human. Empty otherwise.
	//
	// Here because the most useful thing in a list of peers is the one that is
	// stopped waiting on somebody. A session that can answer it can see that
	// from the list rather than having to be told.
	AskPeer string `json:"ask_peer,omitempty"`
	// Recap is what a finished session said it did, and empty on a card that
	// finished without saying.
	Recap string `json:"recap,omitempty"`
	// Since is when this card started wanting what it wants, and Seconds is
	// that as an age. Ordering within a bucket is oldest first.
	Since   time.Time `json:"since,omitempty"`
	Seconds int64     `json:"seconds,omitempty"`
}

// peers lists the sessions that can be addressed.
//
// Addressable means it has a name to address and has not ended. A dead card
// cannot be reached by anything, and telling a model otherwise wastes a turn
// and produces a message nobody will ever read.
//
// Ranked by what each one wants from a human, which `roster` decides. That
// costs nothing here and is worth having: a peer that is blocked on a question
// is one to leave alone, and a peer that is quietly working is one to ask.
func (d *Daemon) peers(exclude string) ([]Peer, error) {
	return d.roster(exclude, false)
}

// handlePeers answers "who can I talk to", and with `fleet=1`, "which of these
// want me".
//
// One endpoint and two questions, because they are the same rows read for
// different reasons and the second is the first plus the sessions that have
// finished. Discovery is first class rather than an afterthought:
// `docs/rnd/charon.md` makes listing mandatory before sending, and the reason is
// that a model which guesses a handle messages nobody and has no way to find
// that out.
func (d *Daemon) handlePeers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	fleet := q.Get("fleet") != "" && q.Get("fleet") != "0"
	// How far back finished work counts. An unreadable value falls back to the
	// default rather than refusing: this list is worth having with the wrong
	// window on it and worth nothing as an error.
	within := defaultFinishedWithin
	if dur, err := time.ParseDuration(q.Get("since")); err == nil && dur > 0 {
		within = dur
	}
	list, err := d.rosterWithin(q.Get("me"), fleet, within)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"peers": list})
}

// checkPeerText applies the bounds one session's words have to fit in.
//
// Its own function because three endpoints now put text on the peer bus, and
// a limit that only two of them enforce is not a limit.
func checkPeerText(w http.ResponseWriter, text, nothing string) bool {
	switch {
	case text == "":
		writeJSONErr(w, http.StatusBadRequest, errString(nothing))
		return false
	case len(text) > maxPeerMessage:
		writeJSONErr(w, http.StatusRequestEntityTooLarge, fmt.Errorf(
			"that is %d characters, over the %d limit. write it to a file and say where it is",
			len(text), maxPeerMessage))
		return false
	}
	return true
}

// resolvePeer answers "can this session reach that one", and says why not.
//
// Shared by everything on the peer bus: telling, routing an ask, and answering
// one. It returns nil having ALREADY written the refusal, so a caller adds
// nothing and cannot get the shape of one wrong. The rate limit is counted
// here too, at the point every path has passed its own validation and is about
// to send.
//
// `verb` is what the caller is trying to do, so the refusal reads as the thing
// that was refused rather than as a generic message failure.
func (d *Daemon) resolvePeer(w http.ResponseWriter, from, to, verb string) *store.Task {
	return d.resolvePeerSay(w, from, to, verb, "", "", false)
}

// resolvePeerSay is resolvePeer for a caller with words to record. A miss on a
// tell is written to the say record; the other verbs write no row (text is "").
func (d *Daemon) resolvePeerSay(w http.ResponseWriter, from, to, verb, text, when string, reply bool) *store.Task {
	return d.resolvePeerSayWake(w, from, to, verb, text, when, reply, false)
}

// resolvePeerSayWake is resolvePeerSay for a sender that may ask a parked card to
// be resumed. The target returned is the row AS FOUND, still marked parked when
// this woke it, which is how the caller knows not to type.
func (d *Daemon) resolvePeerSayWake(w http.ResponseWriter, from, to, verb, text, when string, reply, wake bool) *store.Task {
	switch {
	case from == "":
		writeJSONErr(w, http.StatusBadRequest, errString("say which session is sending"))
		return nil
	case to == "":
		writeJSONErr(w, http.StatusBadRequest, errString("say which session to "+verb))
		return nil
	case from == to:
		// Not an error worth a stack trace, but worth refusing: a model
		// messaging itself is a loop with extra steps.
		writeJSONErr(w, http.StatusBadRequest, errString("a session cannot "+verb+" itself"))
		return nil
	}

	target, err := d.st.GetByWireName(to)
	if err != nil {
		// An alias, `sa89` or `@dotfiles`, is what an operator or a prompt
		// mentions a card by. Tried after the handle, which always wins.
		target, err = d.st.GetByAlias(to)
	}
	if err != nil {
		// Discovery, rediscovered. A handle that does not resolve answers with
		// the list rather than with "no", because the next thing the sender
		// needs is the set of names that would have worked.
		d.writeMiss(w, from, to, verb, text, when, reply)
		return nil
	}
	// The same self-check as above, for a session that named itself by alias.
	if target.WireName == d.st.Qualify(from) {
		writeJSONErr(w, http.StatusBadRequest, errString("a session cannot "+verb+" itself"))
		return nil
	}
	// The same test the message endpoint uses, so `atrium tell` and `atrium_say`
	// agree: a done card whose terminal is still live is somebody to talk to.
	gate := d.sayGate(target)
	if gate == sayGone {
		writeJSONErr(w, http.StatusConflict, fmt.Errorf(
			"%s has ended, so nothing would read this", to))
		return nil
	}
	if gate == sayParked && !wake && d.fromFamily(from, target) {
		wake = true
	}
	if gate == sayParked && !wake {
		// Nothing queued: waking is a cold turn and the sender should choose it.
		if text != "" {
			rec := sayRecordFor(from, target, sayTrace{}, false, verb, when, reply)
			rec.State, rec.Note, rec.ReplyWant = store.SayRefused, "parked", false
			d.recordSay(rec, text)
		}
		writeJSONCode(w, http.StatusOK, map[string]any{
			"queued": false, "typed": false, "delivered": "parked", "reachable": "parked",
			"to": to, "note": parkedNote(target),
		})
		return nil
	}

	if !d.peerLimit.allow(from) {
		writeJSONErr(w, http.StatusTooManyRequests, fmt.Errorf(
			"%s has sent %d messages in the last minute, which is the limit",
			from, peerSendsPerMinute))
		return nil
	}
	if gate == sayParked {
		if err := d.unpark(target.ID, wakeVia(from)); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err)
			return nil
		}
	}
	return target
}

// queuedNote is what a sender needs to know next: this did not land now, and
// the first of three things to happen delivers it.
const queuedNote = "queued. it is typed in when that session's terminal is free, or arrives on its " +
	"next tool call or at the end of its turn, whichever comes first."

// handleTell delivers a message from one session to another.
func (d *Daemon) handleTell(w http.ResponseWriter, r *http.Request) {
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
		Text string `json:"text"`
		// When is `immediate` (the default) or `done`. See saywhen.go.
		When string `json:"when"`
		// Reply is true when the sender needs an answer, not just a delivery.
		Reply bool `json:"reply"`
		// Wake resumes a parked target so this can be delivered. See park.go.
		Wake bool `json:"wake"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	when, err := parseWhen(in.When)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}

	// ANOTHER ROOM, `name@room`, relayed through the hub. See relay.go.
	if name, room, err := SplitAddress(in.To); err == nil {
		if other := d.otherRoom(room); other != "" {
			code, body := d.sayAcross(r.Context(), strings.TrimSpace(in.From), name, other, in.Text, in.When, in.Reply)
			if code < 400 {
				// The two words `atrium tell` reads, beside the rest.
				body["typed"] = body["delivered"] == "terminal"
				body["queued"] = body["delivered"] != "terminal"
			}
			writeJSONCode(w, code, body)
			return
		}
		in.To = name
	}

	from := d.st.Qualify(strings.TrimSpace(in.From))
	to := d.st.Qualify(strings.TrimSpace(in.To))
	text := strings.TrimSpace(in.Text)

	if !checkPeerText(w, text, "there is nothing to say") {
		return
	}
	// A BARE NAME THAT IS NOT ON THIS ROOM, looked for among the cards tagged
	// atrium:everywhere on the others. Only a name that is nobody here.
	if d.localTarget(in.To) == nil && from != to {
		raw := strings.TrimSpace(in.From)
		done, note := d.sayEverywhere(w, r.Context(), raw, strings.TrimSpace(in.To), text, in.When, in.Reply,
			func(code int, body map[string]any) {
				if code < 400 {
					body["typed"] = body["delivered"] == "terminal"
					body["queued"] = body["delivered"] != "terminal"
				}
			})
		if done {
			return
		}
		if note != "" {
			d.writeMissNote(w, from, to, "tell", text, in.When, in.Reply, note)
			return
		}
	}
	target := d.resolvePeerSayWake(w, from, to, "tell", text, in.When, in.Reply, in.Wake)
	if target == nil {
		return
	}

	// TYPED WHEN THE TERMINAL IS FREE, AND QUEUED WHEN IT IS NOT.
	//
	// This refused to type, at length and on principle: atrium owned the
	// terminal for the human, and a peer was not that human. The operator has
	// overruled it, and the reframing is what makes that more than a
	// preference. He considers the pty SHARED between himself and the agents,
	// which turns the question from ownership into contention. Contention is
	// solvable and ownership is not, so the mechanism answers the old
	// objection rather than dropping it.
	//
	// The old comment was also the only caller pretending the risk was
	// unacceptable. `Say` types text, pauses, and presses Enter, and every
	// other way of speaking to a session already goes through it: a message, a
	// note, an action's prompt. Atrium types into a pty somebody may be
	// mid-command in several times a day. The peer bus was not being asked to
	// take a new risk.
	//
	// What answers the objection is `runner.howBusy`, which reads a record
	// rather than a guess. Atrium is the only way the operator can type into a
	// supervised session, so the daemon has already seen every keystroke and
	// knows whether a line is part written.
	//
	// THE QUEUE STAYS. It is the fallback for everything not typed, and a
	// message that is typed is written to the timeline instead so the traffic
	// is still auditable. Both, and the agent would receive it twice.
	waitTurn := d.waitsForTurn(target.ID, when)
	typed, msgID, err := d.deliverPeerWhenID(target, from, text, waitTurn)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	d.recordTell(from, target, text, in.When, in.Reply, typed, msgID)
	d.peerSaid(from, target, text)
	log.Printf("[atrium] %s told %s something (%d chars, typed %v, waits for the turn %v)",
		from, to, len(text), typed, waitTurn)

	w.Header().Set("Content-Type", "application/json")
	if typed {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"typed": true, "to": to, "note": typedNote,
		})
		return
	}
	// The same warning `handleMessage` gives when the card has no way to
	// drain its queue. See reachability in a2a.go.
	note := queuedNote
	if waitTurn {
		note = turnQueuedNote
	}
	reach, why := d.reachability(target)
	if why == "" && waitTurn {
		why = d.turnReachWarning(target)
	}
	if why != "" {
		note = why
	}
	if d.holdingMessages(target.ID) {
		note = newContextHoldNote
	} else if d.deployHeld(target.ID) {
		note = d.deployHoldNote()
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"queued": true, "to": to, "note": note, "reachable": reach, "when": whenWord(waitTurn),
	})
}

// turnQueuedNote is queuedNote for a message that waits for the turn to end.
const turnQueuedNote = "queued until that session's turn ends, then typed in when its terminal is " +
	"free, or carried by the Stop hook."

// whenWord is what a sender is told its message will wait for.
func whenWord(waitTurn bool) string {
	if waitTurn {
		return WhenDone
	}
	return WhenImmediate
}

// typedNote is what a sender is told when its words went straight in.
const typedNote = "typed into the terminal and sent."

// deliverPeer is the one way a peer's words reach another session, shared by
// `tell`, a routed `ask`, and `answer` so the three cannot drift apart.
//
// Typed when the terminal's gate is open. Otherwise queued for the hooks AND
// held for the on-screen retry, so it lands the moment the operator's line
// clears rather than waiting for the target's next tool call.
//
// Immediate, as far as the target's runner allows. See saywhen.go.
func (d *Daemon) deliverPeer(target *store.Task, from, text string) (bool, error) {
	return d.deliverPeerWhen(target, from, text, d.waitsForTurn(target.ID, WhenImmediate))
}

// deliverPeerWhen is deliverPeer with the turn rule already resolved.
func (d *Daemon) deliverPeerWhen(target *store.Task, from, text string, waitTurn bool) (bool, error) {
	typed, _, err := d.deliverPeerWhenID(target, from, text, waitTurn)
	return typed, err
}

// deliverPeerWhenID is deliverPeerWhen that also says which queue row carries
// the words when they were not typed, so a say can be recorded against it.
func (d *Daemon) deliverPeerWhenID(target *store.Task, from, text string, waitTurn bool) (bool, string, error) {
	// A target still marked parked here was woken by this very say: it is queued
	// and carried by the ordinary path, never typed into a session that has only
	// just started.
	if isParked(target) {
		// fall through to the queue
	} else if typed, _ := d.tellByTyping(target, from, text, waitTurn); typed {
		d.publishTask(target.ID)
		return true, "", nil
	}
	var (
		m   *store.Message
		err error
	)
	if waitTurn {
		m, err = d.st.QueueAfterTurn(target.ID, text, from)
	} else {
		m, err = d.st.QueueFromPeer(target.ID, text, from)
	}
	if err != nil {
		return false, "", err
	}
	d.publishTask(target.ID)
	d.deferPeerInjection(target.ID, m.ID, from, text, waitTurn)
	return false, m.ID, nil
}

// peerBanner marks a typed message as coming from another session.
//
// UNMISTAKABLY NOT THE OPERATOR, which is the condition attached to putting
// this in the stream at all. Grey and named, in the sentinel style the rest of
// the board uses for anything atrium says on a terminal it does not own the
// content of.
//
// NO CARRIAGE RETURN AND NO NEWLINE, which is a rule and not a detail. This
// banner is written into the operator's prompt, and a carriage return in it is
// an Enter: it submits whatever the operator had already typed. It used to open
// and close with `\r\n`, so a peer message split the operator's part written
// line in two and sent the first half. The label leads with something that
// cannot press Enter for him, and the body follows it on the same line.
func peerBanner(from string) string {
	return atriumLabel(from + " says:")
}

// atriumLabel is the grey `[atrium] ...` label atrium types ahead of text that
// is not the operator's. peerBanner is one. The after-restart wake is the other.
// The rule peerBanner gives holds for every label: no carriage return in `what`.
func atriumLabel(what string) string {
	return "\x1b[38;5;244m[atrium] " + what + " \x1b[0m"
}

// tellByTyping puts a peer's message into the terminal when the terminal is
// free enough to take it, and answers whether it did.
//
// THE THREE STATES ARE THE DESIGN, and the difference between them is whether
// Enter is pressed:
//
//   - Nobody attached, or attached and idle. Typed and submitted. This is the
//     case the feature exists for, an agent talking to an agent while nobody
//     is at the keyboard, and a message that does not submit does nothing.
//   - Attached and watching. Typed, attributed, and NOT submitted. The pty is
//     shared so the text goes in, and pressing Enter under somebody's hands is
//     a different act from putting words in front of them. They send it, edit
//     it, or clear the line.
//   - A part written line. Never typed. This is what the old refusal was
//     protecting and it stays protected, because there is no way to insert
//     into a line somebody is halfway through without wrecking it.
func (d *Daemon) tellByTyping(target *store.Task, from, text string, waitTurn bool) (bool, string) {
	// A card can refuse on its own account. A lent card is the case this was
	// built for: the guest holds that terminal and was handed exactly one
	// session, so another session's words have no business appearing in it.
	if !target.PeerTyping {
		return false, ""
	}
	// Held for a new-context cycle or a room deploy: queued, delivered after the
	// wake prompt.
	if d.holdingFrom(target.ID, from) {
		return false, ""
	}
	run := d.sup.get(target.ID)
	if run == nil {
		return false, ""
	}
	// A FOURTH STATE, and it refuses like the part written line does.
	//
	// A dialog the runner put up itself is on that screen, and a submitted
	// peer message would end in an Enter that answers it with whatever option
	// was highlighted. Refusing here sends it back to the queue, which is what
	// the peer bus does by default anyway, so nothing is lost but the immediacy.
	if d.act.dialogOpen(target.ID) {
		return false, ""
	}
	// A FIFTH, and it waits rather than refuses: a message that asked to wait
	// for the turn, while the runner is mid-turn. See saywhen.go.
	if d.turnHolds(target.ID, waitTurn) {
		return false, ""
	}
	// Bracketed paste when supported, so a long report stays together even if
	// the PTY splits the write. See SayPasted and B2-47. The banner stays
	// outside the markers so its grey label renders rather than arriving as
	// literal paste text.
	body := text
	if d.bracketedPasteFor(target.ID, false) {
		body = "\x1b[200~" + text + "\x1b[201~"
	}
	// injectPeer types and submits ONLY when the gate is open right now: an empty
	// line and peerGateIdle of quiet. It never leaves unsent text in the prompt
	// and never blocks the operator's keystrokes. A closed gate writes nothing,
	// and the caller defers the message onto the backoff. See injectPeer and
	// pendingInjector.
	wrote, err := run.injectPeer(peerBanner(from), body)
	if err != nil || !wrote {
		return false, ""
	}
	d.notePeerTyped(target.ID, from, text, "typed and sent")
	return true, typedNote
}

// notePeerTyped records a typed message on the timeline.
//
// The queue is what makes peer traffic auditable and a typed message never
// reaches it, so this is the record instead. Written after the bytes are in
// the terminal, because a message that failed to type is not one that happened.
func (d *Daemon) notePeerTyped(taskID, from, text, how string) {
	if err := d.st.AppendEvent(taskID, store.EventPrompted, map[string]any{
		"text": text, "via": "terminal", "from_peer": from, "how": how,
	}); err != nil {
		log.Printf("[atrium] typed a peer message into %s but could not record it: %v", taskID, err)
	}
}
