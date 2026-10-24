package hubstore

import (
	"database/sql"
	"errors"
	"strings"
)

// ITEMS: one assignment of work, owned here. See docs/rnd/atrium-items-decisions.md and docs/changes/items-stage2a.md.
//
//   - THE HUB OWNS THEM. A room keeps a mirror and an outbox, and pushes a revision here when the hub is reachable. The
//     key of a push is (item_id, revision, state): item_event holds every key ever applied, so a replay inserts
//     nothing and changes nothing.
//   - A REVISION ONLY GOES UP. A push older than the row is recorded as seen and never moves the row back, because
//     pushes held through an outage can arrive in any order.
//   - THE HUB DELIVERS NOTHING. The notice for a blocked or finished item is the room's, keyed on the same triple.
//   - A card is named by (room, card). The worker never reads any of this.

// Item states. `blocked` is not `finished`: the owner unblocks it and it runs again.
const (
	ItemStateQueued   = "queued"
	ItemStateRunning  = "running"
	ItemStateBlocked  = "blocked"
	ItemStateFinished = "finished"
)

// Owner kinds. A session owner is a handle, a role owner a flow role resolved to its holder when something is
// delivered, and the human has no handle at all.
const (
	OwnerSession = "session"
	OwnerRole    = "role"
	OwnerHuman   = "human"
)

// OwnerHumanWord is how an owner is asked for in a launch.
const OwnerHumanWord = "human"

// ErrNoItem is an item id nobody holds.
var ErrNoItem = errors.New("no such item")

