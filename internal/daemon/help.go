package daemon

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// An agent saying it is stuck, and what it needs.
//
// The other half of `atrium finish`, and the same shape of hole. `finish` gave
// a session a way to say its work was over. This gives it a way to say the
// opposite: that it has stopped, on purpose, and cannot go on without
// something.
//
// WHY THIS IS NOT ALREADY COVERED. A stuck session already lands in
// `needs-input`, but it gets there by INFERENCE: a hook fires at the end of a
// turn and atrium concludes nobody is typing. That answers "this session
// stopped" and never "why", so the board can say a card is waiting and cannot
// say what it is waiting for. The operator opens the terminal and reads back
// through the scrollback to find out, which is the thing the board exists to
// save them from.
//
// A command rather than a tool, for the reason `finish.go` gives: it is the one
// channel every runner already has, and it has to work for a bare shell as well
// as for something with an MCP surface.
//
// SAME POSTURE AS EVERY OTHER AGENT-FACING ENDPOINT. It answers, it never fails
// a session, and an agent atrium has never heard of gets `ok` and nothing
// recorded.
//
// WHO THE ASK REACHES. By default a human, on the card. With `peer` it is
// routed to another session instead, over the same bus `atrium tell` uses, and
// that session answers with `atrium answer`. Both directions are QUEUED and
// delivered by a hook. Nothing here types into anybody's terminal, and the
// reason is spelled out at the top of `peers.go`: atrium owns a terminal a
// person may be mid-command in, and a peer is not that person.

// MaxAsk bounds what a session may say it needs.
//
// Held in the store beside the column it bounds. Kept here as a name too,
// because this is where an agent-facing limit is read from.
const MaxAsk = store.MaxAsk

// HelpRequest is a session saying it cannot go on.
type HelpRequest struct {
	// Agent is the wire name, the same one every hook uses.
	Agent string `json:"agent"`
	// TaskID is used when the session knows which card it belongs to.
	TaskID string `json:"task_id,omitempty"`
	// Ask is what it needs, in its own words. THE WHOLE POINT: without it this
	// is `needs-input` with extra steps.
	Ask string `json:"ask"`
	// Blocked is whether it has actually stopped, or is carrying on and would
	// like an answer when somebody has one.
	//
	// Two different things and the board treats them differently. A session
	// that has stopped is worth interrupting somebody for. One that is still
	// working and has a question is not.
	//
	// FALSE IS THE DEFAULT AND STOPPING IS THE DEFAULT, which reads backwards
	// until you look at the flag: `atrium ask` stops, and `--continue` is how
	// a session says it is carrying on. The field is named for the state and
	// the flag is named for the decision, on purpose. A caller that omits this
	// is saying nothing about what it is doing, and the safe reading of
	// nothing is that the session is waiting.
	Blocked bool `json:"blocked,omitempty"`
	// Peer is the handle of another session to route this to, or empty to put
	// it on the card for a human.
	//
	// A handle that does not resolve REFUSES rather than falling back to the
	// board. Quietly asking a human instead would be atrium deciding who
	// answers, and the card would then claim a peer was on the hook when
	// nobody was. The refusal carries the handles that would have worked, the
	// same way `tell` does.
	Peer string `json:"peer,omitempty"`
}

