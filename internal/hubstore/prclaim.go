package hubstore

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
)

// The PR claim table: one owner per PR key across rooms. See migration 0010 and docs/rnd/scm-forge-design.md section 6.
//
//   - AN INDEX, NOT A COPY. The row, the findings and the run folder stay on the owning room.
//   - THE FIRST CLAIM WINS. Claim on a key that has an owner answers that owner and changes nothing, so two rooms that
//     find the same PR get one answer.
//   - NEVER RE-PLACED BY ITSELF. Nothing here releases a claim because its room went away. Only Move, which is the
//     operator's, changes the owner.

// PRClaim is one owner.
type PRClaim struct {
	Key       string `json:"key"`
	Room      string `json:"room"`
	Source    string `json:"source,omitempty"`
	ClaimedAt string `json:"claimed_at"`
	// Warned is true while the offline growler for this spell is up, and WarnN numbers the spells.
	Warned bool `json:"-"`
	WarnN  int  `json:"-"`
}

// ErrNoPRClaim is a key nobody owns.
var ErrNoPRClaim = errors.New("no room has claimed that pr")

// PRKey is the canonical key of a pull request, host/org/repo/number in lower case.
func PRKey(host, org, repo string, number int) string {
	return strings.ToLower(host + "/" + org + "/" + repo + "/" + strconv.Itoa(number))
}

const prClaimCols = `key, room, source, claimed_at, warned, warn_n`

func scanPRClaim(sc scanner) (PRClaim, error) {
	var c PRClaim
	var warned int
	if err := sc.Scan(&c.Key, &c.Room, &c.Source, &c.ClaimedAt, &warned, &c.WarnN); err != nil {
		return PRClaim{}, err
	}
	c.Warned = warned != 0
	return c, nil
}

// PRClaimOf is the claim on a key, or ErrNoPRClaim.
func (s *Store) PRClaimOf(key string) (PRClaim, error) {
	var c PRClaim
	err := s.guard(func() error {
		var e error
		c, e = scanPRClaim(s.db.QueryRow(`SELECT `+prClaimCols+` FROM pr_claim WHERE key = ?`, key))
		return e
	})
	if errors.Is(err, sql.ErrNoRows) {
		return PRClaim{}, ErrNoPRClaim
	}
	return c, err
}

// ClaimPR records `room` as the owner of key unless the key already has one. It answers the owner either way, and
// whether this call made the claim.
func (s *Store) ClaimPR(key, room, source string) (owner PRClaim, made bool, err error) {
	err = s.tx(func(t *sql.Tx) error {
		made = false
		res, err := t.Exec(`INSERT INTO pr_claim (key, room, source, claimed_at) VALUES (?, ?, ?, ?)
			ON CONFLICT (key) DO NOTHING`, key, room, source, ts(now()))
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		made = n == 1
		owner, err = scanPRClaim(t.QueryRow(`SELECT `+prClaimCols+` FROM pr_claim WHERE key = ?`, key))
		return err
	})
	return owner, made, err
}

// MovePRClaim gives a key to another room, the operator's hand move. It also ends the offline spell, since the
// owner is no longer the room that was offline. The key must have an owner.
func (s *Store) MovePRClaim(key, room string) (PRClaim, error) {
	var c PRClaim
	err := s.tx(func(t *sql.Tx) error {
		res, err := t.Exec(`UPDATE pr_claim SET room = ?, warned = 0 WHERE key = ?`, room, key)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return sql.ErrNoRows
		}
		c, err = scanPRClaim(t.QueryRow(`SELECT `+prClaimCols+` FROM pr_claim WHERE key = ?`, key))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return PRClaim{}, ErrNoPRClaim
	}
	return c, err
}

// ReleasePRClaim deletes the claim on key while room still owns it, so the next paste places the key again. It is for
// a placement the room refused: nothing was made there. It answers the claim it deleted, and ErrNoPRClaim when the key
// was not room's, which leaves a claim that moved meanwhile alone.
func (s *Store) ReleasePRClaim(key, room string) (PRClaim, error) {
	var c PRClaim
	err := s.tx(func(t *sql.Tx) error {
		var err error
		c, err = scanPRClaim(t.QueryRow(`SELECT `+prClaimCols+` FROM pr_claim WHERE key = ? AND room = ?`, key, room))
		if err != nil {
			return err
		}
		_, err = t.Exec(`DELETE FROM pr_claim WHERE key = ? AND room = ?`, key, room)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return PRClaim{}, ErrNoPRClaim
	}
	return c, err
}

// PRClaims lists every claim, by key.
func (s *Store) PRClaims() ([]PRClaim, error) {
	var out []PRClaim
	err := s.guard(func() error {
		rows, err := s.db.Query(`SELECT ` + prClaimCols + ` FROM pr_claim ORDER BY key`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanPRClaim(rows)
			if err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

// PRClaimWarn flips a claim's offline spell. On with the spell not yet warned starts a new spell and answers its
// number, so the caller can raise a growler under an id no earlier spell used. Off ends the spell. ok is false when
// nothing changed.
func (s *Store) PRClaimWarn(key string, on bool) (n int, ok bool, err error) {
	err = s.tx(func(t *sql.Tx) error {
		n, ok = 0, false
		c, err := scanPRClaim(t.QueryRow(`SELECT `+prClaimCols+` FROM pr_claim WHERE key = ?`, key))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if c.Warned == on {
			n = c.WarnN
			return nil
		}
		next := c.WarnN
		warned := 0
		if on {
			next++
			warned = 1
		}
		if _, err := t.Exec(`UPDATE pr_claim SET warned = ?, warn_n = ? WHERE key = ?`, warned, next, key); err != nil {
			return err
		}
		n, ok = next, true
		return nil
	})
	return n, ok, err
}
