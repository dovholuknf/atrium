package hubstore

import (
	"strings"
	"time"
)

// What the hub heard from a local script, who asked to hear it, and what each was told. See
// docs/fabric/land-webhook-design.md. THE REQUEST BODY IS NEVER STORED: an event is the fields the hub parsed out of it.

// EventSource is where an event came from. Only the hub's own machine posts today.
const EventSource = "local"

// HubEvent is one event the hub recorded. From and To are commit ids.
type HubEvent struct {
	ID         string
	Kind       string
	Repo       string
	Branch     string
	Outcome    string
	From       string
	To         string
	Commits    int
	Reason     string
	Source     string
	ReceivedAt time.Time
}

// EventAdd records an event. A repeat of (kind, repo, branch, outcome, from, to) is dup and nothing is written.
func (s *Store) EventAdd(e HubEvent) (HubEvent, bool, error) {
	e.ID = newID()
	e.ReceivedAt = s.now()
	if e.Source == "" {
		e.Source = EventSource
	}
	var dup bool
	err := s.guard(func() error {
		res, err := s.db.Exec(
			`INSERT INTO hub_event (id, kind, repo, branch, outcome, from_sha, to_sha, commits, reason, source, received_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT (kind, repo, branch, outcome, from_sha, to_sha) DO NOTHING`,
			e.ID, e.Kind, e.Repo, e.Branch, e.Outcome, e.From, e.To, e.Commits, e.Reason, e.Source, ts(e.ReceivedAt))
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		dup = n == 0
		return nil
	})
	return e, dup, err
}