// handleHelp answers a session asking for help.
func (d *Daemon) handleHelp(w http.ResponseWriter, r *http.Request) {
	var in HelpRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(in.Agent) == "" && in.TaskID == "" {
		writeJSONErr(w, http.StatusBadRequest,
			errString("say which session this is, with agent or task_id"))
		return
	}
	ask := strings.TrimSpace(in.Ask)
	if ask == "" {
		writeJSONErr(w, http.StatusBadRequest,
			errString("say what you need. an empty ask is what needs-input already means"))
		return
	}

	task, err := d.taskFor(in.Agent, in.TaskID)
	if err != nil {
		log.Printf("[atrium] %s asked for help and has no card here", in.Agent)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"recorded":false}`))
		return
	}

	// ROUTED FIRST, RECORDED SECOND.
	//
	// A card that says it asked a peer has to be true, so the routing happens
	// before anything is written. An unresolvable handle refuses here, with
	// nothing on the card and no status moved, and the session is free to try
	// a real handle or to ask a human instead.
	peer := strings.TrimSpace(in.Peer)
	if peer != "" {
		from := d.st.Qualify(strings.TrimSpace(in.Agent))
		if from == "" {
			from = task.WireName
		}
		to := d.st.Qualify(peer)
		target := d.resolvePeer(w, from, to, "ask")
		if target == nil {
			return
		}
		if _, err := d.st.QueueFromPeer(target.ID, askEnvelope(from, ask, in.Blocked), from); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err)
			return
		}
		d.publishTask(target.ID)
		peer = to
		log.Printf("[atrium] %s asked %s: %s", from, to, ask)
	}

	// WHY IS NOT WHERE THIS GOES ANY MORE. An ask used to land in `why`, the
	// field the operator writes once and reads in a week, so a question
	// destroyed the standing answer to "what was I even doing" and the board
	// drew both in the same line. See `store/ask.go`.
	//
	// AND IT IS APPENDED, NOT WRITTEN OVER. The columns that replaced `why`
	// held exactly one question, so a second ask destroyed the first with
	// nothing recording it had been asked. A row per question, and the oldest
	// outstanding one is what the card draws.
	if _, err := d.st.AddAsk(task.ID, ask, peer); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	if err := d.st.AppendEvent(task.ID, store.EventSubmitted, map[string]any{
		"by": "agent", "kind": "asked", "ask": ask, "blocked": in.Blocked, "peer": peer,
	}); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}

	// ONLY A BLOCKED SESSION MOVES THE CARD.
	//
	// A session that is still working and has a question should not be filed
	// as waiting: the status column is a bucket of human attention, and
	// putting a working session in it makes the count that drives the alerting
	// lie. The ask is recorded either way and the board can show it either way.
	//
	// A card put down by hand stays put, the same rule `finish` follows.
	//
	// A card that asked a PEER still moves, because it has genuinely stopped
	// and a board that hides stopped sessions is worse than one that shows a
	// session somebody else owes an answer to. What changes is what it says:
	// the card names the peer, so it does not read as a question for you, and
	// it is visible when that peer never answers.
	//
	// AND IT RECORDS THAT IT IS A QUESTION. `SetStatus` writes an empty
	// waiting reason, which reads as "a turn ended", so an agent that stopped
	// on purpose and said what it needed landed in the same bucket as one that
	// simply ran out of things to do. That is the exact distinction
	// `WaitingAsked` exists for, and the asking-tool path already writes it.
	moved := false
	if in.Blocked && task.Status != store.StatusShelved &&
		task.Status != store.StatusNeedsInput {
		if err := d.st.SetStatusBecause(task.ID, store.StatusNeedsInput,
			store.WaitingAsked); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err)
			return
		}
		moved = true
	}
	if in.Blocked {
		d.act.forget(task.ID)
	}
	d.publishTask(task.ID)

	if peer == "" {
		log.Printf("[atrium] %s asked for help: %s", task.DisplayTitle(), ask)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": true, "recorded": true, "task_id": task.ID, "waiting": moved,
		"peer": peer,
	})
}

// askEnvelope is how a routed question reaches the session it was routed to.
//
// Three things the receiver cannot work out for itself, and every one of them
// changes what it should do:
//
//   - That this is a QUESTION, not a handover. The banner already says a peer
//     sent it, which is not the same as saying somebody is stopped on it.
//   - Whether the asker is blocked. An answer that arrives tomorrow is fine
//     for a session that carried on and useless to one that has stopped.
//   - How to answer, by name. Without the command a well-behaved model writes
//     a good answer into its own transcript, where nobody will ever read it,
//     and the loop this feature exists to close stays open.
func askEnvelope(from, ask string, blocked bool) string {
	state := "It is carrying on in the meantime, so an answer when you have one is enough."
	if blocked {
		state = "It has STOPPED and is waiting on this."
	}
	return from + " is asking you a question:\n\n" + ask + "\n\n" +
		state + " Answer with:\n\n" +
		"    atrium answer " + from + " \"your answer\"\n\n" +
		"If you cannot answer it, say so the same way rather than leaving it waiting."
}

// handleAnswer delivers one session's answer to another's question.
//
// The return leg, and it lands the same way the ask did: queued on the peer
// bus, carried to the asker by whichever hook fires first. That is what makes
// this a pair rather than two features. It is a separate endpoint from `tell`
// because it does one thing `tell` must not: it takes the question OFF the
// asker's card, which is the only signal anybody has that the ask is settled.
//
// It does not move the asker's status. Delivering a message already does that:
// a queued message resumes a waiting card when the hook hands it over, and
// moving it here would put the card in `running` while the session is still
// sitting at its prompt.
func (d *Daemon) handleAnswer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
		Text string `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}

	from := d.st.Qualify(strings.TrimSpace(in.From))
	to := d.st.Qualify(strings.TrimSpace(in.To))
	text := strings.TrimSpace(in.Text)

	if !checkPeerText(w, text, "there is nothing to answer with") {
		return
	}
	target := d.resolvePeer(w, from, to, "answer")
	if target == nil {
		return
	}

	// The question, quoted back. A session that asked an hour ago may have
	// compacted its context since, and an answer to a question it no longer
	// remembers asking is a puzzle rather than an answer.
	//
	// ITS OWN QUESTION, NOT WHATEVER THE CARD HAPPENS TO BE DRAWING. A card
	// can be waiting on this peer for one thing and on a human for another,
	// and quoting back the human's question over a peer's answer tells the
	// asker its human question was answered when it was not.
	asked := d.oldestAskTo(target.ID, from)
	body := text
	if asked != "" {
		body = "You asked: " + asked + "\n\n" + text
	}
	if _, err := d.st.QueueFromPeer(target.ID, body, from); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}

	// ONLY THE QUESTIONS THIS PEER WAS ASKED. `ask_peer` is per question, so a
	// peer can only have settled the ones addressed to it. Clearing the card
	// wholesale on the strength of one reply would take a question meant for a
	// human off the board, which is the same silent disappearance the ask
	// table exists to stop.
	settled, err := d.st.AnswerAsksFrom(target.ID, from, from)
	if err != nil {
		log.Printf("[atrium] %s answered %s but the card could not be updated: %v", from, to, err)
	}
	answered := len(settled) > 0
	if answered {
		d.askSettled(target.ID, from, settled)
	}
	d.publishTask(target.ID)
	log.Printf("[atrium] %s answered %s (%d chars)", from, to, len(text))

	note := queuedNote
	if !answered {
		// Said rather than refused. Answering something asked in a terminal,
		// or twice over, is not a mistake worth failing, but a session that
		// thinks it settled a card should not be left believing it.
		note = queuedNote + " " + to + " had no question outstanding, so nothing was cleared."
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"queued": true, "to": to, "answered": answered, "note": note,
	})
}

