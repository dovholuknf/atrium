package store

import (
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

// MarkNoticesRead records that a card read its held notices just now, and reports whether
// that moved the count: a card with none unread changes nothing.
func (s *Store) MarkNoticesRead(taskID string) (bool, error) {
	n, _, err := s.HeldNoticeStats(taskID)
	if err != nil {
		return false, err
	}
	if err := s.SetSetting(NoticesReadKey(taskID), ts(now())); err != nil {
		return false, err
	}
	return n > 0, nil
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
