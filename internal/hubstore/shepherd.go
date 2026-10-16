package hubstore

import (
	"database/sql"
	"errors"
	"strings"
)

// SHEPHERDS: one row per `atrium shepherd run <item>`, the program that takes one backlog item from launch to a held
// packet. See docs/rnd/script-minion.md, recommendation 5.
//
//   - THE ROW IS WRITTEN BEFORE A PHASE RUNS, so a killed shepherd resumes at the phase it was in and the phase, being
//     idempotent, repeats safely. The shepherd keeps nothing in memory that this row does not hold.
//   - THE STORE DOES NOT KNOW WHAT A PHASE MEANS. Phase is a word the shepherd package owns, kept as text so a new
//     phase is not a migration.
//   - A PUT REPLACES THE ROW. There is one writer per item, the shepherd process, so there is no merge to do.

// ErrNoShepherd is an item nobody has a shepherd for.
var ErrNoShepherd = errors.New("no shepherd for that item")

// ShepherdMax bounds the free text of a row (the packet, the last error), in bytes.
const ShepherdMax = 64 << 10

// Shepherd is one row.
type Shepherd struct {
	ItemID string `json:"item_id"`
	Phase  string `json:"phase"`
	Room   string `json:"room"`
	// BaseSHA is claude/main when the worker was launched.
	BaseSHA  string `json:"base_sha"`
	Worktree string `json:"worktree"`
	// Branch is the shepherd's own staging branch, claude/land-<id>.
	Branch string `json:"branch"`
	// Card and WorkItem are the worker's card and the hub item its assignment became.
	Card     string `json:"card,omitempty"`
	WorkItem string `json:"work_item,omitempty"`
	// Ref is the collected branch the worker's commits are read from.
	Ref string `json:"ref,omitempty"`
	// Stage is the staging worktree.
	Stage string `json:"stage,omitempty"`
	// Packet is the evidence a hold shows, JSON the shepherd package shapes.
	Packet    string `json:"packet,omitempty"`
	LastError string `json:"last_error,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

const shepherdCols = `item_id, phase, room, base_sha, worktree, branch, card, work_item, ref, stage, packet, last_error,
	created_at, updated_at`

func clip(s string) string {
	if len(s) > ShepherdMax {
		return s[:ShepherdMax]
	}
	return s
}

// ShepherdGet reads one row, or ErrNoShepherd.
func (s *Store) ShepherdGet(id string) (Shepherd, error) {
	var r Shepherd
	err := s.guard(func() error {
		return s.db.QueryRow(`SELECT `+shepherdCols+` FROM shepherd WHERE item_id = ?`, strings.TrimSpace(id)).Scan(
			&r.ItemID, &r.Phase, &r.Room, &r.BaseSHA, &r.Worktree, &r.Branch, &r.Card, &r.WorkItem, &r.Ref, &r.Stage,
			&r.Packet, &r.LastError, &r.CreatedAt, &r.UpdatedAt)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Shepherd{}, ErrNoShepherd
	}
	return r, err
}

// ShepherdPut writes the row whole, stamping its times. created_at is kept from the first write.
func (s *Store) ShepherdPut(r Shepherd) (Shepherd, error) {
	r.ItemID = strings.TrimSpace(r.ItemID)
	if r.ItemID == "" {
		return r, refuse(errors.New("a shepherd needs an item id"))
	}
	if strings.TrimSpace(r.Phase) == "" {
		return r, refuse(errors.New("a shepherd needs a phase"))
	}
	now := ts(s.now())
	r.Packet, r.LastError = clip(r.Packet), clip(r.LastError)
	err := s.guard(func() error {
		_, err := s.db.Exec(`INSERT INTO shepherd (`+shepherdCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(item_id) DO UPDATE SET phase=excluded.phase, room=excluded.room, base_sha=excluded.base_sha,
			worktree=excluded.worktree, branch=excluded.branch, card=excluded.card, work_item=excluded.work_item,
			ref=excluded.ref, stage=excluded.stage, packet=excluded.packet, last_error=excluded.last_error,
			updated_at=excluded.updated_at`,
			r.ItemID, r.Phase, r.Room, r.BaseSHA, r.Worktree, r.Branch, r.Card, r.WorkItem, r.Ref, r.Stage, r.Packet,
			r.LastError, now, now)
		return err
	})
	if err != nil {
		return r, err
	}
	return s.ShepherdGet(r.ItemID)
}