// EventSub is one card's wish to hear one kind of event. Repo and Branch are filters, empty for any.
type EventSub struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Repo      string    `json:"repo"`
	Branch    string    `json:"branch"`
	Card      string    `json:"card"`
	Wake      bool      `json:"wake"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

const eventSubCols = `id, kind, repo, branch, card, wake, created_by, created_at`

func scanEventSub(sc scanner) (EventSub, error) {
	var e EventSub
	var wake int
	var created string
	if err := sc.Scan(&e.ID, &e.Kind, &e.Repo, &e.Branch, &e.Card, &wake, &e.CreatedBy, &created); err != nil {
		return e, err
	}
	e.Wake = wake != 0
	if t := parseOrNil(created); t != nil {
		e.CreatedAt = *t
	}
	return e, nil
}

func (s *Store) eventSubs(where string, args ...any) ([]EventSub, error) {
	out := []EventSub{}
	err := s.guard(func() error {
		out = out[:0]
		rows, err := s.db.Query(`SELECT `+eventSubCols+` FROM event_sub `+where, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			e, err := scanEventSub(rows)
			if err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

// EventSubAdd records a subscription. Asking again for the same (kind, repo, branch, card) is existed, and the row that
// was there is returned unchanged.
func (s *Store) EventSubAdd(e EventSub) (EventSub, bool, error) {
	e.ID = newID()
	e.CreatedAt = s.now()
	var existed bool
	err := s.guard(func() error {
		res, err := s.db.Exec(
			`INSERT INTO event_sub (id, kind, repo, branch, card, wake, created_by, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT (kind, repo, branch, card) DO NOTHING`,
			e.ID, e.Kind, e.Repo, e.Branch, e.Card, b2i(e.Wake), e.CreatedBy, ts(e.CreatedAt))
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		existed = n == 0
		if !existed {
			return nil
		}
		row := s.db.QueryRow(`SELECT `+eventSubCols+` FROM event_sub WHERE kind = ? AND repo = ? AND branch = ? AND card = ?`,
			e.Kind, e.Repo, e.Branch, e.Card)
		cur, err := scanEventSub(row)
		if err != nil {
			return err
		}
		e = cur
		return nil
	})
	return e, existed, err
}

// EventSubGet is one subscription by id, and whether it exists.
func (s *Store) EventSubGet(id string) (EventSub, bool, error) {
	subs, err := s.eventSubs(`WHERE id = ?`, id)
	if err != nil || len(subs) == 0 {
		return EventSub{}, false, err
	}
	return subs[0], true, nil
}

// EventSubDelete removes one subscription and says whether there was one.
func (s *Store) EventSubDelete(id string) (bool, error) {
	var n int64
	err := s.guard(func() error {
		res, err := s.db.Exec(`DELETE FROM event_sub WHERE id = ?`, id)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	return n > 0, err
}

// EventSubsOf is every subscription of one card, oldest first.
func (s *Store) EventSubsOf(card string) ([]EventSub, error) {
	return s.eventSubs(`WHERE card = ? ORDER BY created_at, id`, card)
}

// EventSubsAll is every subscription, oldest first.
func (s *Store) EventSubsAll() ([]EventSub, error) {
	return s.eventSubs(`ORDER BY created_at, id`)
}

// EventSubsMatching is the subscriptions an event of this kind, repo and branch is for.
func (s *Store) EventSubsMatching(kind, repo, branch string) ([]EventSub, error) {
	return s.eventSubs(`WHERE kind = ? AND (repo = '' OR repo = ? COLLATE NOCASE) AND (branch = '' OR branch = ?)
		ORDER BY created_at, id`, kind, repo, branch)
}

// EventSubDeleteCard removes every subscription of a card that is gone, and says how many there were.
func (s *Store) EventSubDeleteCard(card string) (int64, error) {
	var n int64
	err := s.guard(func() error {
		res, err := s.db.Exec(`DELETE FROM event_sub WHERE card = ?`, card)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	return n, err
}

// EventDelivery is one subscription's copy of one event. Target is the card, `room~id`. Kind, Repo and Branch are the
// event's, read through the join.
type EventDelivery struct {
	ID       string
	EventRef string
	Target   string
	Kind     string
	Wake     bool
	Text     string
	State    string
	Answer   string
	Card     string
	Attempts int
	Created  time.Time
	Updated  time.Time

	EventKind string
	Repo      string
	Branch    string
}

// EventDeliveryAdd records a target for an event, pending.
func (s *Store) EventDeliveryAdd(d EventDelivery) (string, error) {
	d.ID = newID()
	now := ts(s.now())
	err := s.guard(func() error {
		_, err := s.db.Exec(
			`INSERT INTO event_delivery (id, event_ref, target, kind, wake, text, state, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, 'pending', ?, ?)`,
			d.ID, d.EventRef, d.Target, d.Kind, b2i(d.Wake), d.Text, now, now)
		return err
	})
	return d.ID, err
}

const eventDeliveryCols = `d.id, d.event_ref, d.target, d.kind, d.wake, d.text, d.state, d.answer, d.card, d.attempts,
	d.created_at, d.updated_at, e.kind, e.repo, e.branch`

func scanEventDelivery(sc scanner) (EventDelivery, error) {
	var d EventDelivery
	var wake int
	var created, updated string
	if err := sc.Scan(&d.ID, &d.EventRef, &d.Target, &d.Kind, &wake, &d.Text, &d.State, &d.Answer, &d.Card, &d.Attempts,
		&created, &updated, &d.EventKind, &d.Repo, &d.Branch); err != nil {
		return d, err
	}
	d.Wake = wake != 0
	if t := parseOrNil(created); t != nil {
		d.Created = *t
	}
	if t := parseOrNil(updated); t != nil {
		d.Updated = *t
	}
	return d, nil
}

// EventDeliveriesPending is every event delivery still to be made, oldest event first.
func (s *Store) EventDeliveriesPending() ([]EventDelivery, error) {
	return s.eventDeliveries(`WHERE d.state = 'pending' ORDER BY e.received_at, d.created_at, d.id`)
}

// EventDeliveriesOf is every delivery of one event, in the order they were added.
func (s *Store) EventDeliveriesOf(eventRef string) ([]EventDelivery, error) {
	return s.eventDeliveries(`WHERE d.event_ref = ? ORDER BY d.created_at, d.id`, eventRef)
}

func (s *Store) eventDeliveries(where string, args ...any) ([]EventDelivery, error) {
	var out []EventDelivery
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT `+eventDeliveryCols+` FROM event_delivery d JOIN hub_event e ON e.id = d.event_ref `+
			where, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanEventDelivery(rows)
			if err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}

// EventDeliverySet moves a delivery to a state, with the room's answer and the card it went to.
func (s *Store) EventDeliverySet(id, state, answer, card string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(
			`UPDATE event_delivery SET state = ?, answer = ?, card = ?, attempts = attempts + 1, updated_at = ?
			 WHERE id = ?`, state, answer, card, ts(s.now()), id)
		return err
	})
}

// EventSweep drops events older than CIKeep and their deliveries.
func (s *Store) EventSweep() (int64, error) {
	var n int64
	cut := ts(s.now().Add(-CIKeep))
	err := s.guard(func() error {
		if _, err := s.db.Exec(
			`DELETE FROM event_delivery WHERE event_ref IN (SELECT id FROM hub_event WHERE received_at < ?)`, cut); err != nil {
			return err
		}
		res, err := s.db.Exec(`DELETE FROM hub_event WHERE received_at < ?`, cut)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	return n, err
}

// SplitEventCard reads a subscription's card, `room~id`.
func SplitEventCard(card string) (room, id string, ok bool) {
	room, id, ok = strings.Cut(card, "~")
	return room, id, ok && room != "" && id != "" && !strings.Contains(id, "~")
}
