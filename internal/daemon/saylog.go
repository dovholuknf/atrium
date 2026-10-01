package daemon

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// A say's lifecycle on record. See docs/runtime/say-lifecycle-design.md.
//
// Every write here is best effort and logs its failure: a say is not held back
// because its own bookkeeping failed.

// sayTrace is what `handleSay` knows and `handleMessage` does not: what the
// sender typed and how it resolved. On the CONTEXT of the inner request, not a
// header, because that request never crosses a wire and a client cannot forge one.
type sayTrace struct{ to, via string }

type sayTraceKey struct{}

func withSayTrace(ctx context.Context, to, via string) context.Context {
	return context.WithValue(ctx, sayTraceKey{}, sayTrace{to: to, via: via})
}

func sayTraceFrom(ctx context.Context) (sayTrace, bool) {
	v, ok := ctx.Value(sayTraceKey{}).(sayTrace)
	return v, ok
}

// senderTask is the sender's card when it is one here.
func (d *Daemon) senderTask(from string) string {
	if from == "" {
		return ""
	}
	if t, err := d.st.GetByWireName(d.st.Qualify(from)); err == nil {
		return t.ID
	}
	return ""
}

// recordSay writes one row and returns its id, or "" when it could not.
func (d *Daemon) recordSay(v store.Say, text string) string {
	if v.FromWire == "" {
		return ""
	}
	if v.FromTask == "" {
		v.FromTask = d.senderTask(v.FromWire)
	}
	id, err := d.st.RecordSay(v, text)
	if err != nil {
		log.Printf("[atrium] could not record a say from %s to %s: %v", v.FromWire, v.ToInput, err)
		return ""
	}
	return id
}

// sayRecordFor builds the row for a say that reached a resolved local card.
func sayRecordFor(from string, target *store.Task, tr sayTrace, traced bool, door, when string, reply bool) store.Say {
	v := store.Say{FromWire: from, ToTask: target.ID, ToWire: target.WireName, Door: door, When: when,
		ReplyWant: reply, ToInput: target.WireName, Via: "handle"}
	if traced {
		v.ToInput, v.Via = tr.to, tr.via
	}
	return v
}

// saySettled is a reply: `from` said something to `target`, which settles every
// reply target asked `from` for, and only those.
func (d *Daemon) saySettled(from string, target *store.Task) {
	replier := d.senderTask(from)
	if replier == "" || target == nil {
		return
	}
	if _, err := d.st.AnswerSaysFrom(replier, target.ID); err != nil {
		log.Printf("[atrium] %s replied to %s but the owed reply could not be settled: %v", from, target.WireName, err)
	}
}

// recordTell records a `tell` that reached a resolved local card, typed or queued.
func (d *Daemon) recordTell(from string, target *store.Task, text, when string, reply, typed bool, msgID string) {
	rec := sayRecordFor(from, target, sayTrace{}, false, "tell", when, reply)
	if typed {
		rec.State, rec.Channel = store.SayDelivered, store.SayViaTerminal
	} else {
		rec.State, rec.MessageID = store.SayQueued, msgID
	}
	d.recordSay(rec, text)
	d.saySettled(from, target)
}

// sayLapsed gives up on replies owed to a card that has ended.
func (d *Daemon) sayLapsed(taskID string) {
	if err := d.st.LapseSaysFor(taskID); err != nil {
		log.Printf("[atrium] could not lapse the replies owed to %s: %v", taskID, err)
	}
}

// sayReset notes a clear or a compact on the says it may have erased.
func (d *Daemon) sayReset(taskID, kind string) {
	if err := d.st.NoteContextReset(taskID, kind); err != nil {
		log.Printf("[atrium] could not note a %s on the says to %s: %v", kind, taskID, err)
	}
}

// ── a miss answers with candidates ──────────────────────────────────────────

const maxCandidates = 8

// candidatesFor lists the live cards a mistyped name may have meant: the name
// contained in a handle or alias, ranked prefix first. NEVER resolved to. A
// near miss that is guessed at is a message to the wrong card.
func (d *Daemon) candidatesFor(name, exclude string) []string {
	needle := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "@"))
	if needle == "" {
		return nil
	}
	list, err := d.peers(exclude)
	if err != nil {
		return nil
	}
	type hit struct {
		label string
		rank  int
	}
	var hits []hit
	for _, p := range list {
		h, a := strings.ToLower(p.Handle), strings.ToLower(p.Alias)
		var rank int
		switch {
		case strings.HasPrefix(h, needle) || strings.HasPrefix(a, needle):
			rank = 0
		case strings.Contains(h, needle) || strings.Contains(a, needle):
			rank = 1
		default:
			continue
		}
		label := p.Handle
		if p.Alias != "" {
			label += " (@" + p.Alias + ")"
		}
		hits = append(hits, hit{label, rank})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].rank < hits[j].rank })
	out := make([]string, 0, maxCandidates)
	for _, h := range hits {
		if len(out) == maxCandidates {
			break
		}
		out = append(out, h.label)
	}
	return out
}

// missSentence is the error a sender reads for a name that matched nothing.
func missSentence(name string, cands []string) string {
	if len(cands) == 0 {
		return "no session called " + name + ". none is close. atrium_peers lists who can be reached. " +
			"nothing was messaged."
	}
	return "no session called " + name + ". did you mean: " + strings.Join(cands, ", ") +
		"? none of these was messaged."
}

// writeMiss answers a name that resolved to nothing, and records the attempt.
func (d *Daemon) writeMiss(w http.ResponseWriter, from, name, door, text, when string, reply bool) {
	d.writeMissNote(w, from, name, door, text, when, reply, "")
}

// writeMissNote is writeMiss with a sentence added to the error: what the hub
// said about cards on other rooms, or why it could not be asked.
func (d *Daemon) writeMissNote(w http.ResponseWriter, from, name, door, text, when string, reply bool, note string) {
	cands := d.candidatesFor(name, d.st.Qualify(from))
	list, _ := d.peers(d.st.Qualify(from))
	if from != "" && text != "" {
		note := "no card matched"
		if len(cands) > 0 {
			note += ". offered: " + strings.Join(cands, ", ")
		}
		d.recordSay(store.Say{FromWire: from, ToInput: name, Via: "none", Door: door, When: when,
			State: store.SayUnresolved, Note: note, ReplyWant: reply}, text)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": strings.TrimSpace(missSentence(name, cands) + " " + note), "candidates": cands, "peers": list,
	})
}

// handleTaskSays is `GET /v1/tasks/{id}/says`: the says a card sent or received.
func (d *Daemon) handleTaskSays(w http.ResponseWriter, r *http.Request) {
	rows, err := d.st.SaysFor(r.PathValue("id"), 20)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	if rows == nil {
		rows = []store.Say{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"says": rows})
}
