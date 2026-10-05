package hubstore

import (
	"database/sql"
	"errors"
)

// Web push subscriptions: one row per browser. See migration 0011 and docs/rnd/web-push-design.md.
//
//   - THE ENDPOINT IS THE PROOF OF OWNERSHIP. Only the browser that subscribed knows it, so a removal from the share
//     names the endpoint and never the id, which is not a secret.
//   - AT MOST PushMax ROWS, AND NOTHING IS EVICTED. A ninth is refused, so a stranger who fills the slots cannot push
//     the operator's own phone out.
//   - THE KEYS STAY HERE. PushSub carries p256dh and auth because the sender needs them, and the API layer never
//     encodes them.

// PushMax is how many subscriptions the hub holds.
const PushMax = 8

// ErrPushFull is a subscribe over the cap.
var ErrPushFull = errors.New("8 devices already get alerts. Remove one in the desktop gear first.")

// ErrNoPushSub is an id or an endpoint nobody holds.
var ErrNoPushSub = errors.New("no such subscription")

// PushSub is one subscription.
type PushSub struct {
	ID             string
	Endpoint       string
	P256dh         string
	Auth           string
	Label          string
	Origin         string
	CreatedAt      string
	Failures       int
	DisabledReason string
}

const pushCols = `id, endpoint, p256dh, auth, label, origin, created_at, failures, disabled_reason`

func scanPushSub(sc scanner) (PushSub, error) {
	var p PushSub
	err := sc.Scan(&p.ID, &p.Endpoint, &p.P256dh, &p.Auth, &p.Label, &p.Origin, &p.CreatedAt, &p.Failures,
		&p.DisabledReason)
	return p, err
}

// PushSubs lists every subscription, oldest first.
func (s *Store) PushSubs() ([]PushSub, error) {
	var out []PushSub
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT ` + pushCols + ` FROM push_subscription ORDER BY created_at, id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scanPushSub(rows)
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// PushAdd stores a subscription. An endpoint already held is the same browser subscribing again: its keys and label
// are replaced, it is switched back on, and it is not a new device (added is false) and does not count toward the cap.
func (s *Store) PushAdd(p PushSub) (id string, added bool, err error) {
	err = s.tx(func(t *sql.Tx) error {
		var have string
		switch e := t.QueryRow(`SELECT id FROM push_subscription WHERE endpoint = ?`, p.Endpoint).Scan(&have); {
		case e == nil:
			id, added = have, false
			_, e = t.Exec(`UPDATE push_subscription SET p256dh = ?, auth = ?, label = ?, origin = ?, failures = 0,
				disabled_reason = '' WHERE id = ?`, p.P256dh, p.Auth, p.Label, p.Origin, have)
			return e
		case !errors.Is(e, sql.ErrNoRows):
			return e
		}
		var n int
		if e := t.QueryRow(`SELECT COUNT(*) FROM push_subscription`).Scan(&n); e != nil {
			return e
		}
		if n >= PushMax {
			return refuse(ErrPushFull)
		}
		id, added = newID(), true
		_, e := t.Exec(`INSERT INTO push_subscription (`+pushCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, 0, '')`,
			id, p.Endpoint, p.P256dh, p.Auth, p.Label, p.Origin, ts(now()))
		return e
	})
	if errors.Is(err, ErrPushFull) {
		return "", false, ErrPushFull
	}
	return id, added, err
}

// PushRemoveID removes by id, which is the operator's.
func (s *Store) PushRemoveID(id string) error { return s.pushRemove(`id = ?`, id) }

// PushRemoveEndpoint removes by endpoint, which is the browser's own proof.
func (s *Store) PushRemoveEndpoint(endpoint string) error {
	return s.pushRemove(`endpoint = ?`, endpoint)
}

func (s *Store) pushRemove(where, arg string) error {
	return s.guard(func() error {
		res, err := s.db.Exec(`DELETE FROM push_subscription WHERE `+where, arg)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return refuse(ErrNoPushSub)
		}
		return nil
	})
}

// PushOutcome records one send. A success clears the count. A failure adds one and, when it reaches `limit`, switches
// the subscription off with the reason. It returns the count after.
func (s *Store) PushOutcome(id string, ok bool, reason string, limit int) (failures int, err error) {
	err = s.tx(func(t *sql.Tx) error {
		if ok {
			failures = 0
			_, e := t.Exec(`UPDATE push_subscription SET failures = 0 WHERE id = ?`, id)
			return e
		}
		if e := t.QueryRow(`SELECT failures FROM push_subscription WHERE id = ?`, id).Scan(&failures); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return nil
			}
			return e
		}
		failures++
		off := ""
		if failures >= limit {
			off = reason
		}
		_, e := t.Exec(`UPDATE push_subscription SET failures = ?, disabled_reason = CASE WHEN ? <> '' THEN ?
			ELSE disabled_reason END WHERE id = ?`, failures, off, off, id)
		return e
	})
	return failures, err
}
