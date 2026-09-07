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

// MaxAsk bounds what a session may say it needs.
//
// A sentence or two. This is a question, not a transcript: an agent that needs
// to explain at length has something to say in its terminal, and the board is
// going to draw this on a card.
const MaxAsk = 500

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
	Blocked bool `json:"blocked,omitempty"`
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
	if len(ask) > MaxAsk {
		// Truncated rather than refused, for the reason `SetRecap` gives:
		// refusing makes an agent retry, and the retry is longer.
		ask = strings.TrimSpace(ask[:MaxAsk]) + "..."
	}

	task, err := d.taskFor(in.Agent, in.TaskID)
	if err != nil {
		log.Printf("[atrium] %s asked for help and has no card here", in.Agent)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"recorded":false}`))
		return
	}

	// WHY, on the card, which is the whole feature. `why` is what the board
	// already draws under a title, so an ask lands where somebody is already
	// looking rather than in a place they have to go and find.
	if err := d.st.SetWhy(task.ID, ask); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	if err := d.st.AppendEvent(task.ID, store.EventSubmitted, map[string]any{
		"by": "agent", "kind": "asked", "ask": ask, "blocked": in.Blocked,
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

	log.Printf("[atrium] %s asked for help: %s", task.DisplayTitle(), ask)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": true, "recorded": true, "task_id": task.ID, "waiting": moved,
	})
}
