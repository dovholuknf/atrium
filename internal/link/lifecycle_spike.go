package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// THE SPIKED GATE: a note at docs/design/<item>*.md whose first line is the Decision holds the item for the human.

var decisionLine = regexp.MustCompile(`^Decision: (build|drop|question)(\s.*)?$`)

// ParseDecision is the note's first line when it is a Decision line, else "".
func ParseDecision(text string) string {
	text = strings.TrimPrefix(text, string(rune(0xFEFF)))
	first, _, _ := strings.Cut(text, "\n")
	first = strings.TrimSpace(first)
	if !decisionLine.MatchString(first) {
		return ""
	}
	return first
}

// isNotePath is whether path is a note of the item: docs/design/<id>*.md, directly in that folder.
func isNotePath(path, id string) bool {
	l := strings.ToLower(path)
	rest, ok := strings.CutPrefix(l, "docs/design/")
	return ok && id != "" && strings.HasPrefix(rest, strings.ToLower(id)) && strings.HasSuffix(rest, ".md") && !strings.Contains(rest, "/")
}

// spikeFact is a gate fact (approve, send_back, drop) the human gave at the spiked gate rather than the design's.
func spikeFact(f hubstore.WorkFact) bool { return f.Note("step") == hubstore.StepSpiked }

// spikeNotes are the item's notes that answer the spiked step, oldest first.
func spikeNotes(facts []hubstore.WorkFact) []hubstore.WorkFact {
	var out []hubstore.WorkFact
	for _, f := range facts {
		if f.Kind == hubstore.FactNote && f.Note("step") == hubstore.StepSpiked {
			out = append(out, f)
		}
	}
	return out
}

// spikeGateFact is the newest gate fact of kind given at the spiked gate on the note at sha.
func spikeGateFact(facts []hubstore.WorkFact, kind, sha string) (hubstore.WorkFact, bool) {
	for i := len(facts) - 1; i >= 0; i-- {
		if f := facts[i]; f.Kind == kind && spikeFact(f) && f.AtSHA == sha {
			return f, true
		}
	}
	return hubstore.WorkFact{}, false
}

// deriveSpiked is the `spiked` step. An item no spike was run for skips it, so every item from before the gate reads as it did.
func deriveSpiked(in DeriveIn) hubstore.WorkStepRow {
	sp := in.of(hubstore.StepSpiked)
	row := hubstore.WorkStepRow{Step: hubstore.StepSpiked, State: hubstore.StepPending, Cards: hubstore.CardsJSON(sp.cards), StartedAt: sp.started()}
	notes := spikeNotes(in.Facts)
	switch {
	case has(in.Facts, hubstore.FactBuildDirectly):
		row.State, row.Reason = hubstore.StepSkipped, "filed to build directly"
		return row
	case Adhoc(in.Work):
		row.State, row.Reason = hubstore.StepSkipped, "no backlog item named this work, so there is no spike"
		return row
	case len(notes) == 0 && len(sp.cards) == 0:
		row.State, row.Reason = hubstore.StepSkipped, "no spike was run for this item"
		return row
	case len(notes) == 0:
		switch {
		case sp.any(hubstore.ItemStateBlocked):
			row.State, row.Party, row.Reason = hubstore.StepBlocked, hubstore.PartyCard, "the spiker is blocked"
		case sp.any(hubstore.ItemStateRunning, hubstore.ItemStateQueued):
			row.State, row.Party = hubstore.StepActive, hubstore.PartyCard
		case sp.all(hubstore.ItemStateFinished):
			row.State, row.Party = hubstore.StepActive, hubstore.PartyCard
			at, sha := sp.lastFinished()
			row.EndedAt, row.SHA = at, sha
			row.Reason = "the spiker finished with no note at docs/design/" + in.Work.BacklogID + "*.md the hub can read"
		}
		return row
	}
	note := notes[len(notes)-1]
	row.SHA = note.AtSHA
	if row.StartedAt == "" {
		row.StartedAt = notes[0].At
	}
	dec := note.Note("decision")
	at := short(note.AtSHA)
	var prior hubstore.WorkFact
	priorOK := false
	for _, n := range notes[:len(notes)-1] {
		if f, ok := spikeGateFact(in.Facts, hubstore.FactApprove, n.AtSHA); ok {
			prior, priorOK = f, true
		}
	}
	drop, dropped := spikeGateFact(in.Facts, hubstore.FactDrop, note.AtSHA)
	ok, accepted := spikeGateFact(in.Facts, hubstore.FactApprove, note.AtSHA)
	_, sent := spikeGateFact(in.Facts, hubstore.FactSendBack, note.AtSHA)
	switch {
	case dec == "":
		row.State, row.Party = hubstore.StepActive, hubstore.PartyCard
		row.Reason = "the note at " + at + " has no Decision line"
	case dropped:
		row.State, row.EndedAt = hubstore.StepDone, drop.At
		row.Reason = "dropped at " + at + ": " + dec
	case accepted:
		row.State, row.EndedAt = hubstore.StepDone, ok.At
		row.Reason = "accepted at " + at + ": " + dec
	case sent:
		row.State, row.Party = hubstore.StepActive, hubstore.PartyCard
		row.Reason = "sent back at " + at + ", waiting for the spiker's next note"
	case priorOK:
		row.State, row.Party = hubstore.StepStale, hubstore.PartyHuman
		row.Reason = "accepted at " + short(prior.AtSHA) + ", and the note has changed since, at " + at + ": " + dec
	default:
		row.State, row.Party = hubstore.StepActive, hubstore.PartyHuman
		row.Reason = "waiting on you: " + dec + ". accept, send back with a note, or drop"
	}
	return row
}

