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
	// UnblockText is the answer, UnblockAt when it was given and RetypedAt when it was typed a second time. Part 2b.
	UnblockText string
	UnblockAt   string
	RetypedAt   string
	// StuckAlerted is why the human was alerted for this revision, or "". One alert per stall, because a new revision
	// clears it.
	StuckAlerted string
	// MissedOwner and MissedWorker say the notice, or the answer, was handed back once after a context clear.
	MissedOwner  bool
	MissedWorker bool
	// Pool and Brief are an item submitted to a pool: the pool's name, and the words typed to whichever resident card
	// takes it. An item that was queued has no card (TaskID is empty) until one does. See item_pool.go.
	Pool  string
	Brief string
	// WorkID is the backlog id of the item this assignment is attached to, empty for one no backlog item named. The hub
	// files an empty one as `adhoc-<item_id>`.
	WorkID    string
	CreatedAt string
	UpdatedAt string
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
	unblock_delivered, unblock_text, unblock_at, retyped_at, stuck_alerted, missed_owner, missed_worker, pool, brief,
	work_id, created_at, updated_at`

func scanItemRow(sc interface{ Scan(...any) error }) (*ItemRow, error) {
	var it ItemRow
	var del, mo, mw int
	if err := sc.Scan(&it.ID, &it.TaskID, &it.Generation, &it.State, &it.Revision, &it.OwnerKind, &it.Owner, &it.CreatedBy,
		&it.Note, &it.UnblockMsg, &del, &it.UnblockText, &it.UnblockAt, &it.RetypedAt, &it.StuckAlerted, &mo, &mw,
		&it.Pool, &it.Brief, &it.WorkID, &it.CreatedAt, &it.UpdatedAt); err != nil {
		return nil, err
	}
	it.UnblockDelivered, it.MissedOwner, it.MissedWorker = del != 0, mo != 0, mw != 0
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
		"work_id": it.WorkID,
	})
	return string(b)
}

func owe(tx *Tx, it *ItemRow, at string) error {
	// A queued item has no card yet, and the hub takes an item only with the card that runs it. Its first push is the
	// assignment.
	if it.TaskID == "" {
		return nil
	}
	_, err := tx.Exec(`INSERT INTO item_outbox (item_id, revision, state, body, created_at, attempts, last_error)
		VALUES (?,?,?,?,?,0,'') ON CONFLICT (item_id, revision, state) DO NOTHING`,
		it.ID, it.Revision, it.State, it.pushBody(), at)
	return err
}

// ItemCreate records a new item and the push it owes. An id already held is the same item: nothing changes and the
// row comes back.
func (s *Store) ItemCreate(it ItemRow) (*ItemRow, error) {
	if strings.TrimSpace(it.ID) == "" || (strings.TrimSpace(it.TaskID) == "" && it.Pool == "") {
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
		res, err := tx.Exec(`INSERT INTO item_mirror (`+itemRowCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,0,'','','','',0,0,?,?,?,?,?)
			ON CONFLICT (item_id) DO NOTHING`, it.ID, it.TaskID, it.Generation, it.State, it.Revision, it.OwnerKind,
			it.Owner, it.CreatedBy, it.Note, "", it.Pool, it.Brief, strings.TrimSpace(it.WorkID), now, now)
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
		cur.UnblockText, cur.UnblockAt, cur.RetypedAt, cur.StuckAlerted = "", "", "", ""
		cur.MissedOwner, cur.MissedWorker = false, false
		now := ts(s.now())
		if _, err := tx.Exec(`UPDATE item_mirror SET state = ?, note = ?, revision = ?, unblock_msg = '',
			unblock_delivered = 0, unblock_text = '', unblock_at = '', retyped_at = '', stuck_alerted = '',
			missed_owner = 0, missed_worker = 0, updated_at = ? WHERE item_id = ?`, state, note, cur.Revision, now, id); err != nil {
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
			ORDER BY created_at DESC, rowid DESC LIMIT 1`, taskID))
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
//
// The answer and the time it was given are kept for the stuck timers, which may type it once more.
func (s *Store) ItemAwaitUnblock(id, messageID, answer string) error {
	err := s.guard(func() error {
		res, err := s.db.Exec(`UPDATE item_mirror SET unblock_msg = ?, unblock_delivered = 0, unblock_text = ?,
			unblock_at = ?, retyped_at = '' WHERE item_id = ?`, messageID, answer, ts(s.now()), id)
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

// ItemsBlocked is every blocked item that has not been alerted for, which is all the stuck timers look at.
func (s *Store) ItemsBlocked() ([]*ItemRow, error) {
	var out []*ItemRow
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT ` + itemRowCols + ` FROM item_mirror WHERE state = 'blocked' AND stuck_alerted = ''`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			it, err := scanItemRow(rows)
			if err != nil {
				return err
			}
			out = append(out, it)
		}
		return rows.Err()
	})
	return out, err
}

