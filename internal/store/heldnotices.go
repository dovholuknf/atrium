package store

import (
	"encoding/json"
	"time"
)

// What a card that holds its notices has not read yet. See holdNotice in
// internal/daemon/a2a.go.
//
// DURABLE, AND NOT AN EVENT. A held notice is a `notified` event already. "Read" is one
// timestamp per card, kept as a setting so no new event kind widens the event table's
// CHECK. A held notice is unread when it is newer than that stamp.

// NoticesReadKey is the setting that holds when a card last read its held notices.
func NoticesReadKey(taskID string) string { return "notices_read:" + taskID }

// MarkNoticesRead records that a card read its held notices up to `through`, the `at` of the
// newest one it was handed, and reports whether that moved the count. NEVER NOW: a notice
// held between the read and this stamp is newer than `through`, so it stays unread. The
// marker only moves forward, so a late or repeated stamp cannot unread anything. A
// `through` that is empty or not an RFC3339 time changes nothing.
func (s *Store) MarkNoticesRead(taskID, through string) (bool, error) {
	// An event's `at` comes back from a JSON round trip as RFC3339Nano, which drops trailing
	// zeros, so it is parsed as that and rewritten in the store's fixed width.
	parsed, err := time.Parse(time.RFC3339Nano, through)
	if err != nil {
		return false, nil
	}
	through = ts(parsed)
	stored, err := s.Setting(NoticesReadKey(taskID))
	if err != nil {
		return false, err
	}
	n, _, err := s.HeldNoticeStats(taskID)
	if err != nil {
		return false, err
	}
	// Fixed-width UTC text, so the larger string is the later time.
	if through <= stored {
		return false, nil
	}
	if err := s.SetSetting(NoticesReadKey(taskID), through); err != nil {
		return false, err
	}
	after, _, err := s.HeldNoticeStats(taskID)
	if err != nil {
		return false, err
	}
	return after < n, nil
}

// HeldNotice is one unread held notice as the board lists it: when, who it is about and what it says.
type HeldNotice struct {
	At      string `json:"at"`
	Source  string `json:"source"`
	About   string `json:"about"`
	AboutID string `json:"about_card"`
	Text    string `json:"text"`
}

// UnreadHeldNotices are the notices HeldNoticeStats counts, oldest first, so the list the board opens and the number on
// the row come from the same rows. Newest 50 at most.
func (s *Store) UnreadHeldNotices(taskID string) ([]HeldNotice, error) {
	read, err := s.Setting(NoticesReadKey(taskID))
	if err != nil {
		return nil, err
	}
	var out []HeldNotice
	err = s.guard(func() error {
		rows, err := s.db.Query(`SELECT at, payload FROM (SELECT at, payload FROM event
			WHERE task_id = ? AND kind = ? AND at > ? AND payload LIKE '%"held":true%' ORDER BY at DESC LIMIT 50)
			ORDER BY at`, taskID, EventNotified, read)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var at, payload string
			if err := rows.Scan(&at, &payload); err != nil {
				return err
			}
			var p struct {
				Source  string `json:"source"`
				About   string `json:"about"`
				AboutID string `json:"about_card"`
				Text    string `json:"text"`
			}
			if json.Unmarshal([]byte(payload), &p) != nil {
				continue
			}
			out = append(out, HeldNotice{At: at, Source: p.Source, About: p.About, AboutID: p.AboutID, Text: p.Text})
		}
		return rows.Err()
	})
	return out, err
}

// HeldNoticeStats is how many held notices a card has not read, and when the oldest of
// them was held. Zero time when there are none.
func (s *Store) HeldNoticeStats(taskID string) (int, time.Time, error) {
	read, err := s.Setting(NoticesReadKey(taskID))
	if err != nil {
		return 0, time.Time{}, err
	}
	var (
		n      int
		oldest string
	)
	err = s.guard(func() error {
		// The marker is fixed-width UTC text, so comparing it as text is comparing times.
		// An empty one sorts before every event: nothing has been read.
		return s.db.QueryRow(`SELECT COUNT(*), COALESCE(MIN(at), '') FROM event
			WHERE task_id = ? AND kind = ? AND at > ? AND payload LIKE '%"held":true%'`,
			taskID, EventNotified, read).Scan(&n, &oldest)
	})
	if err != nil || n == 0 {
		return 0, time.Time{}, err
	}
	at, err := parseTS(oldest)
	if err != nil {
		return n, time.Time{}, nil
	}
	return n, at, nil
}
