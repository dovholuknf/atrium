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
// `CLAUDE.md` says atrium has no answer for this at all, and `docs/charon.md`
// ranks it first of six things worth taking, with the instruction: adopt the
// shape, do not port the code, and do not port the delivery mechanism.
//
// Everything needed already existed. The handle is `wire_name`, which is
// unique and qualified per machine. The transport is the agent listener that
// every hook already posts to. The delivery is the `message` table, drained by
// the permission hook and the Stop hook, so a peer message lands whether the
// target is working or idle.
//
// What is NOT adopted is Charon's injection. It types into a session as though
// the human had, which works there because its sessions are SDK turns with
// nobody at a keyboard. Atrium owns a real terminal that a person may be
// mid-command in, and `docs/supervision-design.md` settled that input is not
// fanned out. So a peer message is QUEUED, always, even when atrium owns the
// terminal and could type it. That is the one line in here most likely to be
// "simplified" by reusing handleMessage, which does type, and doing so would
// quietly reintroduce the thing this refuses.

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
// which is the failure `docs/charon.md` catalogues at length.
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
	Title  string `json:"title"`
	Status string `json:"status"`
	Runner string `json:"runner,omitempty"`
	// Worktree is where it is working, which is usually the fastest way for a
	// model to tell two sessions of the same repo apart.
	Worktree string `json:"worktree,omitempty"`
	// Why is what the operator said this card is for. The one field that says
	// what a session is DOING rather than where it is.
	Why string `json:"why,omitempty"`
	// Waiting is how many messages are queued for it and undelivered. A peer
	// with a pile already waiting is one to leave alone.
	Waiting int `json:"waiting,omitempty"`
}

// peers lists the sessions that can be addressed.
//
// Addressable means it has a name to address and has not ended. A dead card
// cannot be reached by anything, and telling a model otherwise wastes a turn
// and produces a message nobody will ever read.
func (d *Daemon) peers(exclude string) ([]Peer, error) {
	tasks, err := d.st.List()
	if err != nil {
		return nil, err
	}
	waiting, err := d.st.UndeliveredCounts()
	if err != nil {
		// Not fatal. The list is still worth having without the counts.
		waiting = map[string]int{}
	}
	exclude = d.st.Qualify(strings.TrimSpace(exclude))

	out := make([]Peer, 0, len(tasks))
	for _, t := range tasks {
		if t.WireName == "" || t.WireName == exclude {
			continue
		}
		switch t.Status {
		case store.StatusDead, store.StatusDone, store.StatusBacklog:
			continue
		}
		out = append(out, Peer{
			Handle: t.WireName, Title: t.DisplayTitle(), Status: t.Status,
			Runner: t.Runner, Worktree: t.Worktree, Why: t.Why,
			Waiting: waiting[t.ID],
		})
	}
	return out, nil
}

// handlePeers answers "who can I talk to".
//
// Discovery is first class rather than an afterthought. `docs/charon.md` makes
// listing mandatory before sending, and the reason is that a model which
// guesses a handle messages nobody and has no way to find that out.
func (d *Daemon) handlePeers(w http.ResponseWriter, r *http.Request) {
	list, err := d.peers(r.URL.Query().Get("me"))
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"peers": list})
}

// handleTell delivers a message from one session to another.
func (d *Daemon) handleTell(w http.ResponseWriter, r *http.Request) {
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

	switch {
	case from == "":
		writeJSONErr(w, http.StatusBadRequest, errString("say which session is sending"))
		return
	case to == "":
		writeJSONErr(w, http.StatusBadRequest, errString("say which session to tell"))
		return
	case text == "":
		writeJSONErr(w, http.StatusBadRequest, errString("there is nothing to say"))
		return
	case len(text) > maxPeerMessage:
		writeJSONErr(w, http.StatusRequestEntityTooLarge, fmt.Errorf(
			"that is %d characters, over the %d limit. write it to a file and say where it is",
			len(text), maxPeerMessage))
		return
	case from == to:
		// Not an error worth a stack trace, but worth refusing: a model
		// messaging itself is a loop with extra steps.
		writeJSONErr(w, http.StatusBadRequest, errString("a session cannot tell itself"))
		return
	}

	target, err := d.st.GetByWireName(to)
	if err != nil {
		// Discovery, rediscovered. A handle that does not resolve answers with
		// the list rather than with "no", because the next thing the sender
		// needs is the set of names that would have worked.
		list, _ := d.peers(from)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "no session called " + to,
			"peers": list,
		})
		return
	}
	switch target.Status {
	case store.StatusDead, store.StatusDone:
		writeJSONErr(w, http.StatusConflict, fmt.Errorf(
			"%s has ended, so nothing would read this", to))
		return
	}

	if !d.peerLimit.allow(from) {
		writeJSONErr(w, http.StatusTooManyRequests, fmt.Errorf(
			"%s has sent %d messages in the last minute, which is the limit",
			from, peerSendsPerMinute))
		return
	}

	// QUEUED, never typed. See the note at the top of this file: atrium owns a
	// terminal a person may be mid-command in, and a peer is not that person.
	// Reusing handleMessage here would type into the pty and reintroduce
	// exactly the injection docs/supervision-design.md refuses.
	if _, err := d.st.QueueFromPeer(target.ID, text, from); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	d.publishTask(target.ID)
	log.Printf("[atrium] %s told %s something (%d chars)", from, to, len(text))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"queued": true, "to": to,
		// What the sender needs to know next: this arrives when the target
		// makes its next tool call or ends its turn, not now.
		"note": "queued. it arrives on that session's next tool call or at the end of its turn.",
	})
}