// Item is one row.
type Item struct {
	ID   string `json:"item_id"`
	Room string `json:"room"`
	Card string `json:"card"`
	// Generation is the card's session generation the item ran in.
	Generation int    `json:"generation"`
	State      string `json:"state"`
	// Revision goes up on every change, and is the room's to number.
	Revision  int    `json:"revision"`
	OwnerKind string `json:"owner_kind"`
	// Owner is a handle for a session, a role name for a role and empty for the human.
	Owner     string `json:"owner,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
	// Note is the words of the last change: the blocked reason, or the result line.
	Note string `json:"note,omitempty"`
	// WorkID is the backlog id of the item this assignment belongs to, `adhoc-<item_id>` for work no backlog item named.
	// A push may leave it empty, and the hub then files the assignment as adhoc. See work.go.
	WorkID string `json:"work_id,omitempty"`
	// Step is the step the card was launched for, `atrium:step:<step>` on its tags. It rides the first push, and is not a
	// column of this table: the attachment holds it (work_card.step).
	Step      string `json:"step,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ItemNoteMax bounds a note, in bytes.
const ItemNoteMax = 2000

func validItemState(s string) bool {
	switch s {
	case ItemStateQueued, ItemStateRunning, ItemStateBlocked, ItemStateFinished:
		return true
	}
	return false
}

// SingleHolderRole is whether a role has one holder at a time. Only those may own an item: the tester has several, so
// "the tester" is nobody in particular.
func SingleHolderRole(role string) bool {
	return ValidRole(role) && role != RoleTester
}

var errTesterOwner = errors.New("the tester has several holders, so it cannot own an item. name orchestrator, merger, " +
	"director or human")

// ResolveOwner turns what a launch asked for into an owner. Empty is the creating session, `human` is the human, and
// a role name (bare or `role:<name>`) is a flow role, refused unless it has a single holder.
func ResolveOwner(spec, creator string) (kind, owner string, err error) {
	spec = strings.ToLower(strings.TrimSpace(spec))
	switch {
	case spec == "":
		if c := strings.TrimSpace(creator); c != "" {
			return OwnerSession, c, nil
		}
		return "", "", refuse(errors.New("an item needs an owner: there is no creating session, so name `human` or a role"))
	case spec == OwnerHumanWord:
		return OwnerHuman, "", nil
	}
	role := strings.TrimPrefix(spec, "role:")
	switch {
	case role == RoleTester:
		return "", "", refuse(errTesterOwner)
	case role == RoleMerger:
		return "", "", refuse(ErrMergerRetired)
	case !SingleHolderRole(role):
		return "", "", refuse(errors.New("an owner is empty (the creating session), human, orchestrator or director"))
	}
	return OwnerRole, role, nil
}

func checkItem(it Item) error {
	switch {
	case strings.TrimSpace(it.ID) == "" || len(it.ID) > 80:
		return refuse(errors.New("an item has an id"))
	case fold(it.Room) == "" || strings.TrimSpace(it.Card) == "":
		return refuse(errors.New("an item names the room and card that run it"))
	case !validItemState(it.State):
		return refuse(errors.New("an item is queued, running, blocked or finished"))
	case it.Revision < 1:
		return refuse(errors.New("an item's revision starts at 1"))
	case it.OwnerKind != OwnerSession && it.OwnerKind != OwnerRole && it.OwnerKind != OwnerHuman:
		return refuse(errors.New("an item's owner is a session, a role or the human"))
	case it.OwnerKind == OwnerRole && it.Owner == RoleTester:
		return refuse(errTesterOwner)
	case it.OwnerKind == OwnerRole && !SingleHolderRole(it.Owner):
		return refuse(errors.New("only a single-holder role may own an item"))
	case it.OwnerKind == OwnerSession && strings.TrimSpace(it.Owner) == "":
		return refuse(errors.New("a session owner has a handle"))
	case len(it.Note) > ItemNoteMax:
		return refuse(errors.New("that note is too long"))
	case len(it.WorkID) > 80 || strings.ContainsAny(it.WorkID, " \t\r\n/"):
		return refuse(errors.New("an item's work id is a backlog id"))
	}
	return nil
}

const atItemCols = `item_id, room, card, generation, state, revision, owner_kind, owner, created_by, note, created_at, updated_at, work_id`

func scanAtItem(sc scanner) (Item, error) {
	var it Item
	err := sc.Scan(&it.ID, &it.Room, &it.Card, &it.Generation, &it.State, &it.Revision, &it.OwnerKind, &it.Owner,
		&it.CreatedBy, &it.Note, &it.CreatedAt, &it.UpdatedAt, &it.WorkID)
	return it, err
}

// PutItem applies one push and answers the row as it stands, and whether this push changed anything. A replay of a
// key already applied is a no-op, and so is a push older than the row.
func (s *Store) PutItem(it Item) (Item, bool, error) {
	it.Room = fold(it.Room)
	// A step this hub does not know is a newer room's, and is ignored: refusing the push would have the room retry it forever.
	if !ValidStep(it.Step) {
		it.Step = ""
	}
	if err := checkItem(it); err != nil {
		return Item{}, false, err
	}
	var out Item
	applied := false
	err := s.tx(func(t *sql.Tx) error {
		applied = false
		now := ts(s.now())
		res, err := t.Exec(`INSERT OR IGNORE INTO item_event (item_id, revision, state, at) VALUES (?, ?, ?, ?)`,
			it.ID, it.Revision, it.State, now)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			cur, err := scanAtItem(t.QueryRow(`SELECT `+atItemCols+` FROM item WHERE item_id = ?`, it.ID))
			switch {
			case errors.Is(err, sql.ErrNoRows):
				if it.WorkID == "" {
					it.WorkID = AdhocWorkID(it.ID)
				}
				_, err = t.Exec(`INSERT INTO item (`+atItemCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
					it.ID, it.Room, it.Card, it.Generation, it.State, it.Revision, it.OwnerKind, it.Owner, it.CreatedBy,
					it.Note, now, now, it.WorkID)
				applied = err == nil
			case err != nil:
				return err
			case it.Revision > cur.Revision:
				// Who and where an item is never changes: a push moves its state, its words and its revision.
				_, err = t.Exec(`UPDATE item SET state = ?, revision = ?, generation = ?, note = ?, updated_at = ? WHERE item_id = ?`,
					it.State, it.Revision, it.Generation, it.Note, now, it.ID)
				applied = err == nil
			}
			if err != nil {
				return err
			}
		}
		out, err = scanAtItem(t.QueryRow(`SELECT `+atItemCols+` FROM item WHERE item_id = ?`, it.ID))
		if err != nil {
			return err
		}
		// THE ATTACH, once per card. An assignment held from before this stage and pushed again is attached the same way,
		// and a work id the row has not got is set from a push that names one and never changed once it has one.
		if out.WorkID == "" && it.WorkID != "" {
			if _, err = t.Exec(`UPDATE item SET work_id = ? WHERE item_id = ? AND work_id = ''`, it.WorkID, it.ID); err != nil {
				return err
			}
			out.WorkID = it.WorkID
		}
		if out.WorkID == "" {
			return nil
		}
		var have int
		if err = t.QueryRow(`SELECT COUNT(*) FROM work_card WHERE backlog_id = ? AND room = ? AND card = ?`,
			out.WorkID, out.Room, out.Card).Scan(&have); err != nil {
			return err
		}
		if have > 0 {
			return setCardStep(t, out.WorkID, out.Room, out.Card, it.Step)
		}
		return attachWork(t, out.WorkID, out.Room, out.Card, it.Step, now)
	})
	return out, applied, err
}

