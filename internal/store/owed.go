package store

import (
	"encoding/json"
	"strings"
	"time"
)

// Owed answers: a worker stopped owing its launcher one, and the open item that keeps it.
// See docs/rnd/long-turn-checkin-design.md section 11 and internal/daemon/owed.go.
//
// KEPT AS SETTINGS, one per worker, so a restart keeps every item and no event kind widens the
// event table's CHECK. The same posture as the held-notice read marker (heldnotices.go). The
// item's words are also held on the launcher's card as a notice, which is what
// `atrium_task` with `notices` reads. Reading that notice closes nothing.

// OwedItem is one worker that owes an answer, and where it is kept.
type OwedItem struct {
	// Worker is the worker's card, and WorkerWire its name.
	Worker     string `json:"worker"`
	WorkerWire string `json:"worker_wire"`
	// Host is the card the item is kept on: the launcher's, else the local orchestrator's, else
	// the worker's own (Orphan). Launcher is the card whose answer is owed, empty when there is none.
	Host     string `json:"host"`
	Launcher string `json:"launcher,omitempty"`
	Orphan   bool   `json:"orphan,omitempty"`
	// RemoteLauncher is `name@room` when the launcher is on another room: the item is kept here as
	// an orphan's is, but the push names who has not answered.
	RemoteLauncher string `json:"remote_launcher,omitempty"`
	// Reason is `ended`, `asked`, `stopped` or `permission`.
	Reason string `json:"reason"`
	// Status is the worker's status when it opened, for the listing line.
	Status string    `json:"status"`
	Text   string    `json:"text,omitempty"`
	Since  time.Time `json:"since"`
	// Pushed is when the orchestrator was told, zero before. One push per item.
	Pushed time.Time `json:"pushed,omitempty"`
}

const (
	owedPrefix  = "owed:"
	askPrefix   = "owed_ask:"
	closePrefix = "owed_closed:"
)

// OwedItemOf is the open item for a worker, nil when there is none.
func (s *Store) OwedItemOf(workerID string) (*OwedItem, error) {
	raw, err := s.Setting(owedPrefix + workerID)
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil, err
	}
	var it OwedItem
	if json.Unmarshal([]byte(raw), &it) != nil {
		return nil, nil
	}
	return &it, nil
}

// PutOwedItem writes an item, opening it or recording a push.
func (s *Store) PutOwedItem(it OwedItem) error {
	raw, err := json.Marshal(it)
	if err != nil {
		return err
	}
	return s.SetSetting(owedPrefix+it.Worker, string(raw))
}

// CloseOwedItem drops a worker's item, stamps the close so the same reason cannot reopen it,
// and reports whether there was one.
func (s *Store) CloseOwedItem(workerID string) (bool, error) {
	it, err := s.OwedItemOf(workerID)
	if err != nil || it == nil {
		return false, err
	}
	if err := s.SetSetting(owedPrefix+workerID, ""); err != nil {
		return false, err
	}
	raw, _ := json.Marshal(OwedClosed{At: now(), Reason: it.Reason, Since: it.Since})
	return true, s.SetSetting(closePrefix+workerID, string(raw))
}

// OwedClosed is what is remembered of a worker's last closed item, so the same reason cannot
// reopen it: a status change moves a card's last activity, and that is not a new reason.
type OwedClosed struct {
	At     time.Time `json:"at"`
	Reason string    `json:"reason"`
	Since  time.Time `json:"since"`
}

// OwedClosedOf is a worker's last closed item, nil if none.
func (s *Store) OwedClosedOf(workerID string) *OwedClosed {
	raw, _ := s.Setting(closePrefix + workerID)
	if raw == "" {
		return nil
	}
	var c OwedClosed
	if json.Unmarshal([]byte(raw), &c) != nil {
		return nil
	}
	return &c
}

const reportedPrefix = "owed_reported:"

// SetOwedReported records that a report of the worker's went to the orchestrator because it had
// no launcher. That report IS the item: it must not open another.
func (s *Store) SetOwedReported(workerID string) error {
	return s.SetSetting(reportedPrefix+workerID, ts(now()))
}

// OwedReportedAt is when that was, zero if never.
func (s *Store) OwedReportedAt(workerID string) time.Time {
	raw, _ := s.Setting(reportedPrefix + workerID)
	if at, err := parseTS(raw); err == nil {
		return at
	}
	return time.Time{}
}

// OpenOwedItems is every open item, oldest first.
func (s *Store) OpenOwedItems() ([]OwedItem, error) {
	var raws []string
	err := s.guard(func() error {
		raws = nil
		rows, err := s.db.Query(`SELECT value FROM setting WHERE key LIKE 'owed:%' AND value != '' ORDER BY key`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				return err
			}
			raws = append(raws, v)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	var out []OwedItem
	for _, r := range raws {
		var it OwedItem
		if json.Unmarshal([]byte(r), &it) == nil {
			out = append(out, it)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Since.Before(out[j-1].Since); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

// OwedAsk is a worker's last question to its launcher that the launcher has not answered.
type OwedAsk struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

// SetOwedAsk records that a worker asked its launcher something.
func (s *Store) SetOwedAsk(workerID, text string) error {
	raw, _ := json.Marshal(OwedAsk{At: now(), Text: text})
	return s.SetSetting(askPrefix+workerID, string(raw))
}

// ClearOwedAsk records that the question was answered or withdrawn.
func (s *Store) ClearOwedAsk(workerID string) error {
	if raw, _ := s.Setting(askPrefix + workerID); raw == "" {
		return nil
	}
	return s.SetSetting(askPrefix+workerID, "")
}

// OwedAskOf is the worker's unanswered question, nil when there is none.
func (s *Store) OwedAskOf(workerID string) *OwedAsk {
	raw, _ := s.Setting(askPrefix + workerID)
	if raw == "" {
		return nil
	}
	var a OwedAsk
	if json.Unmarshal([]byte(raw), &a) != nil {
		return nil
	}
	return &a
}
