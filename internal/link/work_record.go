package link

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/edge"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/ctl"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// RECORDING A TEST, AND TAKING A REVIEW (item lifecycle L4, docs/rnd/item-lifecycle-design.md sections 3 and 4). The change
// record's C1 and C2, on the item.
//
//	POST /_hub/work/record         {room, card, kind: test, cmd, result: pass|fail, duration_s?, log? | log_file?}
//	                               a card's `atrium_record`. The hub finds the item the card is attached to, asks the card's room
//	                               to read the card's own worktree, and writes a `test` fact REPORTED at the sha the room read
//	POST /_hub/work/<id>/review    {verdict: ok|hold, findings?, report?, by?}
//	                               a review atrium took (what `atrium_review` will call). Never from a card
//
// THE RULES, each of which is a hole if it is missed:
//
//   - A CARD NEVER NAMES A SHA. The body has no field for one, and the decoder refuses a field it does not know, so `sha` and
//     `at_sha` are refused by name rather than ignored. The room reads the sha from the card's worktree.
//   - A CARD CANNOT WRITE A REVIEW. `kind` here is `test` and nothing else. A review fact is written by the hub from a report
//     it took itself, and the route refuses a request that says it comes from a session (X-Atrium-Agent). Verdict trailers are
//     never stored: they are read from git on every derivation, so a rebase cannot leave a copy that still says ok.
//   - THE WORKTREE'S BRANCH HAS TO BE THE ITEM'S. A record from a worktree on another branch is refused, naming both. An item
//     with no branch yet takes the first one a card records from, unless it is a main branch or another item's.
//   - A REPORTED TEST IS SHOWN AND DOES NOT COUNT. The step carries the `reported` tag, and the item is not `ready` until atrium
//     itself has seen a test at the same sha.

// protectedBranches are never adopted as an item's build branch.
var protectedBranches = map[string]bool{"main": true, "master": true, "claude/main": true, "HEAD": true}

type recordIn struct {
	Room      string  `json:"room"`
	Card      string  `json:"card"`
	Kind      string  `json:"kind"`
	Cmd       string  `json:"cmd"`
	Result    string  `json:"result"`
	DurationS float64 `json:"duration_s,omitempty"`
	Log       string  `json:"log,omitempty"`
	LogFile   string  `json:"log_file,omitempty"`
}

// RecordCmdMax is the longest command a record keeps, in bytes.
const RecordCmdMax = 300

// RecordLogMax is the most log text one call takes, in bytes. The room keeps the tail of a bigger one.
const RecordLogMax = 512 << 10

// stampReply is what the room read of the card's worktree.
type stampReply struct {
	AtSHA   string `json:"at_sha"`
	Dirty   bool   `json:"dirty"`
	Branch  string `json:"branch"`
	LogPath string `json:"log_path"`
}

// RecordOut is what a record answers.
type RecordOut struct {
	Recorded bool   `json:"recorded"`
	Item     string `json:"item"`
	AtSHA    string `json:"at_sha"`
	Dirty    bool   `json:"dirty,omitempty"`
	Branch   string `json:"branch"`
	Tag      string `json:"tag"`
	// Tested is how the tested step reads after this: its state and the words.
	Tested string `json:"tested"`
	Ready  bool   `json:"ready"`
	Note   string `json:"note,omitempty"`
}

