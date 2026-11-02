package hubstore

import (
	"database/sql"
	"errors"
	"fmt"
)

// An epoch is one round of clint fetching claude/main, re-signing and pushing. See migration 0016.
//
//   - ONE EPOCH IS OPEN AT A TIME. Opening records the sha clint fetched. claude/main keeps moving while it is open:
//     nothing is frozen, and nothing that lands is lost, because closing replays what is not in main.
//   - CLOSING RECORDS WHAT CLAUDE/MAIN WAS AND IS. The hub's repository keeps refs/atrium/epochs/<n> at the old tip.

// ErrEpochOpen is an opening while one is open. ErrNoEpoch is a closing with none to close.
var (
	ErrEpochOpen = errors.New("an epoch is already open")
	ErrNoEpoch   = errors.New("no epoch is open")
)

// FlowEpoch is one round.
type FlowEpoch struct {
	N          int64  `json:"n"`
	State      string `json:"state"`
	FetchedSHA string `json:"fetched_sha"`
	OldTip     string `json:"old_tip,omitempty"`
	NewTip     string `json:"new_tip,omitempty"`
	OpenedAt   string `json:"opened_at"`
	OpenedBy   string `json:"opened_by"`
	ClosedAt   string `json:"closed_at,omitempty"`
	ClosedBy   string `json:"closed_by,omitempty"`
}

const epochCols = `n, state, fetched_sha, old_tip, new_tip, opened_at, opened_by, closed_at, closed_by`

func scanEpoch(sc rowScanner) (FlowEpoch, error) {
	var e FlowEpoch
	err := sc.Scan(&e.N, &e.State, &e.FetchedSHA, &e.OldTip, &e.NewTip, &e.OpenedAt, &e.OpenedBy, &e.ClosedAt, &e.ClosedBy)
	return e, err
}

// EpochOpen records that the operator fetched claude/main at sha.
func (s *Store) EpochOpen(sha, by string) (FlowEpoch, error) {
	if !flowShaRe.MatchString(sha) {
		return FlowEpoch{}, refuse(errors.New("a sha is 7 to 64 lower case hex digits"))
	}
	if why := CRText("opener", by, backlogByMax, false); why != "" {
		return FlowEpoch{}, refuse(errors.New(why))
	}
	var out FlowEpoch
	err := s.tx(func(t *sql.Tx) error {
		var open int64
		switch err := t.QueryRow(`SELECT n FROM flow_epoch WHERE state = 'open'`).Scan(&open); {
		case err == nil:
			return refuse(fmt.Errorf("%w: epoch %d", ErrEpochOpen, open))
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		var top int64
		if err := t.QueryRow(`SELECT COALESCE(MAX(n), 0) FROM flow_epoch`).Scan(&top); err != nil {
			return err
		}
		if _, err := t.Exec(`INSERT INTO flow_epoch (n, state, fetched_sha, opened_at, opened_by) VALUES (?, 'open', ?, ?, ?)`,
			top+1, sha, ts(s.now()), by); err != nil {
			return err
		}
		var err error
		out, err = scanEpoch(t.QueryRow(`SELECT `+epochCols+` FROM flow_epoch WHERE n = ?`, top+1))
		return err
	})
	return out, err
}

// EpochOpenNow is the open epoch, or ErrNoEpoch.
func (s *Store) EpochOpenNow() (FlowEpoch, error) {
	var e FlowEpoch
	err := s.guard(func() error {
		var err error
		e, err = scanEpoch(s.db.QueryRow(`SELECT ` + epochCols + ` FROM flow_epoch WHERE state = 'open'`))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNoEpoch
	}
	return e, err
}

// EpochClose records the open epoch n as closed, with the tip claude/main was and the tip it is.
func (s *Store) EpochClose(n int64, oldTip, newTip, by string) (FlowEpoch, error) {
	if !flowShaRe.MatchString(oldTip) || !flowShaRe.MatchString(newTip) {
		return FlowEpoch{}, refuse(errors.New("a sha is 7 to 64 lower case hex digits"))
	}
	if why := CRText("closer", by, backlogByMax, false); why != "" {
		return FlowEpoch{}, refuse(errors.New(why))
	}
	var out FlowEpoch
	err := s.tx(func(t *sql.Tx) error {
		res, err := t.Exec(`UPDATE flow_epoch SET state = 'closed', old_tip = ?, new_tip = ?, closed_at = ?, closed_by = ?
			WHERE n = ? AND state = 'open'`, oldTip, newTip, ts(s.now()), by, n)
		if err != nil {
			return err
		}
		if c, _ := res.RowsAffected(); c == 0 {
			return refuse(ErrNoEpoch)
		}
		out, err = scanEpoch(t.QueryRow(`SELECT `+epochCols+` FROM flow_epoch WHERE n = ?`, n))
		return err
	})
	return out, err
}

// Epochs lists the latest rounds, newest first.
func (s *Store) Epochs(limit int) ([]FlowEpoch, error) {
	if limit <= 0 || limit > backlogListMax {
		limit = 20
	}
	out := []FlowEpoch{}
	err := s.guard(func() error {
		out = []FlowEpoch{}
		rows, err := s.db.Query(`SELECT ` + epochCols + fmt.Sprintf(` FROM flow_epoch ORDER BY n DESC LIMIT %d`, limit))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			e, err := scanEpoch(rows)
			if err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}
