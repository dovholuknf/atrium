package hubstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// THE STEP ENGINE'S EVIDENCE (item lifecycle L2). The derived step rows are workstep.go's, one model for every stage. See docs/rnd/item-lifecycle-design.md, sections 3 and 4.1.
//
//   - A FACT IS APPENDED AND NEVER EDITED. The unique key is (backlog_id, kind, ref, at_sha), so the same approval of the same
//     design, or the same design noticed twice, is one row. `ref` is what tells two facts of one kind at one sha apart:
//     the assignment a design came from, or one send-back among several.
//   - A STEP ROW IS A CACHE. The engine (internal/link/lifecycle.go) derives all eight from the facts, the cards, the
//     assignments and git, and writes them whole. Nothing here is the only copy of anything, so a bug in the engine is
//     fixed by running it again.

// The eight steps, in order.
const (
	StepDesigned = "designed"
	StepApproved = "approved"
	StepBuilding = "building"
	StepTested   = "tested"
	StepReviewed = "reviewed"
	StepLanded   = "landed"
	StepDeployed = "deployed"
	StepPushed   = "pushed"
)

// Steps is every step, in order.
var Steps = []string{StepDesigned, StepApproved, StepBuilding, StepTested, StepReviewed, StepLanded, StepDeployed, StepPushed}

// ValidStep is whether s is one of the eight.
func ValidStep(s string) bool {
	for _, x := range Steps {
		if x == s {
			return true
		}
	}
	return false
}

// Parties: who a step is waiting on when it is active.
const (
	PartyHuman = "human"
	PartyCard  = "card"
)

// Fact kinds of this stage.
const (
	// FactDesign is a designer's finished commit that touches a design doc, noticed by the hub. at_sha is that commit, ref is
	// the assignment, payload {repo, path}.
	FactDesign = "design"
	// FactApprove is the human's click. at_sha is the design doc's commit that was approved, payload {design} names the
	// designer's commit it approved, so a design sent back and made again is approved again.
	FactApprove = "approve"
	// FactSendBack is the human sending a design back. at_sha is the designer's commit that was sent back, payload {note}.
	FactSendBack = "send_back"
	// FactBuildDirectly is the item being filed to skip designed and approved.
	FactBuildDirectly = "build_directly"
	// FactTest is a test run. at_sha and dirty are STAMPED by the room from the card's own worktree, never named by the card.
	// ref is one record, so two runs at one commit are two facts. payload {cmd, result: pass|fail, room, duration, log, branch}.
	// Source is `reported` for a card's run, and `seen` only when atrium ran the tests itself.
	FactTest = "test"
	// FactReview is a verdict on the item's branch, always `seen`: no card writes one. at_sha is the tip the hub read when it
	// took the report, payload {verdict: ok|hold, findings, report, by}. A verdict trailer is read from git each time and is
	// never stored as a fact, so a rebase cannot leave a stale copy of it.
	FactReview = "review"
)

// Test results and review verdicts.
const (
	ResultPass = "pass"
	ResultFail = "fail"

	VerdictOK   = "ok"
	VerdictHold = "hold"
)

// Fact sources: seen is what atrium read itself, reported is a card's say-so.
const (
	SourceSeen     = "seen"
	SourceReported = "reported"
)

// WorkFact is one append-only fact about an item.
type WorkFact struct {
	Seq       int64  `json:"seq"`
	BacklogID string `json:"backlog_id"`
	Kind      string `json:"kind"`
	Source    string `json:"source"`
	// Card is `room/card` of who it came from, empty for the human and for atrium itself.
	Card    string `json:"card,omitempty"`
	Ref     string `json:"ref,omitempty"`
	AtSHA   string `json:"at_sha,omitempty"`
	Dirty   bool   `json:"dirty,omitempty"`
	At      string `json:"at"`
	Payload string `json:"payload,omitempty"`
}

// Note reads a string out of the fact's JSON payload.
func (f WorkFact) Note(key string) string {
	if f.Payload == "" {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(f.Payload), &m) != nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

var errFact = errors.New("a fact has an item, a kind and a source of seen or reported")

// AddWorkFact appends a fact, once. It answers whether this call wrote it: the same (item, kind, ref, sha) again changes
// nothing and answers false.
func (s *Store) AddWorkFact(f WorkFact) (bool, error) {
	f.BacklogID = strings.TrimSpace(f.BacklogID)
	if f.BacklogID == "" || f.Kind == "" || (f.Source != SourceSeen && f.Source != SourceReported) {
		return false, refuse(errFact)
	}
	wrote := false
	err := s.tx(func(t *sql.Tx) error {
		wrote = false
		res, err := t.Exec(`INSERT OR IGNORE INTO work_fact (backlog_id, kind, source, card, ref, at_sha, dirty, at, payload)
			VALUES (?,?,?,?,?,?,?,?,?)`, f.BacklogID, f.Kind, f.Source, f.Card, f.Ref, f.AtSHA, b2i(f.Dirty), ts(s.now()), f.Payload)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		wrote = n > 0
		return nil
	})
	return wrote, err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// WorkFacts is every fact of an item, oldest first.
func (s *Store) WorkFacts(id string) ([]WorkFact, error) {
	var out []WorkFact
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT seq, backlog_id, kind, source, card, ref, at_sha, dirty, at, payload FROM work_fact
			WHERE backlog_id = ? ORDER BY seq`, strings.TrimSpace(id))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var f WorkFact
			var dirty int
			if err := rows.Scan(&f.Seq, &f.BacklogID, &f.Kind, &f.Source, &f.Card, &f.Ref, &f.AtSHA, &dirty, &f.At, &f.Payload); err != nil {
				return err
			}
			f.Dirty = dirty != 0
			out = append(out, f)
		}
		return rows.Err()
	})
	return out, err
}