func (p *Proxy) serveWorkRecord(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	var in recordIn
	if err := crDecode(r, &in); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "unknown field") {
			msg += ". a record never names a commit: atrium reads it from your worktree"
		}
		crFail(w, http.StatusBadRequest, "could not read that: "+msg)
		return
	}
	switch kind := strings.ToLower(strings.TrimSpace(in.Kind)); kind {
	case "test", "":
	case "review", "finding", "push", "ci":
		crFail(w, http.StatusForbidden, "a card cannot record a "+kind+": atrium reads that itself. record a test")
		return
	default:
		crFail(w, http.StatusBadRequest, "kind is test")
		return
	}
	in.Cmd = strings.TrimSpace(in.Cmd)
	switch {
	case in.Room == "" || in.Card == "":
		crFail(w, http.StatusBadRequest, "say which card this is")
		return
	case in.Cmd == "" || len(in.Cmd) > RecordCmdMax:
		crFail(w, http.StatusBadRequest, "cmd is the command you ran, up to 300 bytes")
		return
	case in.Result != hubstore.ResultPass && in.Result != hubstore.ResultFail:
		crFail(w, http.StatusBadRequest, "result is pass or fail")
		return
	case len(in.Log) > RecordLogMax:
		crFail(w, http.StatusBadRequest, "that log is over 512 KB. send log_file and the room keeps its tail")
		return
	case in.DurationS < 0 || in.DurationS > 7*24*3600:
		crFail(w, http.StatusBadRequest, "duration_s is the seconds the run took")
		return
	}
	work, err := st.WorkOfCard(in.Room, in.Card)
	if err != nil {
		if errors.Is(err, hubstore.ErrNoWork) {
			crFail(w, http.StatusConflict, "this card is not attached to an item, so there is nothing to record against. launch it with --item")
			return
		}
		wfail(w, err)
		return
	}
	raw, _ := json.Marshal(map[string]string{"item": work.BacklogID, "log": in.Log, "log_file": in.LogFile})
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	code, resp, err := p.callItemRoom(ctx, in.Room, "/v1/tasks/"+url.PathEscape(in.Card)+"/stamp", raw)
	if err != nil {
		crFail(w, http.StatusBadGateway, "the room this card is on is not answering: "+err.Error())
		return
	}
	if code >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(resp, &e)
		if e.Error == "" {
			e.Error = strings.TrimSpace(string(resp))
		}
		crFail(w, http.StatusConflict, "the room could not stamp this record: "+e.Error)
		return
	}
	var sr stampReply
	if json.Unmarshal(resp, &sr) != nil || !gitsync.ValidSHA(strings.ToLower(sr.AtSHA)) {
		crFail(w, http.StatusBadGateway, "the room did not say which commit this card is at")
		return
	}
	sr.AtSHA = strings.ToLower(sr.AtSHA)

	// THE BRANCH IS THE ITEM'S, or the call is refused.
	switch branch := strings.TrimSpace(sr.Branch); {
	case branch == "" || branch == "HEAD":
		crFail(w, http.StatusConflict, "this card's directory is on a detached HEAD, not on the item's branch, so a record cannot be pinned to it")
		return
	case work.Branch != "" && branch != work.Branch:
		crFail(w, http.StatusConflict, "this card's directory is on branch "+branch+", and "+work.BacklogID+" builds on "+work.Branch+
			". record from a directory on that branch")
		return
	case work.Branch == "":
		if protectedBranches[branch] {
			crFail(w, http.StatusConflict, "this card's directory is on "+branch+", which is not an item's build branch, so there is nothing to record against")
			return
		}
		if other, held, err := st.WorkOfBranch(branch, work.BacklogID); err != nil {
			wfail(w, err)
			return
		} else if held {
			crFail(w, http.StatusConflict, "branch "+branch+" already belongs to "+other+". a branch belongs to at most one item")
			return
		}
		if err := st.SetWorkBranch(work.BacklogID, branch); err != nil {
			wfail(w, err)
			return
		}
		work.Branch = branch
	}

	pl, _ := json.Marshal(map[string]any{"cmd": in.Cmd, "result": in.Result, "room": in.Room, "duration_s": in.DurationS,
		"log": sr.LogPath, "branch": sr.Branch})
	if _, err := st.AddWorkFact(hubstore.WorkFact{BacklogID: work.BacklogID, Kind: hubstore.FactTest, Source: hubstore.SourceReported,
		Card: cardKey(in.Room, in.Card), Ref: newFactRef(), AtSHA: sr.AtSHA, Dirty: sr.Dirty, Payload: string(pl)}); err != nil {
		wfail(w, err)
		return
	}
	got, err := p.refreshWork(r.Context(), st, work.BacklogID, true)
	if err != nil {
		wfail(w, err)
		return
	}
	p.backlogEvent("item", map[string]string{"id": work.BacklogID, "work": "1"})
	out := RecordOut{Recorded: true, Item: work.BacklogID, AtSHA: sr.AtSHA, Dirty: sr.Dirty, Branch: sr.Branch,
		Tag: hubstore.TagReported, Ready: got.Lifecycle.Ready, Note: got.Lifecycle.NotReady}
	for _, s := range got.Lifecycle.Steps {
		if s.Step == hubstore.StepTested {
			out.Tested = s.State + ": " + s.Reason
		}
	}
	crJSON(w, http.StatusOK, out)
}

