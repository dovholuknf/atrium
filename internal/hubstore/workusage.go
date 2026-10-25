package hubstore

import (
	"strings"

	"github.com/dovholuknf/atrium/internal/base/tokenprice"
)

// WHAT AN ITEM'S CARDS SPENT, AS THE HUB INGESTED IT (item lifecycle L5, docs/rnd/item-lifecycle-design.md section 6).
//
//   - A ROW IS KEPT ONCE. The key is the room, the room's own usage id and the item, so a pull that is repeated, or one
//     that overlaps the last, inserts nothing. The item is in the key because a card may be on two items and an
//     orchestrator turn may be about three.
//   - TOKENS ONLY. There is no dollar column: a cost is worked out from the counts and the `price` table when it is read.
//   - A DELIVERY'S TOKENS CAN GROW after the hub first read them (a turn costed again after a mid-turn flush), so a row
//     from a delivery is updated when its counts change. A usage row never is.

// Usage classes: a step's spend, or what coordinating the item cost (a relay's turns, the orchestrator's answers).
const (
	ClassStep  = "step"
	ClassCoord = "coord"
)

// WorkUsageRow is one room row as it belongs to one item.
type WorkUsageRow struct {
	Room      string `json:"room"`
	UsageID   string `json:"usage_id"`
	BacklogID string `json:"backlog_id"`
	// Card is the room's card id the spend was on, or the one that sent a delivery.
	Card string `json:"card"`
	// Class is `step` or `coord`.
	Class string `json:"class"`
	// Source is `usage` for a card's own turn, `delivery` for an orchestrator turn an item's card caused.
	Source    string `json:"source"`
	Model     string `json:"model"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at"`
	tokenprice.Tokens
}

// Prices is the price table, every row.
func (s *Store) Prices() (tokenprice.Table, error) {
	var tb tokenprice.Table
	err := s.guard(func() error {
		tb = tokenprice.Table{}
		rows, err := s.db.Query(`SELECT model, valid_from, input, cache_write_5m, cache_write_1h, cache_read, output
			FROM price ORDER BY model, valid_from`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p tokenprice.Price
			if err := rows.Scan(&p.Model, &p.ValidFrom, &p.Input, &p.CacheWrite5m, &p.CacheWrite1h, &p.CacheRead, &p.Output); err != nil {
				return err
			}
			tb.Rows = append(tb.Rows, p)
		}
		return rows.Err()
	})
	return tb, err
}

// PutPrice adds a price row, or replaces the one for that model and time. A model is stored by the name the matcher reads.
func (s *Store) PutPrice(p tokenprice.Price) error {
	p.Model = tokenprice.Normal(p.Model)
	return s.guard(func() error {
		_, err := s.db.Exec(`INSERT INTO price (model, valid_from, input, cache_write_5m, cache_write_1h, cache_read, output)
			VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT (model, valid_from) DO UPDATE SET input = excluded.input,
			cache_write_5m = excluded.cache_write_5m, cache_write_1h = excluded.cache_write_1h,
			cache_read = excluded.cache_read, output = excluded.output`,
			p.Model, p.ValidFrom, p.Input, p.CacheWrite5m, p.CacheWrite1h, p.CacheRead, p.Output)
		return err
	})
}

// UsageCursor is where the last pull of a room reached, and when it ran. ok is false for a room never pulled.
func (s *Store) UsageCursor(room string) (since, pulledAt string, ok bool, err error) {
	err = s.guard(func() error {
		since, pulledAt, ok = "", "", false
		rows, err := s.db.Query(`SELECT since, pulled_at FROM work_usage_cursor WHERE room = ?`, fold(room))
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			ok = true
			return rows.Scan(&since, &pulledAt)
		}
		return rows.Err()
	})
	return
}

// SetUsageCursor remembers how far a room's rows have been read.
func (s *Store) SetUsageCursor(room, since, pulledAt string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`INSERT INTO work_usage_cursor (room, since, pulled_at) VALUES (?, ?, ?)
			ON CONFLICT (room) DO UPDATE SET since = excluded.since, pulled_at = excluded.pulled_at`, fold(room), since, pulledAt)
		return err
	})
}