// noteGit reads a file at a commit. gitsync.Store has it, and a test's git may.
type noteGit interface {
	FileAt(ctx context.Context, repo, sha, path string) (string, bool, error)
}

var _ noteGit = (*gitsync.Store)(nil)

// notesRead is the store, item and commit triples already read for notes. A commit never changes, so a finished card is read once.
var notesRead sync.Map

// notePayload is what a note fact says.
type notePayload struct {
	Repo       string `json:"repo"`
	Path       string `json:"path"`
	SHA        string `json:"sha"`
	Step       string `json:"step"`
	Decision   string `json:"decision"`
	Assignment string `json:"assignment"`
}

// noticeNotes writes a `note` fact for each note a finished card's commit touches, with its Decision line, once. A note
// from a designer answers `designed`, and any other card's answers `spiked`. True when it wrote one.
func (p *Proxy) noticeNotes(ctx context.Context, st *hubstore.Store, gr designGit, r *workRefresh) bool {
	fa, ok := gr.(noteGit)
	if !ok {
		return false
	}
	id := r.Work.BacklogID
	stepOf := map[string]string{}
	for _, c := range r.Cards {
		stepOf[cardKey(c.Room, c.Card)] = StepOfCard(c)
	}
	wrote := false
	for _, a := range r.Assignments {
		sha := NoteSHA(a.Note)
		if a.State != hubstore.ItemStateFinished || sha == "" {
			continue
		}
		key := fmt.Sprintf("%p@%s@%s", st, id, sha)
		if _, done := notesRead.Load(key); done {
			continue
		}
		repo, full, ok := gr.FindCommit(ctx, sha)
		if !ok {
			continue
		}
		files, err := gr.CommitFiles(ctx, repo, full)
		if err != nil {
			continue
		}
		read := true
		for _, f := range files {
			if !isNotePath(f, id) {
				continue
			}
			text, ok, err := fa.FileAt(ctx, repo, full, f)
			if err != nil {
				read = false
				continue
			}
			if !ok {
				continue
			}
			step := hubstore.StepSpiked
			if stepOf[cardKey(a.Room, a.Card)] == hubstore.StepDesigned {
				step = hubstore.StepDesigned
			}
			pl, _ := json.Marshal(notePayload{Repo: repo, Path: f, SHA: full, Step: step, Decision: ParseDecision(text), Assignment: a.ID})
			if w, err := st.AddWorkFact(hubstore.WorkFact{
				BacklogID: id, Kind: hubstore.FactNote, Source: hubstore.SourceSeen,
				Card: cardKey(a.Room, a.Card), Ref: f, AtSHA: full, Payload: string(pl),
			}); err == nil && w {
				wrote = true
			} else if err != nil {
				read = false
			}
		}
		if read {
			notesRead.Store(key, true)
		}
	}
	return wrote
}

