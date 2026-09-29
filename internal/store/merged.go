package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// The merged-cull mark: a worker whose branch a merge covered is culled after a
// grace period unless somebody keeps it. See docs/rnd/merged-cull-design.md.
//
// The mark lives on the work item, not the card, because only a card another
// session launched has one, and that is exactly the set this applies to.
//
// THE MARK IS CLEARED BY A NEW TURN, in setStatusOn, and not here. A card going
// back to running is the launcher sending it back to fix something, and the
// only place that sees every such change is the one place a status is written.
//
// A HOLD IS NOT A MARK. It survives everything but an explicit atrium_cull:
// a later merge never marks a held card again.

// MarkMerged marks a worker's item as merged into `into` at `sha`, to be culled
// at `cullAt`. It reports whether it marked. It does not when the item is held,
// is already closed, or already carries a mark: a second merge must not push
// the deadline out from under whoever is watching the chip.
func (s *Store) MarkMerged(taskID, into, sha, branch string, cullAt time.Time) (bool, error) {
	marked := false
	err := s.inTx(func(tx *Tx) error {
		marked = false
		n := now()
		res, err := tx.Exec(`UPDATE work_item SET merged_at = ?, merged_into = ?, merged_sha = ?,
				merged_branch = ?, cull_at = ?
			WHERE task_id = ? AND held_by = '' AND cull_at = ''
				AND state IN `+openStatesSQL, ts(n), into, sha, branch, ts(cullAt), taskID)
		if err != nil {
			return err
		}
		got, err := res.RowsAffected()
		if err != nil || got == 0 {
			return err
		}
		marked = true
		if _, err := s.insertLog(tx, taskID, logRow{kind: LogAtrium, by: ByAtrium,
			text: "branch " + branch + " merged into " + into + " at " + shortSHA(sha) +
				", to be culled at " + cullAt.Local().Format("15:04")}); err != nil {
			return err
		}
		s.ledgerChanged(tx, taskID)
		return nil
	})
	return marked, err
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// clearMarkOn drops the mark, on whatever connection the caller holds.
func clearMarkOn(q querier, taskID string) (bool, error) {
	res, err := q.Exec(`UPDATE work_item SET merged_at = '', merged_into = '', merged_sha = '',
			merged_branch = '', cull_at = ''
		WHERE task_id = ? AND (cull_at != '' OR merged_at != '')`, taskID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ClearCullMark drops a card's mark, and reports whether there was one.
func (s *Store) ClearCullMark(taskID string) (bool, error) {
	cleared := false
	err := s.guard(func() error {
		var err error
		cleared, err = clearMarkOn(s.db, taskID)
		return err
	})
	if err == nil && cleared && s.OnLedgerChange != nil {
		s.OnLedgerChange(taskID)
	}
	return cleared, err
}

// HoldCull keeps a card: the mark is dropped and `by` is recorded, and nothing
// marks it again. `sql.ErrNoRows` when the card has no work item.
func (s *Store) HoldCull(taskID, by string) error {
	by = strings.TrimSpace(by)
	if by == "" {
		by = "someone"
	}
	return s.inTx(func(tx *Tx) error {
		res, err := tx.Exec(`UPDATE work_item SET held_by = ?, merged_at = '', merged_into = '',
				merged_sha = '', merged_branch = '', cull_at = '' WHERE task_id = ?`, by, taskID)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return sql.ErrNoRows
		}
		if _, err := s.insertLog(tx, taskID, logRow{kind: LogAtrium, by: ByAtrium,
			text: "held by " + by + ", so it is not culled automatically"}); err != nil {
			return err
		}
		s.ledgerChanged(tx, taskID)
		return nil
	})
}

// MergedView is what the board's chip needs: "merged · culling in 28m · keep".
// Absent on a card that is neither marked nor held.
type MergedView struct {
	// Into and Branch are what merged into what, and SHA the tip it merged at.
	Into   string `json:"into,omitempty"`
	Branch string `json:"branch,omitempty"`
	SHA    string `json:"sha,omitempty"`
	// MergedAt is when the room noticed, and CullAt when it will cull. CullAt is
	// absent once held. CullSeconds is the time left, so a browser whose clock
	// disagrees with the machine's does not show a cull in the past.
	MergedAt    *time.Time `json:"merged_at,omitempty"`
	CullAt      *time.Time `json:"cull_at,omitempty"`
	CullSeconds int64      `json:"cull_seconds,omitempty"`
	// HeldBy is who kept it. A held card is never culled automatically.
	HeldBy string `json:"held_by,omitempty"`
}

func (w *WorkItem) mergedView(at time.Time) *MergedView {
	if w == nil || (w.CullAt == nil && w.MergedAt == nil && w.HeldBy == "") {
		return nil
	}
	v := &MergedView{Into: w.MergedInto, Branch: w.MergedBranch, SHA: w.MergedSHA,
		MergedAt: w.MergedAt, CullAt: w.CullAt, HeldBy: w.HeldBy}
	if w.CullAt != nil {
		if left := w.CullAt.Sub(at); left > 0 {
			v.CullSeconds = int64(left.Seconds())
		}
	}
	return v
}

// MergedViewFor is one card's chip data, or nil.
func (s *Store) MergedViewFor(taskID string) (*MergedView, error) {
	var out *MergedView
	err := s.guard(func() error {
		w, err := workItemOn(s.db, taskID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out = w.mergedView(now())
		return nil
	})
	return out, err
}

// MergedViews is every card's chip data in one query, for the list.
func (s *Store) MergedViews() (map[string]*MergedView, error) {
	out := map[string]*MergedView{}
	err := s.guard(func() error {
		items, err := listWorkItemsOn(s.db, `merged_at != '' OR cull_at != '' OR held_by != ''`)
		if err != nil {
			return err
		}
		at := now()
		for _, w := range items {
			out[w.TaskID] = w.mergedView(at)
		}
		return nil
	})
	return out, err
}

// ArchiveCulled takes a culled card off the board and says why in its event
// log. The status is left alone: recording a culled worker as dead would put a
// death in the ledger for work that was accepted. The card and its history stay.
func (s *Store) ArchiveCulled(taskID, why string) error {
	return s.guard(func() error {
		res, err := s.db.Exec(`UPDATE task SET archived_at = ? WHERE id = ? AND archived_at = ''`,
			ts(now()), taskID)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			return err
		}
		return s.appendEvent(taskID, EventNotified, map[string]any{
			"by": "atrium", "detected": why, "archived": true,
		})
	})
}

// DueCulls lists the items whose cull time has come.
func (s *Store) DueCulls(at time.Time) ([]*WorkItem, error) {
	var out []*WorkItem
	err := s.guard(func() error {
		var err error
		out, err = listWorkItemsOn(s.db, `cull_at != '' AND cull_at <= ? AND held_by = ''`, ts(at))
		return err
	})
	return out, err
}

// AcceptMerged closes an item as accepted after its merged worker was culled:
// the first code to reach WorkAccepted. The mark goes with it.
func (s *Store) AcceptMerged(taskID string) error {
	return s.inTx(func(tx *Tx) error {
		res, err := tx.Exec(`UPDATE work_item SET state = ?, state_at = ?, state_by = ?,
				revision = revision + 1, accepted_report_id = latest_report_id, cull_at = ''
			WHERE task_id = ? AND state IN `+openStatesSQL, WorkAccepted, ts(now()), ByAtrium, taskID)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			return err
		}
		if _, err := s.insertLog(tx, taskID, logRow{kind: LogVerdict, by: ByAtrium, status: WorkAccepted,
			text: "accepted: its branch merged and the grace period passed with no new turn and no hold"}); err != nil {
			return err
		}
		s.ledgerChanged(tx, taskID)
		return nil
	})
}
