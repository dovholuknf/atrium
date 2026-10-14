package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// THE ROOM'S MIRROR OF ITS ITEMS, AND THE OUTBOX THAT CARRIES THEM TO THE HUB. The hub owns items (internal/hubstore,
// item.go) and this is the room's copy: what it needs to type an unblock and to see a turn start, and what it owes the
// hub while the hub is not answering. See docs/changes/items-stage2a.md.
//
//   - EVERY CHANGE IS A REVISION and a revision is an outbox row keyed (item_id, revision, state). The row is written in
//     the SAME transaction as the mirror, so a crash cannot change an item and forget to say so.
//   - A ROW LEAVES ONLY WHEN THE HUB TOOK IT. A push that fails stays, with its attempts and the reason, and the hub
//     treats a replay of the same key as a no-op, so sending twice is always safe.
//   - THE STATE IS NOT WHAT A RUNNER IS DOING. running, blocked and finished are facts about the work, and they change
//     when atrium sees them change: a finish records blocked or finished, and only a turn that starts from an unblock
//     (or from the human typing) records running again.

// Item states, as the hub's.
const (
	ItemQueued   = "queued"
	ItemRunning  = "running"
	ItemBlocked  = "blocked"
	ItemFinished = "finished"
)

// Owner kinds, as the hub's.
const (
	ItemOwnerSession = "session"
	ItemOwnerRole    = "role"
	ItemOwnerHuman   = "human"
)

// ErrNoItem is an item id this room does not hold.
var ErrNoItem = errors.New("no such item")

// ItemRow is one item as this room mirrors it.
type ItemRow struct {
	ID         string
	TaskID     string
	Generation int
	State      string
	Revision   int
	OwnerKind  string
	// Owner is a handle for a session, a role name for a role, empty for the human.
	Owner     string
	CreatedBy string
	Note      string
	// UnblockMsg is the queued message that carries the owner's answer, and UnblockDelivered whether it has reached the
	// worker. The item is still blocked until a turn starts from it.
	UnblockMsg       string
	UnblockDelivered bool
	CreatedAt        string
	UpdatedAt        string
}

// ItemOwed is one outbox row: a push the hub has not taken.
type ItemOwed struct {
	ID       string
	Revision int
	State    string
	// Body is the JSON the hub's sync endpoint takes.
	Body      string
	Attempts  int
	LastError string
}

const itemRowCols = `item_id, task_id, generation, state, revision, owner_kind, owner, created_by, note, unblock_msg,
	unblock_delivered, created_at, updated_at`

func scanItemRow(sc interface{ Scan(...any) error }) (*ItemRow, error) {
	var it ItemRow
	var del int
	if err := sc.Scan(&it.ID, &it.TaskID, &it.Generation, &it.State, &it.Revision, &it.OwnerKind, &it.Owner, &it.CreatedBy,
		&it.Note, &it.UnblockMsg, &del, &it.CreatedAt, &it.UpdatedAt); err != nil {
		return nil, err
	}
	it.UnblockDelivered = del != 0
	return &it, nil
}

func validItemState(s string) bool {
	switch s {
	case ItemQueued, ItemRunning, ItemBlocked, ItemFinished:
		return true
	}
	return false
}

// pushBody is what the hub's sync endpoint takes. The room is the hub's to add, from the certificate.
func (it *ItemRow) pushBody() string {
	b, _ := json.Marshal(map[string]any{
		"item_id": it.ID, "card": it.TaskID, "generation": it.Generation, "state": it.State, "revision": it.Revision,
		"owner_kind": it.OwnerKind, "owner": it.Owner, "created_by": it.CreatedBy, "note": it.Note,
	})
	return string(b)
}

func owe(tx *Tx, it *ItemRow, at string) error {
	_, err := tx.Exec(`INSERT INTO item_outbox (item_id, revision, state, body, created_at, attempts, last_error)
		VALUES (?,?,?,?,?,0,'') ON CONFLICT (item_id, revision, state) DO NOTHING`,
		it.ID, it.Revision, it.State, it.pushBody(), at)
	return err
}