// serveWorkReview is `POST /_hub/work/<id>/review`: a review atrium took. The hub pins it to the branch tip it reads itself.
func (p *Proxy) serveWorkReview(w http.ResponseWriter, r *http.Request, st *hubstore.Store, id string) {
	switch {
	case wire.DocsCrossOrigin.Check(r) != nil:
		crFail(w, http.StatusForbidden, "a page on another origin cannot write here")
		return
	case !edge.LocalOperator(r):
		crFail(w, http.StatusForbidden, "reviews are taken only from the machine the hub runs on"+edge.ProxyNote(r))
		return
	case strings.TrimSpace(r.Header.Get(ctl.AgentHeader)) != "":
		crFail(w, http.StatusForbidden, "a card cannot write a review. atrium takes one from a review it ran, or reads a verdict trailer")
		return
	}
	var in struct {
		Verdict  string `json:"verdict"`
		Findings int    `json:"findings"`
		Report   string `json:"report"`
		By       string `json:"by"`
	}
	if err := crDecode(r, &in); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error()+". a review never names a commit: the hub pins it to the branch tip")
		return
	}
	if in.Verdict != hubstore.VerdictOK && in.Verdict != hubstore.VerdictHold {
		crFail(w, http.StatusBadRequest, "verdict is ok or hold")
		return
	}
	if in.Findings < 0 || in.Findings > 10000 || len(in.Report) > 1000 || len(in.By) > 200 {
		crFail(w, http.StatusBadRequest, "findings is a count, report a path or link of up to 1000 bytes")
		return
	}
	work, err := st.WorkByID(id)
	if err != nil {
		wfail(w, err)
		return
	}
	var tip string
	p.mu.Lock()
	lg := p.landGit
	p.mu.Unlock()
	if g, ok := lg.(EvidenceGit); ok && g != nil && work.Branch != "" {
		cctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		ev, _ := g.ItemEvidence(cctx, work.Branch, nil)
		cancel()
		tip = ev.Tip
	}
	if tip == "" {
		crFail(w, http.StatusConflict, "the hub does not hold a tip for "+id+"'s branch, so a review cannot be pinned to a commit. push the branch first")
		return
	}
	pl, _ := json.Marshal(map[string]any{"verdict": in.Verdict, "findings": in.Findings, "report": in.Report, "by": in.By})
	if _, err := st.AddWorkFact(hubstore.WorkFact{BacklogID: id, Kind: hubstore.FactReview, Source: hubstore.SourceSeen,
		Ref: newFactRef(), AtSHA: tip, Payload: string(pl)}); err != nil {
		wfail(w, err)
		return
	}
	p.RecordAudit("", "work-review", id)
	if _, err := p.refreshWork(r.Context(), st, id, true); err != nil {
		wfail(w, err)
		return
	}
	p.backlogEvent("item", map[string]string{"id": id, "work": "1"})
	p.writeWorkDetail(w, r, st, id)
}
