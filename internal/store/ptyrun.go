package store

import (
	"errors"
	"strings"
	"time"
)

// One row per terminal start, so an exit can be filed exactly once. See
// docs/rnd/rolling-restart-design.md, section 3.2.
//
// The key is the START, not the card: a card that is started again gets a new
// run_id, so the exit of a later start is never taken for an earlier one. A pid
// is reused and two clocks disagree, which is why neither is the key.

const (
	// RunKindRunner is a card's runner, and RunKindShell is the plain shell
	// beside it. A card holds both at once, which is why a card id alone never
	// names a terminal.
	RunKindRunner = "runner"
	RunKindShell  = "shell"
)

// SettingPtyHost is `on` when new terminals should be started in a pty host
// rather than in this process. Unset is off. NOTHING READS IT YET beyond the
// one place a terminal is spawned, and that place has only the in-process path.
const SettingPtyHost = "pty_host"

// PtyHostOn reports whether the setting is on. A read failure answers off:
// the host path changes where every runner lives, so it is opt in.
func (s *Store) PtyHostOn() bool {
	v, err := s.Setting(SettingPtyHost)
	return err == nil && strings.EqualFold(strings.TrimSpace(v), "on")
}

// NewRunID makes a run id: random, 128 bits, the same shape as every other key.
func NewRunID() string { return newID() }

// PtyRun is one terminal start.
type PtyRun struct {
	RunID   string
	TaskID  string
	Kind    string
	Host    string // empty for a terminal in the daemon's own process
	Started time.Time
	Filed   bool
}

// RecordRun writes a start down. Idempotent on run_id, so a retry is harmless.
func (s *Store) RecordRun(r PtyRun) error {
	if r.RunID == "" || r.TaskID == "" {
		return errors.New("a run needs a run_id and a task_id")
	}
	if r.Kind != RunKindRunner && r.Kind != RunKindShell {
		return errors.New("a run is a runner or a shell")
	}
	if r.Started.IsZero() {
		r.Started = now()
	}
	return s.guard(func() error {
		_, err := s.db.Exec(
			`INSERT OR IGNORE INTO pty_run (run_id, task_id, kind, host, started_at, filed_at)
			 VALUES (?, ?, ?, ?, ?, '')`,
			r.RunID, r.TaskID, r.Kind, r.Host, ts(r.Started))
		return err
	})
}

// Run reads one row. sql.ErrNoRows when there is none.
func (s *Store) Run(runID string) (*PtyRun, error) {
	var r PtyRun
	var started, filed string
	err := s.guard(func() error {
		return s.db.QueryRow(
			`SELECT run_id, task_id, kind, host, started_at, filed_at FROM pty_run WHERE run_id = ?`,
			runID).Scan(&r.RunID, &r.TaskID, &r.Kind, &r.Host, &started, &filed)
	})
	if err != nil {
		return nil, err
	}
	if t, perr := time.Parse(time.RFC3339Nano, started); perr == nil {
		r.Started = t
	}
	r.Filed = filed != ""
	return &r, nil
}

// ExitFiling is everything one exit writes together.
type ExitFiling struct {
	TaskID string
	RunID  string
	// Kind names the row to create if the run was never recorded. Empty means
	// a runner.
	Kind string
	// Payload is the `exited` event. Nil files a run without an event, which is
	// what a shell's exit is: it belongs to nobody's history.
	Payload map[string]any
	// Why, when set, is written to the card in the same transaction.
	Why string
}

// FileExit records an exit AT MOST ONCE per (task_id, run_id) and reports
// whether this call was the one that did.
//
// The row's mark, the `why` and the `exited` event commit together, so a crash
// between them cannot file twice or lose the exit. A second call for the same
// pair changes nothing and returns false with no error. A run that was never
// recorded (a terminal a crashed daemon started before its row landed) gets a
// row here, already marked, so it too can only be filed once.
//
// An empty RunID is the one exception: it has no identity to be idempotent on,
// so it files every time, exactly as an exit did before runs had ids.
func (s *Store) FileExit(f ExitFiling) (bool, error) {
	filed := false
	err := s.inTx(func(tx *Tx) error {
		filed = false
		if f.RunID != "" {
			kind := f.Kind
			if kind == "" {
				kind = RunKindRunner
			}
			n := now()
			if _, err := tx.Exec(
				`INSERT OR IGNORE INTO pty_run (run_id, task_id, kind, host, started_at, filed_at)
				 VALUES (?, ?, ?, '', ?, '')`, f.RunID, f.TaskID, kind, ts(n)); err != nil {
				return err
			}
			res, err := tx.Exec(
				`UPDATE pty_run SET filed_at = ? WHERE run_id = ? AND task_id = ? AND filed_at = ''`,
				ts(n), f.RunID, f.TaskID)
			if err != nil {
				return err
			}
			if c, _ := res.RowsAffected(); c == 0 {
				return nil
			}
		}
		if f.Why != "" {
			if _, err := tx.Exec(`UPDATE task SET why = ?, last_activity_at = ? WHERE id = ?`,
				f.Why, ts(now()), f.TaskID); err != nil {
				return err
			}
		}
		if f.Payload != nil {
			e, err := s.appendEventOn(tx, f.TaskID, EventExited, f.Payload)
			if err != nil {
				return err
			}
			if err := s.ledgerOnSession(tx, f.TaskID, e); err != nil {
				return err
			}
		}
		filed = true
		return nil
	})
	return filed, err
}
