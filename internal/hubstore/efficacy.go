package hubstore

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// EFFICACY: HOW WELL THE PROCESS WORKED (item lifecycle L9, docs/rnd/item-lifecycle-design.md section 12).
//
//   - AN EVENT IS A FACT ABOUT A STEP, NOT A GRADE FOR A CARD. `work_event` is append-only, and its key (work_id, kind, at,
//     sha) is what makes a replayed collect insert nothing. A send-back is counted by the transition: two at different
//     times are two even when the note repeats.
//   - AN OUTCOME IS DERIVED. `work_outcome` is rewritten whole on every tick from the events, the steps and the token
//     rows, never edited by hand, so a changed rule is a recompute. It is a cache and nothing in it is the only copy.
//   - EVERY EVENT SAYS OBSERVED OR REPORTED. Observed is atrium's own read (a sha on the mirror, a CI result, a send-back it
//     recorded). Reported is somebody's say-so, and carries the tag all the way to the figure.

// The kinds of event.
const (
	EventSendBack     = "send_back"
	EventReviewVerdct = "review_verdict"
	EventNotAccepted  = "not_accepted"
	EventTestResult   = "test_result"
	EventCIRed        = "ci_red"
	EventCIGreen      = "ci_green"
	EventRevert       = "revert"
	EventBugFiled     = "bug_filed"
	EventAbandoned    = "abandoned"
)

// ValidEventKind is whether k is one of the kinds.
func ValidEventKind(k string) bool {
	switch k {
	case EventSendBack, EventReviewVerdct, EventNotAccepted, EventTestResult, EventCIRed, EventCIGreen, EventRevert,
		EventBugFiled, EventAbandoned:
		return true
	}
	return false
}

// The sources of an event.
const (
	EvObserved = "observed"
	EvReported = "reported"
)

// WorkEvent is one thing that went right or wrong on an item.
type WorkEvent struct {
	ID     int64  `json:"id"`
	WorkID string `json:"work_id"`
	Kind   string `json:"kind"`
	Step   string `json:"step,omitempty"`
	At     string `json:"at"`
	SHA    string `json:"sha,omitempty"`
	Note   string `json:"note,omitempty"`
	Source string `json:"source"`
}

var errEvent = errors.New("an event has an item, a kind, a time and a source of observed or reported")

// AddWorkEvent records an event, once. It answers whether this call wrote it: the same (item, kind, time, sha) again
// changes nothing and answers false.
func (s *Store) AddWorkEvent(e WorkEvent) (bool, error) {
	e.WorkID, e.SHA = strings.TrimSpace(e.WorkID), strings.ToLower(strings.TrimSpace(e.SHA))
	if e.WorkID == "" || !ValidEventKind(e.Kind) || e.At == "" || (e.Source != EvObserved && e.Source != EvReported) {
		return false, refuse(errEvent)
	}
	if len(e.Note) > ItemNoteMax {
		e.Note = e.Note[:ItemNoteMax]
	}
	wrote := false
	err := s.tx(func(t *sql.Tx) error {
		wrote = false
		res, err := t.Exec(`INSERT OR IGNORE INTO work_event (work_id, kind, step, at, sha, note, source) VALUES (?,?,?,?,?,?,?)`,
			e.WorkID, e.Kind, e.Step, e.At, e.SHA, e.Note, e.Source)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		wrote = n > 0
		return nil
	})
	return wrote, err
}

// WorkEvents is the events of one item (all of them when id is empty), oldest first.
func (s *Store) WorkEvents(id string) (map[string][]WorkEvent, error) {
	out := map[string][]WorkEvent{}
	err := s.guard(func() error {
		out = map[string][]WorkEvent{}
		q, args := `SELECT id, work_id, kind, step, at, sha, note, source FROM work_event`, []any(nil)
		if id = strings.TrimSpace(id); id != "" {
			q, args = q+` WHERE work_id = ?`, []any{id}
		}
		rows, err := s.db.Query(q+` ORDER BY at, id`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e WorkEvent
			if err := rows.Scan(&e.ID, &e.WorkID, &e.Kind, &e.Step, &e.At, &e.SHA, &e.Note, &e.Source); err != nil {
				return err
			}
			out[e.WorkID] = append(out[e.WorkID], e)
		}
		return rows.Err()
	})
	return out, err
}

