package store

import (
	"strings"
	"time"
)

// The unexpected-exit notice rides the restart wake's row. See
// docs/runtime/unexpected-exit-wake.md.
//
// ONE ROW PER CARD IS THE PRECEDENCE RULE. A card that queued its own wake keeps
// it, and a card that already has a notice waiting does not get a second one, so
// a crash loop leaves one line to type however many times the room goes down
// before the runner is back.

// UnexpectedExitBy is the `queued_by` a notice's row carries, and the `by` and
// `from` of its events, so the notice and a self-queued wake stay apart.
const UnexpectedExitBy = "unexpected-exit"

// SettingUnexpectedExit switches the notice off with `off`. Anything else, unset
// included, is on.
const SettingUnexpectedExit = "unexpected_exit_wake"

// SettingRoomStopped is when this room last stopped on purpose, written by the
// wind-down and cleared by the next start. A start that finds it empty follows a
// crash or a kill.
const SettingRoomStopped = "room_stopped_at"

// UnexpectedExitOn reports whether the notice is on. A read failure answers on:
// the notice only ever resumes work, so on is the default to fall back to.
func (s *Store) UnexpectedExitOn() bool {
	v, err := s.Setting(SettingUnexpectedExit)
	if err != nil {
		return true
	}
	return !strings.EqualFold(strings.TrimSpace(v), "off")
}

// MarkRoomStopped records a planned stop.
func (s *Store) MarkRoomStopped(at time.Time) error {
	return s.SetSetting(SettingRoomStopped, ts(at))
}

// TakeRoomStopped reads and clears the planned-stop mark. ok is false when there
// was none, which means the last room did not get to its wind-down.
func (s *Store) TakeRoomStopped() (at time.Time, ok bool, err error) {
	v, err := s.Setting(SettingRoomStopped)
	if err != nil || strings.TrimSpace(v) == "" {
		return time.Time{}, false, err
	}
	if err := s.SetSetting(SettingRoomStopped, ""); err != nil {
		return time.Time{}, false, err
	}
	at, err = parseTS(v)
	if err != nil {
		// A mark that will not parse still says the stop was planned.
		return time.Time{}, true, nil
	}
	return at, true, nil
}

// QueueUnexpectedExit queues the notice on a card unless the card already has a
// row, a self-queued wake or an earlier notice. Reports whether it queued.
func (s *Store) QueueUnexpectedExit(taskID, text string) (*RestartWake, bool, error) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > MaxRestartWake {
		return nil, false, ErrWakeText
	}
	var w *RestartWake
	err := s.inTx(func(tx *Tx) error {
		w = &RestartWake{TaskID: taskID, Text: text, By: UnexpectedExitBy, QueuedAt: now()}
		res, err := tx.Exec(`INSERT INTO restart_wake (task_id, text, queued_by, queued_at, expires_at, expired_at)
			SELECT ?, ?, ?, ?, ?, NULL WHERE EXISTS (SELECT 1 FROM task WHERE id = ?)
			ON CONFLICT (task_id) DO NOTHING`,
			taskID, w.Text, w.By, ts(w.QueuedAt), ts(w.QueuedAt), taskID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			w = nil
			return nil
		}
		_, err = s.appendEventOn(tx, taskID, EventNotified, map[string]any{
			"by": UnexpectedExitBy, "what": "queued", "text": w.Text,
		})
		return err
	})
	if err != nil {
		return nil, false, err
	}
	return w, w != nil, nil
}
