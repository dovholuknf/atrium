package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// A room hold: every held card's next gated call is refused until the hold is
// lifted. See docs/rnd/room-deploy-hold-design.md and section 1 of
// docs/rnd/freeze-budget-design.md.
//
// ONE SETTING, A LIST OF AT MOST ONE HOLD PER KIND. The daemon reads it from
// memory on the permission path, and this file is only the durable copy.
//
// IN THE ROOM, NOT THE HUB, because the room has to find the hold again after the
// restart it was set for, and the permission chain that enforces it runs here.

// SettingRoomHold is the key the holds are kept under, as JSON.
const SettingRoomHold = "room_hold"

// SettingDeployHoldMax is how long a deploy hold may last, in minutes, before the
// room lifts it on its own. Empty is sixty.
const SettingDeployHoldMax = "deploy_hold_max"

// HoldDeploy is the kind a deploy owner sets. It is the only kind built so far.
const HoldDeploy = "deploy"

// EventHoldLifted is the `by` on the event a lift writes on each held card. A
// second startup finds the hold gone and writes nothing, which is what keeps a
// card from being woken twice.
const EventHoldLifted = "deploy-hold-lifted"

// EventResumeContinue is the `by` on the event a startup lift writes on each card
// it tells to continue. The board shows it as "resumed, told to continue".
const EventResumeContinue = "resume-continue"

// RoomHold is one hold on this room.
type RoomHold struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	// By is who set it: the deployer's handle, or `operator`.
	By string `json:"by"`
	// ByCard is the deployer's card here, when it is on this room. It is never
	// held by its own hold.
	ByCard string `json:"by_card,omitempty"`
	// Whys is every reason the deploy was asked for.
	Whys []string `json:"whys,omitempty"`
	// Exempt cards are not held: a card the deploy itself needs.
	Exempt []string `json:"exempt,omitempty"`
	// FromBuild is the build the room was on when the hold was set. A room that
	// comes back on the same build did not take the deploy.
	FromBuild string    `json:"from_build"`
	StartedAt time.Time `json:"started_at"`
	ExpiresAt time.Time `json:"expires_at"`
	// Cards is every card held at the start, and the list the wake goes to.
	Cards []string `json:"cards"`
	// Working is the cards that were mid-turn when the hold was set, or whose
	// gated call the hold refused, since the hold is what ended their turn. Only
	// these are told to continue after the restart. The rest were waiting for a
	// human, and still are.
	Working []string `json:"working,omitempty"`
}

// Worked reports whether a card was working when the hold took its turn.
func (h *RoomHold) Worked(taskID string) bool {
	if h == nil {
		return false
	}
	for _, id := range h.Working {
		if id == taskID {
			return true
		}
	}
	return false
}

// Holds reports whether h holds a card.
func (h *RoomHold) Holds(taskID string) bool {
	if h == nil || taskID == "" || taskID == h.ByCard {
		return false
	}
	for _, id := range h.Exempt {
		if id == taskID {
			return false
		}
	}
	for _, id := range h.Cards {
		if id == taskID {
			return true
		}
	}
	return false
}

// RoomHolds reads every hold on this room. Unset reads as none.
func (s *Store) RoomHolds() ([]RoomHold, error) {
	v, err := s.Setting(SettingRoomHold)
	if err != nil {
		return nil, err
	}
	return parseRoomHolds(v)
}

