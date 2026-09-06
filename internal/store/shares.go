package store

import (
	"database/sql"
	"strings"
)

// A card that is lent out, remembered across restarts.
//
// The first version of sharing was deliberately amnesiac: an address was
// generated, handed over, and released on shutdown, so a share lived exactly
// as long as the process that made it. That was defended on the grounds that
// the address IS the credential, since there is no login, and a reserved
// address would mean handing out the same guessable one forever.
//
// The defence conflated two independent things. UNGUESSABLE and DURABLE are
// not the same property. A name generated once, at random, held by the
// controller, and asked for again on every start is both: nobody can guess
// `atrium-k4m2p9x7qd3f`, and the link somebody was given still works tomorrow.
//
// So a share is a fact about a card now, not a fact about a process. What is
// stored is enough to put the same address back up and nothing more: which
// overlay, which mode, the reserved name, the last token, and whether the
// operator still wants it up.

// CardShare is one card's standing share.
type CardShare struct {
	// TaskID is the card. One share per card, which is why it is the key: two
	// addresses reaching the same terminal is two things to remember to stop.
	TaskID string `json:"task_id"`
	// Kind is which overlay made it. Only `zrok` today, and the column exists
	// so that a second one does not need a migration to be told apart from the
	// first. A stop that guesses wrong releases somebody else's share.
	Kind string `json:"kind"`
	// Mode is public or private. They are different things to hand somebody and
	// they are rebound differently: a public share asks for its NAME back, a
	// private one asks for its TOKEN back.
	Mode string `json:"mode"`
	// Namespace and Name are the reserved name, for a public share. Empty for a
	// private one, which has no name at all.
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
	// Token is the share token as of the last time it was bound. It is the
	// address for a private share, and for a public one it is what releases the
	// share. It CHANGES on every rebind, so nothing may treat it as identity.
	Token string `json:"token,omitempty"`
	// Address is what was handed over, kept so the board can show it without a
	// live share to read it off. For a public share this is the durable part
	// and the whole reason any of this is stored.
	Address string `json:"address,omitempty"`
	// Wanted is whether the operator still wants this card lent out. It is what
	// a restart consults. Stopping a share clears it, and a shutdown does NOT:
	// that difference is the entire feature.
	Wanted bool `json:"wanted"`
	// CreatedAt is when it was first shared, BoundAt when it last came up.
	// Two timestamps because they answer different questions: how long somebody
	// has had this link, and whether it survived the last restart.
	CreatedAt string `json:"created_at"`
	BoundAt   string `json:"bound_at,omitempty"`
}

// PutCardShare records a share, or updates the one already there.
//
// Upsert rather than insert, because rebinding writes the same row with a new
// token and re-sharing a card that was stopped writes it with a new name. A
// caller that had to know which it was doing would get it wrong on the path
// that matters least and fail on the path that matters most.
func (s *Store) PutCardShare(c CardShare) error {
	mode := strings.TrimSpace(c.Mode)
	kind := strings.TrimSpace(c.Kind)
	if kind == "" {
		kind = "zrok"
	}
	wanted := 0
	if c.Wanted {
		wanted = 1
	}
	created := c.CreatedAt
	if created == "" {
		created = ts(now())
	}
	return s.guard(func() error {
		_, err := s.db.Exec(
			`INSERT INTO card_share
			   (task_id, kind, mode, namespace, name, token, address, wanted, created_at, bound_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(task_id) DO UPDATE SET
			   kind      = excluded.kind,
			   mode      = excluded.mode,
			   namespace = excluded.namespace,
			   name      = excluded.name,
			   token     = excluded.token,
			   address   = excluded.address,
			   wanted    = excluded.wanted,
			   bound_at  = excluded.bound_at`,
			c.TaskID, kind, mode, c.Namespace, c.Name, c.Token, c.Address,
			wanted, created, c.BoundAt)
		return err
	})
}

// CardShareFor reads one card's share, or nil when there is none.
//
// A card that has never been shared and a card whose share was stopped both
// answer nil. They are the same state: `wanted` is false and there is nothing
// to put back up. What is kept for the second one is the row, so the name can
// be released rather than left on the account.
func (s *Store) CardShareFor(taskID string) (*CardShare, error) {
	var c CardShare
	var wanted int
	err := s.guard(func() error {
		err := s.db.QueryRow(
			`SELECT task_id, kind, mode, namespace, name, token, address,
			        wanted, created_at, bound_at
			   FROM card_share WHERE task_id = ?`, taskID).
			Scan(&c.TaskID, &c.Kind, &c.Mode, &c.Namespace, &c.Name, &c.Token,
				&c.Address, &wanted, &c.CreatedAt, &c.BoundAt)
		if err == sql.ErrNoRows {
			c.TaskID = ""
			return nil
		}
		return err
	})
	if err != nil || c.TaskID == "" {
		return nil, err
	}
	c.Wanted = wanted == 1
	return &c, nil
}

// WantedCardShares is every share that should be up.
//
// What a restart reads. Ordered by card so a boot that reports what it is
// rebinding does not reorder its own log between runs.
func (s *Store) WantedCardShares() ([]CardShare, error) {
	var out []CardShare
	err := s.guard(func() error {
		rows, err := s.db.Query(
			`SELECT task_id, kind, mode, namespace, name, token, address, created_at, bound_at
			   FROM card_share WHERE wanted = 1 ORDER BY task_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c CardShare
			if err := rows.Scan(&c.TaskID, &c.Kind, &c.Mode, &c.Namespace, &c.Name,
				&c.Token, &c.Address, &c.CreatedAt, &c.BoundAt); err != nil {
				return err
			}
			c.Wanted = true
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

// UnwantCardShare records that the operator stopped this share.
//
// The row SURVIVES, holding the name that is still reserved on the account.
// Deleting it here would lose the only record of a name atrium reserved, which
// is how an account fills up with names nothing will ever ask for again.
// `ForgetCardShare` is the one that removes it, once the name is released.
func (s *Store) UnwantCardShare(taskID string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(
			`UPDATE card_share SET wanted = 0, token = '' WHERE task_id = ?`, taskID)
		return err
	})
}

// ForgetCardShare drops the row, once whatever it named has been released.
func (s *Store) ForgetCardShare(taskID string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`DELETE FROM card_share WHERE task_id = ?`, taskID)
		return err
	})
}

// StaleCardShares is every share row whose card has gone.
//
// A card can be pruned while its share is recorded, and the row left behind
// names a reserved name on the account that nothing will ever ask for again.
// Answering with them is how the sweep finds what to release: the name outlives
// the card and only this join knows it.
func (s *Store) StaleCardShares() ([]CardShare, error) {
	var out []CardShare
	err := s.guard(func() error {
		rows, err := s.db.Query(
			`SELECT cs.task_id, cs.kind, cs.mode, cs.namespace, cs.name, cs.token,
			        cs.address, cs.wanted, cs.created_at, cs.bound_at
			   FROM card_share cs
			   LEFT JOIN task t ON t.id = cs.task_id
			  WHERE t.id IS NULL ORDER BY cs.task_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c CardShare
			var wanted int
			if err := rows.Scan(&c.TaskID, &c.Kind, &c.Mode, &c.Namespace, &c.Name,
				&c.Token, &c.Address, &wanted, &c.CreatedAt, &c.BoundAt); err != nil {
				return err
			}
			c.Wanted = wanted == 1
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}
