package hubstore

import (
	"database/sql"
	"errors"
	"strings"
)

// WORK: THE ITEM IS THE UNIT OF WORK, one per backlog id, with every card that touches it attached. The `item` table
// (item.go) is one card's run of it, an assignment. See docs/rnd/item-lifecycle-design.md, stage L1.
//
//   - ONE WORK ROW PER BACKLOG ID. A second launch on the same id attaches another card to the same row and never makes
//     a second one.
//   - AN ATTACH IS A SIDE EFFECT OF A PUSH. The room's item push carries the backlog id, so it rides the outbox the
//     assignment already rides, and a replay is the same `item_event` no-op as any other replay.
//   - AN ASSIGNMENT WITH NO BACKLOG ID IS `adhoc-<item_id>`: everything pushed before this stage, and anything a room
//     older than this pushes.

// AdhocPrefix starts the work id of an assignment that no backlog item named.
const AdhocPrefix = "adhoc-"

// AdhocWorkID is the work id of an assignment that named no backlog item.
func AdhocWorkID(itemID string) string { return AdhocPrefix + strings.TrimSpace(itemID) }

// Work is one item: a backlog item at work.
type Work struct {
	BacklogID string `json:"backlog_id"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	Dept      string `json:"dept,omitempty"`
	Priority  string `json:"priority,omitempty"`
	Kind      string `json:"kind"`
	OwnerKind string `json:"owner_kind,omitempty"`
	Owner     string `json:"owner,omitempty"`
	// Branch is the build branch, once one exists.
	Branch    string `json:"branch,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// WorkCard is one card attached to an item. Role and Step are the launch's own words, empty until a stage names them.
type WorkCard struct {
	BacklogID  string `json:"backlog_id"`
	Room       string `json:"room"`
	Card       string `json:"card"`
	Role       string `json:"role,omitempty"`
	Step       string `json:"step,omitempty"`
	AttachedAt string `json:"attached_at"`
}

// ErrNoWork is a backlog id nothing has been attached to.
var ErrNoWork = errors.New("no such item")

// ensureWork makes the work row for id, copying the backlog item's words when the hub holds that item, and refreshing
// them when it does. A backlog id the hub does not hold still gets a row, titled with the id: an attach is never refused
// for it, because a push that is refused is retried forever.
func ensureWork(t *sql.Tx, id, now string) error {
	res, err := t.Exec(`INSERT INTO work (backlog_id, title, body, dept, priority, kind, created_at, updated_at)
		SELECT id, title, body, dept, priority, 'code', ?, ? FROM backlog_item WHERE id = ?
		ON CONFLICT (backlog_id) DO UPDATE SET title = excluded.title, body = excluded.body, dept = excluded.dept,
			priority = excluded.priority, updated_at = excluded.updated_at`, now, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = t.Exec(`INSERT OR IGNORE INTO work (backlog_id, title, kind, created_at, updated_at) VALUES (?, ?, 'code', ?, ?)`,
		id, id, now, now)
	return err
}

// attachWork puts a card on an item, once. The same card again changes nothing.
func attachWork(t *sql.Tx, id, room, card, step, now string) error {
	if err := ensureWork(t, id, now); err != nil {
		return err
	}
	_, err := t.Exec(`INSERT OR IGNORE INTO work_card (backlog_id, room, card, role, step, attached_at) VALUES (?, ?, ?, '', ?, ?)`,
		id, fold(room), card, step, now)
	return err
}

// setCardStep names the step of a card that was attached without one, once. A step a card already has never changes.
func setCardStep(t *sql.Tx, id, room, card, step string) error {
	if step == "" {
		return nil
	}
	_, err := t.Exec(`UPDATE work_card SET step = ? WHERE backlog_id = ? AND room = ? AND card = ? AND step = ''`,
		step, id, fold(room), card)
	return err
}

const workCols = `backlog_id, title, body, dept, priority, kind, owner_kind, owner, branch, created_at, updated_at`

func scanWork(sc scanner) (Work, error) {
	var w Work
	err := sc.Scan(&w.BacklogID, &w.Title, &w.Body, &w.Dept, &w.Priority, &w.Kind, &w.OwnerKind, &w.Owner, &w.Branch,
		&w.CreatedAt, &w.UpdatedAt)
	return w, err
}

// WorkByID reads one item, or ErrNoWork.
func (s *Store) WorkByID(id string) (Work, error) {
	var w Work
	err := s.guard(func() error {
		var err error
		w, err = scanWork(s.db.QueryRow(`SELECT `+workCols+` FROM work WHERE backlog_id = ?`, strings.TrimSpace(id)))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Work{}, ErrNoWork
	}
	return w, err
}

// WorkCards is every card attached to an item, in the order they were attached.
func (s *Store) WorkCards(id string) ([]WorkCard, error) {
	var out []WorkCard
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT backlog_id, room, card, role, step, attached_at FROM work_card
			WHERE backlog_id = ? ORDER BY attached_at, room, card`, strings.TrimSpace(id))
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

// WorkOfCard is the item a card is attached to, the newest attachment when it is on several. ErrNoWork when it is on none.
func (s *Store) WorkOfCard(room, card string) (Work, error) {
	var w Work
	err := s.guard(func() error {
		var err error
		w, err = scanWork(s.db.QueryRow(`SELECT `+prefixed("w", workCols)+` FROM work w JOIN work_card c ON c.backlog_id = w.backlog_id
			WHERE c.room = ? AND c.card = ? ORDER BY c.attached_at DESC LIMIT 1`, fold(room), strings.TrimSpace(card)))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Work{}, ErrNoWork
	}
	return w, err
}

// prefixed is a column list with a table alias in front of each name.
func prefixed(alias, cols string) string {
	parts := strings.Split(cols, ",")
	for i, c := range parts {
		parts[i] = alias + "." + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}

// WorkList is every item, newest change first, at most limit of them.
func (s *Store) WorkList(limit int) ([]Work, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	var out []Work
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT `+workCols+` FROM work ORDER BY updated_at DESC, backlog_id LIMIT ?`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			w, err := scanWork(rows)
			if err != nil {
				return err
			}
			out = append(out, w)
		}
		return rows.Err()
	})
	return out, err
}

// WorkAssignments is the assignments that belong to an item, oldest first.
func (s *Store) WorkAssignments(id string) ([]Item, error) {
	var out []Item
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT `+atItemCols+` FROM item WHERE work_id = ? ORDER BY created_at, item_id`,
			strings.TrimSpace(id))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			it, err := scanAtItem(rows)
			if err != nil {
				return err
			}
			out = append(out, it)
		}
		return rows.Err()
	})
	return out, err
}