// ItemByID reads one item, or ErrNoItem.
func (s *Store) ItemByID(id string) (Item, error) {
	var it Item
	err := s.guard(func() error {
		var err error
		it, err = scanAtItem(s.db.QueryRow(`SELECT `+atItemCols+` FROM item WHERE item_id = ?`, strings.TrimSpace(id)))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNoItem
	}
	return it, err
}

// ItemEvent is one revision the hub has applied to an item.
type ItemEvent struct {
	Revision int    `json:"revision"`
	State    string `json:"state"`
	At       string `json:"at"`
}

// ItemHistory is every revision applied to an item, oldest first.
func (s *Store) ItemHistory(id string) ([]ItemEvent, error) {
	var out []ItemEvent
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT revision, state, at FROM item_event WHERE item_id = ? ORDER BY revision, at`,
			strings.TrimSpace(id))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e ItemEvent
			if err := rows.Scan(&e.Revision, &e.State, &e.At); err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

// ItemQuery narrows Items. Every empty field matches everything.
type ItemQuery struct {
	Room      string
	OwnerKind string
	Owner     string
	States    []string
	Limit     int
}

// Items lists items, newest change first.
func (s *Store) Items(f ItemQuery) ([]Item, error) {
	where, args := []string{"1 = 1"}, []any{}
	if r := fold(f.Room); r != "" {
		where, args = append(where, "room = ?"), append(args, r)
	}
	if f.OwnerKind != "" {
		where, args = append(where, "owner_kind = ?"), append(args, f.OwnerKind)
	}
	if f.Owner != "" {
		where, args = append(where, "owner = ?"), append(args, f.Owner)
	}
	if len(f.States) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(f.States)), ",")
		where = append(where, "state IN ("+ph+")")
		for _, st := range f.States {
			args = append(args, st)
		}
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	var out []Item
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT `+atItemCols+` FROM item WHERE `+strings.Join(where, " AND ")+
			` ORDER BY updated_at DESC, item_id LIMIT ?`, append(args, limit)...)
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

// ItemHolder resolves who an item's notice goes to NOW: a role to its current holder, a session to its handle's own
// room and card is the room's business, so ok is false for it. ok is also false for the human and for a role nobody
// holds, which is the stuck alert's to say, not this method's.
func (s *Store) ItemHolder(it Item) (room, card string, ok bool, err error) {
	if it.OwnerKind != OwnerRole {
		return "", "", false, nil
	}
	hs, err := s.Roles(it.Owner)
	if err != nil || len(hs) == 0 {
		return "", "", false, err
	}
	return hs[0].Room, hs[0].Card, true, nil
}