// ItemStuck records the alert for a revision and says whether this call was the one that did, so an alert is once per
// stall however many ticks or paths find it. Only a blocked item at that revision can be alerted for.
func (s *Store) ItemStuck(id string, revision int, why string) (bool, error) {
	return s.itemFlip(`UPDATE item_mirror SET stuck_alerted = ? WHERE item_id = ? AND revision = ? AND state = 'blocked'
		AND stuck_alerted = ''`, why, id, revision)
}

// ItemRetyped records that the answer was typed a second time, and when. Once per revision.
func (s *Store) ItemRetyped(id string, revision int, at string) (bool, error) {
	return s.itemFlip(`UPDATE item_mirror SET retyped_at = ? WHERE item_id = ? AND revision = ? AND state = 'blocked'
		AND retyped_at = ''`, at, id, revision)
}

// ItemMissed records that the notice (owner) or the answer (worker) was handed back after a context clear, and says
// whether this call was the first. Once per revision and side.
func (s *Store) ItemMissed(id string, revision int, worker bool) (bool, error) {
	col := "missed_owner"
	if worker {
		col = "missed_worker"
	}
	return s.itemFlip(`UPDATE item_mirror SET `+col+` = 1 WHERE item_id = ? AND revision = ? AND `+col+` = 0`, id, revision)
}

func (s *Store) itemFlip(q string, args ...any) (bool, error) {
	first := false
	err := s.guard(func() error {
		first = false
		res, err := s.db.Exec(q, args...)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		first = n > 0
		return nil
	})
	return first, err
}

// ItemsQueued is a pool's items still waiting for a card, oldest first. That order is the pool's: one at a time, in
// the order they were submitted.
func (s *Store) ItemsQueued(pool string) ([]*ItemRow, error) {
	var out []*ItemRow
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT `+itemRowCols+` FROM item_mirror WHERE pool = ? AND state = 'queued'
			ORDER BY created_at, rowid`, pool)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			it, err := scanItemRow(rows)
			if err != nil {
				return err
			}
			out = append(out, it)
		}
		return rows.Err()
	})
	return out, err
}

// ItemPoolsQueued is the pools that have an item waiting.
func (s *Store) ItemPoolsQueued() ([]string, error) {
	var out []string
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT DISTINCT pool FROM item_mirror WHERE pool <> '' AND state = 'queued'`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// ItemCardBusy is whether a card holds an item that is not finished. A blocked item counts as busy: its worker is
// waiting on an answer, and the card takes nothing new until the item is finished.
func (s *Store) ItemCardBusy(taskID string) (bool, error) {
	n := 0
	err := s.guard(func() error {
		return s.db.QueryRow(`SELECT COUNT(*) FROM item_mirror WHERE task_id = ? AND state IN ('running','blocked')`,
			taskID).Scan(&n)
	})
	return n > 0, err
}

// ItemAssign gives a queued item to the card that was typed its brief, as running at the next revision, with the push
// that is owed. False when the item was no longer queued, so a second feed of one item is a no-op.
func (s *Store) ItemAssign(id, taskID string, generation int) (*ItemRow, bool, error) {
	if generation < 1 {
		generation = 1
	}
	var out *ItemRow
	changed := false
	err := s.inTx(func(tx *Tx) error {
		changed = false
		cur, err := scanItemRow(tx.QueryRow(`SELECT `+itemRowCols+` FROM item_mirror WHERE item_id = ?`, id))
		if err != nil {
			return err
		}
		if cur.State != ItemQueued {
			out = cur
			return nil
		}
		now := ts(s.now())
		cur.TaskID, cur.Generation, cur.State, cur.Revision, cur.UpdatedAt = taskID, generation, ItemRunning, cur.Revision+1, now
		if _, err := tx.Exec(`UPDATE item_mirror SET task_id = ?, generation = ?, state = ?, revision = ?, updated_at = ?
			WHERE item_id = ?`, taskID, generation, ItemRunning, cur.Revision, now, id); err != nil {
			return err
		}
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