// spikeAction answers the spiked gate: approve or drop the note at its sha, or send it back to the card that wrote it.
// False when it has already answered the request with a refusal.
func (p *Proxy) spikeAction(w http.ResponseWriter, r *http.Request, st *hubstore.Store, id, do, by string, in workAct,
	cur workRefresh, sp hubstore.WorkStepRow) bool {
	notes := spikeNotes(cur.Facts)
	if len(notes) == 0 {
		crFail(w, http.StatusConflict, "there is no spike note waiting for you")
		return false
	}
	note := notes[len(notes)-1]
	if sha := strings.ToLower(strings.TrimSpace(in.SHA)); sha != "" && sha != note.AtSHA {
		crFail(w, http.StatusConflict, "the note is now at "+short(note.AtSHA)+", not "+short(sha)+". read it again")
		return false
	}
	switch do {
	case "approve", "drop":
		kind := hubstore.FactApprove
		if do == "drop" {
			kind = hubstore.FactDrop
		}
		pl, _ := json.Marshal(map[string]string{"step": hubstore.StepSpiked, "by": by})
		if _, err := st.AddWorkFact(hubstore.WorkFact{BacklogID: id, Kind: kind, Source: hubstore.SourceSeen,
			Ref: hubstore.StepSpiked, AtSHA: note.AtSHA, Payload: string(pl)}); err != nil {
			wfail(w, err)
			return false
		}
		if do == "drop" {
			if _, err := st.ItemSetStatus(id, hubstore.ItemDropped, by); err != nil && !errors.Is(err, hubstore.ErrItemNotFound) {
				wfail(w, err)
				return false
			}
		}
		p.RecordAudit("", "work-spike-"+do, id)
	case "send_back":
		text := strings.TrimSpace(in.Note)
		switch {
		case text == "":
			crFail(w, http.StatusBadRequest, "a spike note is sent back with a note, which is typed to the card that wrote it")
			return false
		case len(text) > hubstore.ItemNoteMax:
			crFail(w, http.StatusBadRequest, "that note is too long")
			return false
		}
		a, err := st.ItemByID(note.Note("assignment"))
		if err != nil {
			ifail(w, err)
			return false
		}
		raw, _ := json.Marshal(map[string]string{"by": by, "answer": text})
		code, resp, err := p.callItemRoom(r.Context(), a.Room, "/v1/items/"+a.ID+"/sendback", raw)
		if err != nil {
			crFail(w, http.StatusBadGateway, "the room that ran the spike is not answering: "+err.Error())
			return false
		}
		if code >= 300 {
			w.WriteHeader(code)
			_, _ = w.Write(resp)
			return false
		}
		pl, _ := json.Marshal(map[string]string{"note": text, "by": by, "step": hubstore.StepSpiked})
		if _, err := st.AddWorkFact(hubstore.WorkFact{BacklogID: id, Kind: hubstore.FactSendBack, Source: hubstore.SourceSeen,
			Card: cardKey(a.Room, a.Card), Ref: newFactRef(), AtSHA: note.AtSHA, Payload: string(pl)}); err != nil {
			wfail(w, err)
			return false
		}
		p.RecordAudit(a.Room, "work-spike-send-back", id)
	}
	return true
}
