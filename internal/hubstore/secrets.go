package hubstore

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

// The join secret: one per room, shown once, good once.
//
// ── what changed, and why it is better ──────────────────
//
// What exists today is a list of hashes in `pending.json` that authorise A
// join, not a join AS ANYTHING IN PARTICULAR. The room names itself at
// enrolment and the hub signs whatever it asked for, which is why the hub
// carries a hand-written check refusing a second room that claims a name
// already attached.
//
// Binding the secret to a room closes that by construction. A secret
// authorises exactly one name, so there is nothing for a room to claim and no
// check to write. See docs/decisions.md, 7 and 8.

// secretLife bounds a join string: long enough to walk to another machine,
// short enough that one left in a chat log has stopped being a key.
const secretLife = time.Hour

// ErrBadSecret is what an unrecognised, spent or expired join secret comes
// back as.
var ErrBadSecret = errors.New("that join string has been used already, or it expired")

// Mint makes this room a fresh join secret and returns it in the clear, once.
//
// THE ONLY TIME THE CLEAR SECRET EXISTS. What is stored is its SHA-256, the way
// a password is, because the hub only ever compares and has no business holding
// the original. Losing the string means minting another, which is what makes
// "copy once" a fact rather than a label.
//
// Minting replaces whatever the room had. A room cannot accumulate a drawer
// full of working credentials nobody remembers issuing.
func (s *Store) Mint(roomID string) (string, error) {
	r, err := s.Get(roomID)
	if err != nil {
		return "", err
	}
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	secret := base64.RawURLEncoding.EncodeToString(raw[:])
	sum := sha256.Sum256([]byte(secret))

	made, expires := now(), now().Add(secretLife)
	if err := s.guard(func() error {
		_, err := s.db.Exec(
			`INSERT INTO room_secret (room_id, hash, created_at, expires_at)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT (room_id) DO UPDATE SET hash = excluded.hash,
			                                     created_at = excluded.created_at,
			                                     expires_at = excluded.expires_at`,
			roomID, base64.RawURLEncoding.EncodeToString(sum[:]), ts(made), ts(expires))
		return err
	}); err != nil {
		return "", err
	}
	s.Log(r, "secret-minted", fmt.Sprintf("a join string was issued, good once until %s",
		expires.Format("15:04")))
	return secret, nil
}

// Outstanding reports whether this room has a join secret waiting to be used,
// and when it expires.
//
// So the rooms tab can say "waiting to join" rather than leaving somebody to
// work out from an empty last-seen column whether they ever sent the string.
func (s *Store) Outstanding(roomID string) (bool, time.Time, error) {
	var expires string
	err := s.guard(func() error {
		err := s.db.QueryRow(
			`SELECT expires_at FROM room_secret WHERE room_id = ?`, roomID).Scan(&expires)
		if errors.Is(err, sql.ErrNoRows) {
			expires = ""
			return nil
		}
		return err
	})
	if err != nil || expires == "" {
		return false, time.Time{}, err
	}
	at := parseOrNil(expires)
	if at == nil || !now().Before(*at) {
		return false, time.Time{}, nil
	}
	return true, *at, nil
}

// Spend consumes a join secret and says which room it was for.
//
// GOOD ONCE, and that is how you find out a join string went somewhere it
// should not have: the second paste fails.
//
// Looked up by hash rather than compared one row at a time. There is no timing
// question here: the column holds a SHA-256 and learning how long an indexed
// lookup of a hash took says nothing about the twenty four random bytes behind
// it. What the old list-and-compare loop was defending was a secret that could
// be guessed a character at a time, which this never could be.
func (s *Store) Spend(secret string) (*Room, error) {
	if secret == "" {
		return nil, ErrBadSecret
	}
	sum := sha256.Sum256([]byte(secret))
	want := base64.RawURLEncoding.EncodeToString(sum[:])

	var roomID string
	var expires string
	err := s.guard(func() error {
		err := s.db.QueryRow(
			`SELECT room_id, expires_at FROM room_secret WHERE hash = ?`, want).
			Scan(&roomID, &expires)
		if errors.Is(err, sql.ErrNoRows) {
			return refuse(ErrBadSecret)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	at := parseOrNil(expires)
	if at == nil || !now().Before(*at) {
		// EXPIRED IS SPENT. Deleted here rather than left, because a row that
		// can never succeed is one more thing on a list of things somebody has
		// to reason about.
		_ = s.guard(func() error {
			_, err := s.db.Exec(`DELETE FROM room_secret WHERE room_id = ?`, roomID)
			return err
		})
		return nil, ErrBadSecret
	}

	// THE DELETE IS WHAT MAKES IT SINGLE USE, and it is conditional on the hash
	// still being there so two enrolments racing cannot both succeed. The old
	// implementation read a list, filtered it and wrote it back, which meant
	// two interleaved enrolments could resurrect a spent secret: both read the
	// same list, the first wrote it back without the secret, and the second
	// wrote back ITS copy, which still had it.
	var spent bool
	if err := s.guard(func() error {
		res, err := s.db.Exec(
			`DELETE FROM room_secret WHERE room_id = ? AND hash = ?`, roomID, want)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		spent = n > 0
		return err
	}); err != nil {
		return nil, err
	}
	if !spent {
		return nil, ErrBadSecret
	}

	r, err := s.Get(roomID)
	if err != nil {
		return nil, err
	}
	s.Log(r, "joined", "a join string was spent and this room has a credential")
	return r, nil
}

// Sweep drops join secrets that have expired.
//
// Housekeeping rather than security: an expired secret is already refused by
// `Spend`. This stops the table accumulating rows for rooms somebody made and
// never got round to connecting.
func (s *Store) Sweep() error {
	return s.guard(func() error {
		_, err := s.db.Exec(`DELETE FROM room_secret WHERE expires_at < ?`, ts(now()))
		return err
	})
}