// askAnswered takes every outstanding question off a card.
//
// THE BROAD DOOR, and it is the right one for the two callers it has. The
// operator saying something to the card, and the session declaring its work
// over, are both the end of every question at once: neither is addressed to a
// particular one, and neither leaves anybody owing an answer. A peer replying
// is the opposite, and goes through `AnswerAsksFrom` above.
//
// Typing into the terminal is invisible to atrium and always has been, so an
// ask answered that way stays on the card until the session finishes, is
// spoken to through atrium, or asks something else.
//
// Best effort. A message that has already been queued is a message that
// arrived, and reporting a failure for the bookkeeping after it would be
// reporting a failure for something that happened.
func (d *Daemon) askAnswered(taskID, by string) {
	settled, err := d.st.AnswerAllAsks(taskID, by)
	if err != nil {
		log.Printf("[atrium] answered %s but could not clear the ask: %v", taskID, err)
		return
	}
	d.askSettled(taskID, by, settled)
}

// askSettled records questions that have just been answered.
//
// One event per question rather than one per act, because the event log is
// where you go to find out what happened to a question, and a single line
// saying three were settled cannot tell you which three.
// handleDismissAsks takes every outstanding question off a card and tells the
// session nothing.
//
// THE ONE WAY TO ANSWER A QUESTION THAT DOES NOT INVOLVE ANSWERING IT, and it
// exists because the three that came first all deliver text. Saying something
// to a card types it into the terminal, sending a note does the same, and a
// session finishing is the session's own decision. None of them is available
// to somebody who already answered by TYPING IN THE TERMINAL, which atrium
// cannot see and which is the way anybody sitting in front of a session
// actually replies.
//
// So the question stayed open. Nothing expires an ask, and `wasAsked` on the
// board falls back to the ask field when no waiting reason is set, so every
// later turn-end on that card announced itself as a question. One card carried
// two questions for two days that way, and the operator had answered both
// within a minute of being asked.
//
// Recorded as `dismissed` rather than answered, since that is what happened
// and the event log is the only place it can be said.
func (d *Daemon) handleDismissAsks(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	settled, err := d.st.AnswerAllAsks(taskID, askDismissed)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	d.askSettled(taskID, askDismissed, settled)
	d.publishTask(taskID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"dismissed": len(settled)})
}

// What the event log records for a question nobody answered through atrium.
const askDismissed = "dismissed"

func (d *Daemon) askSettled(taskID, by string, settled []*store.Ask) {
	for _, a := range settled {
		if err := d.st.AppendEvent(taskID, store.EventPrompted, map[string]any{
			"kind": "answered", "by": by, "ask": a.Text, "peer": a.Peer,
		}); err != nil {
			log.Printf("[atrium] answered %s but could not record it: %v", taskID, err)
			return
		}
	}
}

// oldestAskTo is the question this card has been waiting on ONE peer for the
// longest, or empty when that peer owes it nothing.
func (d *Daemon) oldestAskTo(taskID, peer string) string {
	open, err := d.st.OpenAsks(taskID)
	if err != nil {
		return ""
	}
	for _, a := range open {
		if a.Peer == peer {
			return a.Text
		}
	}
	return ""
}