// CardsOnRoom is every card of that room attached to any item.
func (s *Store) CardsOnRoom(room string) ([]WorkCard, error) {
	var out []WorkCard
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT backlog_id, room, card, role, step, attached_at FROM work_card WHERE room = ?
			ORDER BY attached_at, backlog_id`, fold(room))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c WorkCard
			if err := rows.Scan(&c.BacklogID, &c.Room, &c.Card, &c.Role, &c.Step, &c.AttachedAt); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

// AddWorkUsage keeps rows, once each, and says how many it wrote or changed. A usage row already there is left alone; a
// delivery row is updated only when its counts differ, so a replay is zero.
func (s *Store) AddWorkUsage(rows []WorkUsageRow) (int, error) {
	n := 0
	err := s.guard(func() error {
		n = 0
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, r := range rows {
			if r.Class != ClassCoord {
				r.Class = ClassStep
			}
			if r.Source != "delivery" {
				r.Source = "usage"
			}
			res, err := tx.Exec(`INSERT INTO work_usage (room, usage_id, backlog_id, card, class, source, model, started_at, ended_at,
					input, cache_write_5m, cache_write_1h, cache_read, output) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (room, usage_id, backlog_id) DO UPDATE SET model = excluded.model, input = excluded.input,
					cache_write_5m = excluded.cache_write_5m, cache_write_1h = excluded.cache_write_1h,
					cache_read = excluded.cache_read, output = excluded.output
				WHERE work_usage.source = 'delivery' AND (work_usage.input <> excluded.input OR
					work_usage.cache_write_5m <> excluded.cache_write_5m OR work_usage.cache_write_1h <> excluded.cache_write_1h OR
					work_usage.cache_read <> excluded.cache_read OR work_usage.output <> excluded.output OR
					work_usage.model <> excluded.model)`,
				fold(r.Room), r.UsageID, strings.TrimSpace(r.BacklogID), r.Card, r.Class, r.Source, r.Model, r.StartedAt, r.EndedAt,
				r.Input, r.CacheWrite5m, r.CacheWrite1h, r.CacheRead, r.Output)
			if err != nil {
				return err
			}
			c, _ := res.RowsAffected()
			n += int(c)
		}
		return tx.Commit()
	})
	return n, err
}

const workUsageCols = `room, usage_id, backlog_id, card, class, source, model, started_at, ended_at,
	input, cache_write_5m, cache_write_1h, cache_read, output`

// WorkUsage is every row of one item, oldest first.
func (s *Store) WorkUsage(id string) ([]WorkUsageRow, error) {
	m, err := s.workUsage(`WHERE backlog_id = ?`, strings.TrimSpace(id))
	return m[strings.TrimSpace(id)], err
}

// AllWorkUsage is every row of every item, by backlog id, so a list is one query.
func (s *Store) AllWorkUsage() (map[string][]WorkUsageRow, error) { return s.workUsage(``) }

func (s *Store) workUsage(where string, args ...any) (map[string][]WorkUsageRow, error) {
	out := map[string][]WorkUsageRow{}
	err := s.guard(func() error {
		out = map[string][]WorkUsageRow{}
		rows, err := s.db.Query(`SELECT `+workUsageCols+` FROM work_usage `+where+` ORDER BY ended_at, room, usage_id`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r WorkUsageRow
			if err := rows.Scan(&r.Room, &r.UsageID, &r.BacklogID, &r.Card, &r.Class, &r.Source, &r.Model, &r.StartedAt,
				&r.EndedAt, &r.Input, &r.CacheWrite5m, &r.CacheWrite1h, &r.CacheRead, &r.Output); err != nil {
				return err
			}
			out[r.BacklogID] = append(out[r.BacklogID], r)
		}
		return rows.Err()
	})
	return out, err
}