func parseRoomHolds(v string) ([]RoomHold, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	var out []RoomHold
	if err := json.Unmarshal([]byte(v), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ErrHoldExists is a second hold of a kind that already has one.
var ErrHoldExists = errors.New("this room already has a hold of that kind")

// SetRoomHold adds a hold, and refuses a second one of the same kind. It records
// a `notified` event on each held card in the same transaction.
//
// The refusal is reported after the transaction, never returned from inside it,
// where any error halts the store.
func (s *Store) SetRoomHold(h RoomHold) error {
	exists := false
	err := s.inTx(func(tx *Tx) error {
		exists = false
		holds, err := roomHoldsOn(tx)
		if err != nil {
			return err
		}
		for _, o := range holds {
			if o.Kind == h.Kind {
				exists = true
				return nil
			}
		}
		if err := writeRoomHoldsOn(tx, append(holds, h)); err != nil {
			return err
		}
		for _, id := range h.Cards {
			if !h.Holds(id) {
				continue
			}
			if _, err := s.appendEventOn(tx, id, EventNotified, map[string]any{
				"by": "deploy-hold", "what": "held", "hold": h.ID, "held_by": h.By,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil && exists {
		return ErrHoldExists
	}
	return err
}

// MarkHoldWorking records that a card was working under the hold of `kind`. A
// card already marked, or a hold that is gone, changes nothing. Answers whether
// it wrote.
func (s *Store) MarkHoldWorking(kind, taskID string) (bool, error) {
	wrote := false
	err := s.inTx(func(tx *Tx) error {
		wrote = false
		holds, err := roomHoldsOn(tx)
		if err != nil {
			return err
		}
		for i := range holds {
			if holds[i].Kind != kind || holds[i].Worked(taskID) {
				continue
			}
			holds[i].Working = append(holds[i].Working, taskID)
			wrote = true
		}
		if !wrote {
			return nil
		}
		return writeRoomHoldsOn(tx, holds)
	})
	return wrote && err == nil, err
}

// LiftRoomHold ends the hold of `kind` whose id is `id`, and answers whether it
// was there to lift. `id` empty lifts whatever hold of that kind there is.
//
// ONE TRANSACTION for the lift and everything it tells: the setting, a
// EventHoldLifted event on each held card saying `outcome`, and a restart wake for
// every card in `wakes`. A card that already has a wake keeps it, and the hold's
// line is appended, so the card is typed into once.
func (s *Store) LiftRoomHold(kind, id, outcome string, wakes map[string]string) (*RoomHold, error) {
	var lifted *RoomHold
	err := s.inTx(func(tx *Tx) error {
		lifted = nil
		holds, err := roomHoldsOn(tx)
		if err != nil {
			return err
		}
		kept := holds[:0:0]
		for i := range holds {
			if holds[i].Kind == kind && (id == "" || holds[i].ID == id) && lifted == nil {
				h := holds[i]
				lifted = &h
				continue
			}
			kept = append(kept, holds[i])
		}
		if lifted == nil {
			return nil
		}
		if err := writeRoomHoldsOn(tx, kept); err != nil {
			return err
		}
		for _, card := range lifted.Cards {
			if !lifted.Holds(card) {
				continue
			}
			if _, err := s.appendEventOn(tx, card, EventNotified, map[string]any{
				"by": EventHoldLifted, "hold": lifted.ID, "outcome": outcome,
			}); err != nil {
				return err
			}
		}
		for card, text := range wakes {
			if err := s.addRestartWakeOn(tx, card, text, EventHoldLifted); err != nil {
				return err
			}
			if !lifted.Worked(card) {
				continue
			}
			if _, err := s.appendEventOn(tx, card, EventNotified, map[string]any{
				"by": EventResumeContinue, "hold": lifted.ID,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return lifted, err
}

// addRestartWakeOn queues a wake, appended to one the card already has. A card
// that has gone is skipped: its wake has nobody to reach.
func (s *Store) addRestartWakeOn(tx *Tx, taskID, text, by string) error {
	var one int
	if err := tx.QueryRow(`SELECT 1 FROM task WHERE id = ?`, taskID).Scan(&one); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	prev, err := restartWakeOn(tx, taskID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if prev != nil {
		// The card's own wake first, since it is what that session asked to hear.
		// Cut to fit, never the hold's line.
		room := MaxRestartWake - len(text) - 2
		old := prev.Text
		if room < 0 {
			room = 0
		}
		if len(old) > room {
			old = old[:room]
		}
		text = strings.TrimSpace(old + "\n\n" + text)
		by = prev.By
	}
	if len(text) > MaxRestartWake {
		text = text[:MaxRestartWake]
	}
	at := now()
	if _, err := tx.Exec(`INSERT INTO restart_wake (task_id, text, queued_by, queued_at, expires_at, expired_at)
		VALUES (?, ?, ?, ?, ?, NULL)
		ON CONFLICT (task_id) DO UPDATE SET text = excluded.text, queued_by = excluded.queued_by,
			queued_at = excluded.queued_at, expires_at = excluded.expires_at, expired_at = NULL`,
		taskID, text, by, ts(at), ts(at)); err != nil {
		return err
	}
	_, err = s.appendEventOn(tx, taskID, EventNotified, map[string]any{
		"by": RestartWakeBy, "what": "queued", "text": text, "queued_by": by,
	})
	return err
}

func roomHoldsOn(q querier) ([]RoomHold, error) {
	var v string
	err := q.QueryRow(`SELECT value FROM setting WHERE key = ?`, SettingRoomHold).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseRoomHolds(v)
}

func writeRoomHoldsOn(q querier, holds []RoomHold) error {
	v := ""
	if len(holds) > 0 {
		raw, err := json.Marshal(holds)
		if err != nil {
			return err
		}
		v = string(raw)
	}
	_, err := q.Exec(`INSERT INTO setting (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		SettingRoomHold, v, ts(now()))
	return err
}
