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

// EventHoldLifted is the `by` on the event a lift writes on each held card.
const EventHoldLifted = "deploy-hold-lifted"

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
	// Cards is every card held at the start.
	Cards []string `json:"cards"`
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
		if err := s.writeRoomHoldsOn(tx, append(holds, h)); err != nil {
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

// LiftRoomHold ends the hold of `kind` whose id is `id`, and answers whether it
// was there to lift. `id` empty lifts whatever hold of that kind there is.
//
// ONE TRANSACTION for the lift and the EventHoldLifted event on each held card saying `outcome`. Nothing is typed
// into a card: a lift releases it silently.
func (s *Store) LiftRoomHold(kind, id, outcome string) (*RoomHold, error) {
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
		if err := s.writeRoomHoldsOn(tx, kept); err != nil {
			return err
		}
		for _, card := range lifted.Cards {
			if !lifted.Holds(card) {
				continue
			}
			// A held card removed since (a throwaway, a close) has nothing to tell, and must not keep the hold on.
			if _, err := s.appendEventOn(tx, card, EventNotified, map[string]any{
				"by": EventHoldLifted, "hold": lifted.ID, "outcome": outcome,
			}); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		return nil
	})
	return lifted, err
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

func (s *Store) writeRoomHoldsOn(q querier, holds []RoomHold) error {
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
		SettingRoomHold, v, ts(s.now()))
	return err
}