// ItemCreate records a new item and the push it owes. An id already held is the same item: nothing changes and the
// row comes back.
func (s *Store) ItemCreate(it ItemRow) (*ItemRow, error) {
	if strings.TrimSpace(it.ID) == "" || strings.TrimSpace(it.TaskID) == "" {
		return nil, errors.New("an item has an id and a card")
	}
	if it.State == "" {
		it.State = ItemRunning
	}
	if !validItemState(it.State) {
		return nil, errors.New("an item is queued, running, blocked or finished")
	}
	if it.Generation < 1 {
		it.Generation = 1
	}
	it.Revision = 1
	var out *ItemRow
	err := s.inTx(func(tx *Tx) error {
		now := ts(s.now())
		res, err := tx.Exec(`INSERT INTO item_mirror (`+itemRowCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,0,?,?)
			ON CONFLICT (item_id) DO NOTHING`, it.ID, it.TaskID, it.Generation, it.State, it.Revision, it.OwnerKind,
			it.Owner, it.CreatedBy, it.Note, "", now, now)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			it.CreatedAt, it.UpdatedAt = now, now
			if err := owe(tx, &it, now); err != nil {
				return err
			}
		}
		out, err = scanItemRow(tx.QueryRow(`SELECT `+itemRowCols+` FROM item_mirror WHERE item_id = ?`, it.ID))
		return err
	})
	return out, err
}

// ItemTransition moves an item to state with the words of the change, as one new revision and one owed push. Moving to
// the state and the words it already has changes nothing, and says so. It also clears a wait for an unblock.
func (s *Store) ItemTransition(id, state, note string) (*ItemRow, bool, error) {
	if !validItemState(state) {
		return nil, false, errors.New("an item is queued, running, blocked or finished")
	}
	var out *ItemRow
	changed := false
	err := s.inTx(func(tx *Tx) error {
		changed = false
		cur, err := scanItemRow(tx.QueryRow(`SELECT `+itemRowCols+` FROM item_mirror WHERE item_id = ?`, id))
		if err != nil {
			// sql.ErrNoRows passes through guard as an answer. A bare ErrNoItem here would halt the room.
			return err
		}
		if cur.State == state && cur.Note == note {
			out = cur
			return nil
		}
		cur.State, cur.Note, cur.Revision = state, note, cur.Revision+1
		cur.UnblockMsg, cur.UnblockDelivered = "", false
		now := ts(s.now())
		if _, err := tx.Exec(`UPDATE item_mirror SET state = ?, note = ?, revision = ?, unblock_msg = '',
			unblock_delivered = 0, updated_at = ? WHERE item_id = ?`, state, note, cur.Revision, now, id); err != nil {
			return err
		}
		cur.UpdatedAt = now
		if err := owe(tx, cur, now); err != nil {
			return err
		}
		out, changed = cur, true
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrNoItem
	}
	return out, changed, err
}

// ItemGet reads one item.
func (s *Store) ItemGet(id string) (*ItemRow, error) {
	var out *ItemRow
	err := s.guard(func() error {
		var err error
		out, err = scanItemRow(s.db.QueryRow(`SELECT `+itemRowCols+` FROM item_mirror WHERE item_id = ?`, id))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoItem
	}
	return out, err
}

// ItemOfTask is the item a card runs, the latest of them, or nil for a card that runs none.
func (s *Store) ItemOfTask(taskID string) (*ItemRow, error) {
	var out *ItemRow
	err := s.guard(func() error {
		var err error
		out, err = scanItemRow(s.db.QueryRow(`SELECT `+itemRowCols+` FROM item_mirror WHERE task_id = ?
			ORDER BY created_at DESC, item_id DESC LIMIT 1`, taskID))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return out, err
}

// ItemsOwed is the pushes the hub has not taken, oldest first.
func (s *Store) ItemsOwed(limit int) ([]ItemOwed, error) {
	var out []ItemOwed
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT item_id, revision, state, body, attempts, last_error FROM item_outbox
			ORDER BY created_at, item_id, revision LIMIT ?`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var o ItemOwed
			if err := rows.Scan(&o.ID, &o.Revision, &o.State, &o.Body, &o.Attempts, &o.LastError); err != nil {
				return err
			}
			out = append(out, o)
		}
		return rows.Err()
	})
	return out, err
}

// ItemPushed clears a push the hub took. Clearing one that is gone is not an error.
func (s *Store) ItemPushed(id string, revision int, state string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`DELETE FROM item_outbox WHERE item_id = ? AND revision = ? AND state = ?`, id, revision, state)
		return err
	})
}

// ItemPushFailed counts a try and keeps why.
func (s *Store) ItemPushFailed(id string, revision int, state, why string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE item_outbox SET attempts = attempts + 1, last_error = ?
			WHERE item_id = ? AND revision = ? AND state = ?`, why, id, revision, state)
		return err
	})
}

// ItemAwaitUnblock records the message that carries the owner's answer. The item stays blocked.
func (s *Store) ItemAwaitUnblock(id, messageID string) error {
	err := s.guard(func() error {
		res, err := s.db.Exec(`UPDATE item_mirror SET unblock_msg = ?, unblock_delivered = 0 WHERE item_id = ?`, messageID, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoItem
	}
	return err
}

// ItemUnblockDelivered notes that a message reached its worker, and answers the item it was the unblock of, or "".
// Delivery is not a turn: the item is blocked until one starts.
func (s *Store) ItemUnblockDelivered(messageID string) (string, error) {
	if messageID == "" {
		return "", nil
	}
	id := ""
	err := s.guard(func() error {
		id = ""
		err := s.db.QueryRow(`SELECT item_id FROM item_mirror WHERE unblock_msg = ?`, messageID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			id = ""
			return nil
		}
		if err != nil {
			return err
		}
		_, err = s.db.Exec(`UPDATE item_mirror SET unblock_delivered = 1 WHERE item_id = ?`, id)
		return err
	})
	return id, err
}

// ItemNotice queues atrium's one notice about an item to a card on this room, once per (worker, key). The key names the
// item, its revision and its state, so a replay of the same change is the same notice. Nil when it was already told.
func (s *Store) ItemNotice(workerID, toID, key, text string) (*LedgerNotice, error) {
	var out *LedgerNotice
	err := s.inTx(func(tx *Tx) error {
		var err error
		out, err = s.queueNotice(tx, &WorkItem{TaskID: workerID, ArbiterID: toID, Handle: ReservedHandle}, NoticeItem, key, text)
		return err
	})
	return out, err
}
