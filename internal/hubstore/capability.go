package hubstore

import (
	"errors"
	"strings"
	"time"
)

// CapabilityRow is one stored result of one probe on one room.
type CapabilityRow struct {
	Room    string `json:"room"`
	Cap     string `json:"cap"`
	ListVer int    `json:"list_ver"`
	At      string `json:"at"`
	State   string `json:"state"`
	Version string `json:"version,omitempty"`
	Reason  string `json:"reason,omitempty"`
	MS      int64  `json:"ms,omitempty"`
}

// CapabilityKeep is how long history is kept.
const CapabilityKeep = 30 * 24 * time.Hour

// CapabilityPut appends results. A result identical to the newest for its room and cap only moves that row's time, so a
// steady room does not grow the table every tick.
func (s *Store) CapabilityPut(rows []CapabilityRow) error {
	for i := range rows {
		rows[i].Room, rows[i].Cap = strings.TrimSpace(rows[i].Room), strings.TrimSpace(rows[i].Cap)
		switch rows[i].State {
		case "pass", "fail", "unknown":
		default:
			return refuse(errors.New("a capability result is pass, fail or unknown"))
		}
		if rows[i].Room == "" || rows[i].Cap == "" {
			return refuse(errors.New("a capability result needs a room and a capability"))
		}
	}
	cut := ts(s.now().Add(-CapabilityKeep))
	return s.guard(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, r := range rows {
			var id int64
			var state, version, reason, at string
			err := tx.QueryRow(`SELECT id, state, version, reason, at FROM capability_result WHERE room = ? AND cap = ?
				ORDER BY id DESC LIMIT 1`, r.Room, r.Cap).Scan(&id, &state, &version, &reason, &at)
			if err == nil && state == r.State && version == r.Version && reason == r.Reason {
				if r.At > at {
					if _, err := tx.Exec(`UPDATE capability_result SET at = ?, ms = ?, list_ver = ? WHERE id = ?`,
						r.At, r.MS, r.ListVer, id); err != nil {
						return err
					}
				}
				continue
			}
			if _, err := tx.Exec(`INSERT INTO capability_result (room, cap, list_ver, at, state, version, reason, ms)
				VALUES (?,?,?,?,?,?,?,?)`, r.Room, r.Cap, r.ListVer, r.At, r.State, r.Version, r.Reason, r.MS); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`DELETE FROM capability_result WHERE at < ?`, cut); err != nil {
			return err
		}
		return tx.Commit()
	})
}

// CapabilityLatest is the newest row for every room and capability.
func (s *Store) CapabilityLatest() ([]CapabilityRow, error) {
	var out []CapabilityRow
	err := s.guard(func() error {
		out = out[:0]
		rs, err := s.db.Query(`SELECT room, cap, list_ver, at, state, version, reason, ms FROM capability_result
			WHERE id IN (SELECT MAX(id) FROM capability_result GROUP BY room, cap) ORDER BY room, cap`)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var r CapabilityRow
			if err := rs.Scan(&r.Room, &r.Cap, &r.ListVer, &r.At, &r.State, &r.Version, &r.Reason, &r.MS); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rs.Err()
	})
	return out, err
}
