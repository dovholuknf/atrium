package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// The after-restart wake: "when you come back, say this". See
// docs/restart-wake.md.
//
// A restart ends every terminal the room owns, and a resumed session comes back
// idle. A wake is one prompt, queued before the restart by the session itself or
// by a script a human runs, and typed into the card's terminal once when the
// runner is back. The daemon decides when that is. This file only keeps the row,
// and records each change to it on the card in the same transaction.
//
// ONE PER CARD. A newer wake replaces an older one, because the newer one is
// what the caller means now.

// MaxRestartWake bounds a wake's text. It is a prompt to pick work back up, not a
// briefing: a longer one belongs in a file the prompt names.
const MaxRestartWake = 2000

// RestartWakeBy is the `by` every wake event carries, so the timeline can be
// filtered to this feature.
const RestartWakeBy = "restart-wake"

// RestartWake is one card's queued wake.
type RestartWake struct {
	TaskID    string    `json:"task_id"`
	Text      string    `json:"text"`
	By        string    `json:"by,omitempty"`
	QueuedAt  time.Time `json:"queued_at"`
	ExpiresAt time.Time `json:"expires_at"`
	// ExpiredAt is set when the card's runner did not come back before
	// ExpiresAt. The row stays so the card can show it. Nil while it waits.
	ExpiredAt *time.Time `json:"expired_at,omitempty"`
}

// ErrWakeText is a wake with no text, or with too much.
var ErrWakeText = errors.New("a wake needs some text, at most 2000 characters")

// SetRestartWake queues a wake for a card, replacing any it already has, and
// returns the new row and the one it replaced, if any. A card that does not
// exist answers sql.ErrNoRows.
func (s *Store) SetRestartWake(taskID, text, by string, ttl time.Duration) (*RestartWake, *RestartWake, error) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > MaxRestartWake {
		return nil, nil, ErrWakeText
	}
	var w, old *RestartWake
	err := s.inTx(func(tx *Tx) error {
		w, old = nil, nil
		var one int
		if err := tx.QueryRow(`SELECT 1 FROM task WHERE id = ?`, taskID).Scan(&one); err != nil {
			return err
		}
		prev, err := restartWakeOn(tx, taskID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if prev != nil && prev.ExpiredAt == nil {
			old = prev
		}
		at := now()
		w = &RestartWake{TaskID: taskID, Text: text, By: strings.TrimSpace(by), QueuedAt: at, ExpiresAt: at.Add(ttl)}
		if _, err := tx.Exec(`INSERT INTO restart_wake (task_id, text, queued_by, queued_at, expires_at, expired_at)
			VALUES (?, ?, ?, ?, ?, NULL)
			ON CONFLICT (task_id) DO UPDATE SET text = excluded.text, queued_by = excluded.queued_by,
				queued_at = excluded.queued_at, expires_at = excluded.expires_at, expired_at = NULL`,
			taskID, w.Text, w.By, ts(w.QueuedAt), ts(w.ExpiresAt)); err != nil {
			return err
		}
		what := "queued"
		if old != nil {
			what = "replaced"
		}
		_, err = s.appendEventOn(tx, taskID, EventNotified, map[string]any{
			"by": RestartWakeBy, "what": what, "text": w.Text, "queued_by": w.By,
			"expires_at": ts(w.ExpiresAt),
		})
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return w, old, nil
}

// RestartWakes lists every wake, waiting or expired.
func (s *Store) RestartWakes() ([]*RestartWake, error) {
	var out []*RestartWake
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT task_id, text, queued_by, queued_at, expires_at, expired_at
			FROM restart_wake ORDER BY queued_at`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			w, err := scanRestartWake(rows)
			if err != nil {
				return err
			}
			out = append(out, w)
		}
		return rows.Err()
	})
	return out, err
}

// TakeRestartWake deletes a wake that has just been typed in, and records the
// prompt on the card. Only the wake queued at `queuedAt`: one that replaced it in
// the meantime is a different wake and stays. Reports whether a row went.
func (s *Store) TakeRestartWake(taskID string, queuedAt time.Time) (bool, error) {
	took := false
	err := s.inTx(func(tx *Tx) error {
		took = false
		w, err := restartWakeOn(tx, taskID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if !w.QueuedAt.Equal(queuedAt) || w.ExpiredAt != nil {
			return nil
		}
		if _, err := tx.Exec(`DELETE FROM restart_wake WHERE task_id = ?`, taskID); err != nil {
			return err
		}
		took = true
		_, err = s.appendEventOn(tx, taskID, EventPrompted, map[string]any{
			"text": w.Text, "via": "terminal", "from": RestartWakeBy, "queued_by": w.By,
			"queued_at": ts(w.QueuedAt),
		})
		return err
	})
	return took, err
}

// ExpireRestartWake marks a wake as never delivered, and records that on the
// card. The row stays, so the card shows it until the wake is cleared or
// replaced. Only the wake queued at `queuedAt`, for the reason TakeRestartWake
// gives.
func (s *Store) ExpireRestartWake(taskID string, queuedAt, at time.Time) (bool, error) {
	done := false
	err := s.inTx(func(tx *Tx) error {
		done = false
		w, err := restartWakeOn(tx, taskID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if !w.QueuedAt.Equal(queuedAt) || w.ExpiredAt != nil {
			return nil
		}
		if _, err := tx.Exec(`UPDATE restart_wake SET expired_at = ? WHERE task_id = ?`, ts(at), taskID); err != nil {
			return err
		}
		done = true
		_, err = s.appendEventOn(tx, taskID, EventNotified, map[string]any{
			"by": RestartWakeBy, "what": "expired", "text": w.Text, "queued_by": w.By,
			"queued_at": ts(w.QueuedAt),
		})
		return err
	})
	return done, err
}

// ClearRestartWake removes a card's wake, waiting or expired, and records it.
// `forgotten` says it was an expired one aging off rather than somebody
// cancelling it. Reports whether there was one.
func (s *Store) ClearRestartWake(taskID, by string, forgotten bool) (bool, error) {
	gone := false
	err := s.inTx(func(tx *Tx) error {
		gone = false
		w, err := restartWakeOn(tx, taskID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM restart_wake WHERE task_id = ?`, taskID); err != nil {
			return err
		}
		gone = true
		if forgotten {
			return nil
		}
		_, err = s.appendEventOn(tx, taskID, EventNotified, map[string]any{
			"by": RestartWakeBy, "what": "cleared", "text": w.Text, "cleared_by": strings.TrimSpace(by),
		})
		return err
	})
	return gone, err
}

func restartWakeOn(q querier, taskID string) (*RestartWake, error) {
	return scanRestartWake(q.QueryRow(`SELECT task_id, text, queued_by, queued_at, expires_at, expired_at
		FROM restart_wake WHERE task_id = ?`, taskID))
}

type rowScanner interface{ Scan(dest ...any) error }

func scanRestartWake(r rowScanner) (*RestartWake, error) {
	var (
		w               RestartWake
		queued, expires string
		expired         sql.NullString
	)
	if err := r.Scan(&w.TaskID, &w.Text, &w.By, &queued, &expires, &expired); err != nil {
		return nil, err
	}
	var err error
	if w.QueuedAt, err = parseTS(queued); err != nil {
		return nil, err
	}
	if w.ExpiresAt, err = parseTS(expires); err != nil {
		return nil, err
	}
	if expired.Valid && expired.String != "" {
		t, err := parseTS(expired.String)
		if err != nil {
			return nil, err
		}
		w.ExpiredAt = &t
	}
	return &w, nil
}