// WorkOutcome is one item's derived measures. Costs are in 1e-10 dollar, as the cost view's Units are.
type WorkOutcome struct {
	WorkID           string `json:"work_id"`
	LandedAt         string `json:"landed_at,omitempty"`
	CleanAt          string `json:"clean_at,omitempty"`
	WatchEndsAt      string `json:"watch_ends_at,omitempty"`
	Verdict          string `json:"verdict"`
	FirstPass        string `json:"first_pass,omitempty"`
	FirstPassSource  string `json:"first_pass_source,omitempty"`
	EscapedBugs      int    `json:"escaped_bugs"`
	Reverted         int    `json:"reverted"`
	CIRedSeconds     int64  `json:"ci_red_seconds"`
	WaitHumanSeconds int64  `json:"wait_human_seconds"`
	WorkSeconds      int64  `json:"work_seconds"`
	StepCost         int64  `json:"step_cost"`
	CoordCost        int64  `json:"coord_cost"`
	CostKnown        bool   `json:"cost_known"`
	AbandonedAt      string `json:"abandoned_at,omitempty"`
	AbandonedCost    int64  `json:"abandoned_cost,omitempty"`
	// Detail is JSON the derivation owns: counts by kind and step, the bug ages, the step windows.
	Detail     string `json:"detail"`
	ComputedAt string `json:"computed_at"`
}

const outcomeCols = `work_id, landed_at, clean_at, watch_ends_at, verdict, first_pass, first_pass_source, escaped_bugs, reverted,
	ci_red_seconds, wait_human_seconds, work_seconds, step_cost, coord_cost, cost_known, abandoned_at, abandoned_cost, detail, computed_at`

// PutWorkOutcome stores a derived outcome whole, replacing the last.
func (s *Store) PutWorkOutcome(o WorkOutcome) error {
	if o.Detail == "" {
		o.Detail = "{}"
	}
	if o.ComputedAt == "" {
		o.ComputedAt = ts(time.Now())
	}
	return s.guard(func() error {
		_, err := s.db.Exec(`INSERT OR REPLACE INTO work_outcome (`+outcomeCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			o.WorkID, o.LandedAt, o.CleanAt, o.WatchEndsAt, o.Verdict, o.FirstPass, o.FirstPassSource, o.EscapedBugs, o.Reverted,
			o.CIRedSeconds, o.WaitHumanSeconds, o.WorkSeconds, o.StepCost, o.CoordCost, b2i(o.CostKnown), o.AbandonedAt,
			o.AbandonedCost, o.Detail, o.ComputedAt)
		return err
	})
}

// WorkOutcomes is every derived outcome, by item.
func (s *Store) WorkOutcomes() (map[string]WorkOutcome, error) {
	out := map[string]WorkOutcome{}
	err := s.guard(func() error {
		out = map[string]WorkOutcome{}
		rows, err := s.db.Query(`SELECT ` + outcomeCols + ` FROM work_outcome`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var o WorkOutcome
			var known int
			if err := rows.Scan(&o.WorkID, &o.LandedAt, &o.CleanAt, &o.WatchEndsAt, &o.Verdict, &o.FirstPass, &o.FirstPassSource,
				&o.EscapedBugs, &o.Reverted, &o.CIRedSeconds, &o.WaitHumanSeconds, &o.WorkSeconds, &o.StepCost, &o.CoordCost,
				&known, &o.AbandonedAt, &o.AbandonedCost, &o.Detail, &o.ComputedAt); err != nil {
				return err
			}
			o.CostKnown = known != 0
			out[o.WorkID] = o
		}
		return rows.Err()
	})
	return out, err
}

// BacklogForEfficacy is the backlog rows the bug scan and the dropped check read, in one query.
func (s *Store) BacklogForEfficacy() ([]BacklogItem, error) {
	var out []BacklogItem
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT id, dept, title, body, priority, status, card, filed_by, filed_room, created_at, changed_at, changed_by
			FROM backlog_item`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var b BacklogItem
			if err := rows.Scan(&b.ID, &b.Dept, &b.Title, &b.Body, &b.Priority, &b.Status, &b.Card, &b.FiledBy, &b.FiledRoom,
				&b.CreatedAt, &b.ChangedAt, &b.ChangedBy); err != nil {
				return err
			}
			out = append(out, b)
		}
		return rows.Err()
	})
	return out, err
}
