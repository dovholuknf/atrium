package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Work handed to another machine.
//
// `internal/daemon/rooms.go` carries cards INWARD: a room dials this hub every
// twenty seconds and says what is on it. This is the other direction, and it
// is deliberately not a second connection. THE HUB NEVER DIALS A ROOM. A queued
// launch sits here until the room it names checks in, and rides back down the
// reply to the check-in it was already making. Nothing has to be reachable that
// was not already reachable, and a room behind NAT is not a special case.
//
// WHY A ROW WITH A ROOM NAME ON IT, RATHER THAN AN OFFER ROOMS COMPETE FOR.
// An offer pool answers "somebody run this", and that is not the question. A
// laptop, an M1 mini and two cloud boxes are not interchangeable: they have
// different toolchains, different checkouts and different network reach, and
// the operator handing an item over already knows which one it belongs on. An
// offer pool would also make the hub arbitrate a race it has no reason to
// have. A named row makes the common case exact and the race trivial.
//
// TWO ROOMS CANNOT BOTH TAKE ONE ITEM, and the guarantee is not the name. Two
// `atrium room` processes with the SAME name is the real race: a second copy
// started by hand, or a retry after the reply to a check-in was lost. So a
// claim is a conditional state change, `queued` to `claimed` in one statement,
// and the loser of that statement is told about nothing. The claimant is
// handed a token, and a result is only accepted from whoever holds it.
//
// WHAT THIS TABLE IS NOT is a copy of a remote card. Once a room reports that
// it started something, this row is finished and the work is visible the way
// every other remote card is: in that room's own check-in, on that room's own
// board. A durable copy of somebody else's card here is the thing federation
// rules out.

// The states a queued launch moves through.
//
// `running` is terminal HERE. It means the room said it started something, and
// from that moment the room is the truth about it, exactly as it is about every
// other card on it. The hub stops tracking and the work appears in that room's
// check-in like anything else.
const (
	DispatchQueued    = "queued"
	DispatchClaimed   = "claimed"
	DispatchRunning   = "running"
	DispatchFailed    = "failed"
	DispatchCancelled = "cancelled"
)

// MaxDispatchPrompt bounds the instruction a queued launch may carry.
//
// The same reason `MaxActionPrompt` is bounded and a larger number: this is the
// whole brief for a session on another machine, which is a page rather than a
// sentence, and it is still not a place to keep a document.
const MaxDispatchPrompt = 16000

// MaxOpenPerRoom is how many unfinished items one room may have.
//
// It bounds the reply to a check-in, which is the reason it exists. A script
// that queues in a loop against a room that is switched off would otherwise
// grow the body of every heartbeat that room ever makes.
const MaxOpenPerRoom = 50

// MaxHandout is how many items one check-in may carry away.
//
// A room that has been offline for an hour comes back to a queue and starts
// everything on it at once otherwise. Three at a time, twenty seconds apart, is
// slower and is the behaviour somebody can watch.
//
// THE SIZE IS TIED TO THE LEASE. A room runs its handout one item at a time and
// bounds each launch at a minute, so a full batch is three minutes against a
// five minute lease. Raising this without raising `DispatchLease` means the hub
// takes back an item the room is still starting, which is how one instruction
// becomes two runners in one directory.
const MaxHandout = 3

// DispatchLease is how long a claim is good for before the hub takes it back.
//
// A room that claims an item and then loses power never reports anything, and
// without this the item is stuck in `claimed` until a human notices. Comfortably
// longer than a full handout takes to run, so a room that is merely slow is not
// stolen out from under. See `MaxHandout`.
const DispatchLease = 5 * time.Minute

// MaxDispatchAttempts is how many times one item may be handed out.
//
// Two, and the second one is the retry. Beyond that the item is not unlucky,
// it is wrong: a harness that does not exist on that room, or a directory that
// is never going to be there. Handing it out forever would mean a permanently
// broken item starting a permanently broken launch every time that machine
// comes back.
const MaxDispatchAttempts = 2

// Dispatch is one launch queued for another machine.
type Dispatch struct {
	ID string `json:"id"`
	// Room is the name that room reports itself under. It is the whole of the
	// addressing: an item is only ever offered to the room named here.
	Room string `json:"room"`
	// Harness is the runner id AS THAT ROOM KNOWS IT. Not checked here, because
	// this hub's harness table is a fact about this machine and says nothing
	// about what is configured on cdaws.
	Harness string `json:"harness"`
	// Cwd is a directory ON THAT ROOM, and it is optional on purpose. See
	// `docs/remote-launch.md`: the ordinary case sends no directory at all and
	// lets the room's own harness row answer, because the room is the authority
	// on its own filesystem and this hub has never seen it.
	Cwd    string   `json:"cwd,omitempty"`
	Title  string   `json:"title,omitempty"`
	Prompt string   `json:"prompt,omitempty"`
	Why    string   `json:"why,omitempty"`
	Tags   []string `json:"tags,omitempty"`
	Window string   `json:"window,omitempty"`

	State string `json:"state"`
	// Token is what the claiming room must send back with a result. Never
	// rendered on the board and never returned by `Dispatches`.
	Token    string `json:"-"`
	Attempts int    `json:"attempts"`
	// CardID and CardURL are what the room said it made. A pointer, not a copy:
	// the card lives there.
	CardID  string `json:"card_id,omitempty"`
	CardURL string `json:"card_url,omitempty"`
	// Error is why it did not start, in the room's own words. This is the field
	// the whole refusal path exists to fill in.
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	// Pointers, so an item that has not been claimed or settled carries no
	// timestamp at all. `omitempty` does nothing for a zero `time.Time`, and a
	// row that says it was claimed on the first of January year one is worse
	// than a row that says nothing.
	ClaimedAt *time.Time `json:"claimed_at,omitempty"`
	SettledAt *time.Time `json:"settled_at,omitempty"`
}

// Open reports whether this item is still going somewhere.
func (d *Dispatch) Open() bool {
	return d.State == DispatchQueued || d.State == DispatchClaimed
}

const dispatchColumns = `id, room, harness, cwd, title, prompt, why, tags, window_name,
	state, token, attempts, card_id, card_url, error, created_at, claimed_at, settled_at`

func scanDispatch(sc interface{ Scan(...any) error }) (*Dispatch, error) {
	var (
		d                         Dispatch
		tags                      string
		created, claimed, settled string
	)
	if err := sc.Scan(&d.ID, &d.Room, &d.Harness, &d.Cwd, &d.Title, &d.Prompt, &d.Why,
		&tags, &d.Window, &d.State, &d.Token, &d.Attempts, &d.CardID, &d.CardURL,
		&d.Error, &created, &claimed, &settled); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(orDefault(tags, "[]")), &d.Tags); err != nil {
		return nil, err
	}
	var err error
	if d.CreatedAt, err = parseTS(created); err != nil {
		return nil, err
	}
	if strings.TrimSpace(claimed) != "" {
		if at, err := parseTS(claimed); err == nil {
			d.ClaimedAt = &at
		}
	}
	if strings.TrimSpace(settled) != "" {
		if at, err := parseTS(settled); err == nil {
			d.SettledAt = &at
		}
	}
	return &d, nil
}

// QueueDispatch files a launch for another machine.
func (s *Store) QueueDispatch(d Dispatch) (*Dispatch, error) {
	d.Room = strings.TrimSpace(d.Room)
	d.Harness = strings.TrimSpace(d.Harness)
	if d.Room == "" {
		return nil, errors.New("say which room this is for")
	}
	if d.Harness == "" {
		return nil, errors.New("say which runner to start there")
	}
	if len(d.Prompt) > MaxDispatchPrompt {
		return nil, fmt.Errorf("that instruction is %d characters and the limit is %d",
			len(d.Prompt), MaxDispatchPrompt)
	}
	tags, err := json.Marshal(orEmptySlice(d.Tags))
	if err != nil {
		return nil, err
	}

	d.ID = newID()
	d.State = DispatchQueued
	// A REFUSAL IS NOT A STORAGE FAILURE, and `guard` cannot tell the two apart:
	// anything it gets back that is not contention halts the store. So the full
	// queue is reported out here, through a variable, and the closure only ever
	// returns what sqlite said. Getting this wrong means a script queueing one
	// item too many takes the whole daemon down.
	open := 0
	err = s.guard(func() error {
		open = 0
		if err := s.db.QueryRow(
			`SELECT COUNT(*) FROM dispatch WHERE room = ? AND state IN (?, ?)`,
			d.Room, DispatchQueued, DispatchClaimed).Scan(&open); err != nil {
			return err
		}
		if open >= MaxOpenPerRoom {
			return nil
		}
		_, err := s.db.Exec(`INSERT INTO dispatch (`+dispatchColumns+`)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			d.ID, d.Room, d.Harness, d.Cwd, d.Title, d.Prompt, d.Why, string(tags),
			d.Window, d.State, "", 0, "", "", "", ts(now()), "", "")
		return err
	})
	if err != nil {
		return nil, err
	}
	if open >= MaxOpenPerRoom {
		return nil, fmt.Errorf("%s already has %d items waiting on it. "+
			"clear some before queueing more", d.Room, open)
	}
	return s.Dispatch(d.ID)
}

// Dispatch returns one queued launch.
func (s *Store) Dispatch(id string) (*Dispatch, error) {
	var d *Dispatch
	err := s.guard(func() error {
		row := s.db.QueryRow(`SELECT `+dispatchColumns+` FROM dispatch WHERE id = ?`, id)
		got, err := scanDispatch(row)
		if err != nil {
			return err
		}
		d = got
		return nil
	})
	return d, err
}

// Dispatches lists the queue, newest first.
//
// Tokens are blanked. Nothing above this layer has a reason to see one, and the
// board draws this list.
func (s *Store) Dispatches(room string) ([]*Dispatch, error) {
	var out []*Dispatch
	err := s.guard(func() error {
		out = nil
		q := `SELECT ` + dispatchColumns + ` FROM dispatch`
		args := []any{}
		if strings.TrimSpace(room) != "" {
			q += ` WHERE room = ?`
			args = append(args, strings.TrimSpace(room))
		}
		q += ` ORDER BY created_at DESC`
		rows, err := s.db.Query(q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanDispatch(rows)
			if err != nil {
				return err
			}
			d.Token = ""
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}

// ClaimDispatches hands the next few items to a room and marks them taken.
//
// THE CONDITIONAL UPDATE IS THE EXCLUSION. Selecting and then updating without
// the `state = 'queued'` predicate would let two callers both read a row as
// queued and both write it as claimed, which is exactly the case that matters:
// one room name, two `atrium room` processes. `RowsAffected` of zero means
// somebody else got there, and the item is silently left out of this handout
// rather than handed to both.
//
// Each claim gets a fresh token, so a reclaimed item's old holder can no longer
// report a result. That is deliberate. An item taken back after a lease expiry
// has been handed to somebody else, and accepting a late answer from the
// previous holder would overwrite the current one.
func (s *Store) ClaimDispatches(room string, max int) ([]*Dispatch, error) {
	room = strings.TrimSpace(room)
	if room == "" {
		return nil, errors.New("no room to hand work to")
	}
	if max <= 0 || max > MaxHandout {
		max = MaxHandout
	}
	var out []*Dispatch
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(
			`SELECT id FROM dispatch WHERE room = ? AND state = ? ORDER BY created_at ASC LIMIT ?`,
			room, DispatchQueued, max)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		for _, id := range ids {
			token := newID()
			res, err := s.db.Exec(
				`UPDATE dispatch SET state = ?, token = ?, attempts = attempts + 1,
					claimed_at = ? WHERE id = ? AND state = ?`,
				DispatchClaimed, token, ts(now()), id, DispatchQueued)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n == 0 {
				// Somebody claimed it between the select and here. Not an
				// error, and not this caller's item.
				continue
			}
			row := s.db.QueryRow(`SELECT `+dispatchColumns+` FROM dispatch WHERE id = ?`, id)
			d, err := scanDispatch(row)
			if err != nil {
				return err
			}
			out = append(out, d)
		}
		return nil
	})
	return out, err
}

// SettleDispatch records what the room did with an item.
//
// The token and the `claimed` state are both in the WHERE clause, so a result
// is refused from anybody who is not the current holder and refused for an item
// that has already finished. A room whose claim expired gets this refusal and
// says so in its own log, which is a readable outcome. Silently accepting it
// would let a room that came back an hour later overwrite an item somebody
// else has since run.
func (s *Store) SettleDispatch(id, token, state, cardID, cardURL, why string) (*Dispatch, error) {
	switch state {
	case DispatchRunning, DispatchFailed:
	default:
		return nil, fmt.Errorf("a room may only report running or failed, not %q", state)
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("a result has to carry the token it was handed")
	}
	var d *Dispatch
	// Refused OUT HERE rather than from inside the closure. See the note in
	// `QueueDispatch`: an error returned to `guard` halts the store, and a
	// result arriving with a stale token is an ordinary thing that happens
	// whenever a claim was taken back.
	took := false
	err := s.guard(func() error {
		took = false
		res, err := s.db.Exec(
			`UPDATE dispatch SET state = ?, card_id = ?, card_url = ?, error = ?,
				settled_at = ?, token = '' WHERE id = ? AND token = ? AND state = ?`,
			state, cardID, cardURL, firstLineOf(why), ts(now()), id, token, DispatchClaimed)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		took = true
		row := s.db.QueryRow(`SELECT `+dispatchColumns+` FROM dispatch WHERE id = ?`, id)
		got, err := scanDispatch(row)
		if err != nil {
			return err
		}
		got.Token = ""
		d = got
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !took {
		return nil, fmt.Errorf("%s is not yours to report on any more", id)
	}
	return d, nil
}

// CancelDispatch withdraws an item that has not been taken yet.
//
// Only `queued`. Once a room has claimed one, this hub has no way to reach in
// and stop it: the whole design is that the hub never dials a room. Cancelling
// a claimed item here would leave a row saying cancelled while a session starts
// on another machine, which is worse than saying no.
func (s *Store) CancelDispatch(id string) error {
	// Refused out here, not from inside the closure. See `QueueDispatch`.
	took := false
	err := s.guard(func() error {
		took = false
		res, err := s.db.Exec(
			`UPDATE dispatch SET state = ?, settled_at = ?, token = '' WHERE id = ? AND state = ?`,
			DispatchCancelled, ts(now()), id, DispatchQueued)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		took = n > 0
		return nil
	})
	if err != nil {
		return err
	}
	if !took {
		return errors.New("that item has already been handed to its room. " +
			"stop it on that machine, not from here")
	}
	return nil
}

// ExpireDispatchClaims takes back items whose room went quiet, and gives up on
// the ones that have already had their retry.
//
// Returns how many rows moved, so the caller can decide whether to say
// anything.
func (s *Store) ExpireDispatchClaims(lease time.Duration) (int, error) {
	if lease <= 0 {
		lease = DispatchLease
	}
	cutoff := ts(now().Add(-lease))
	moved := 0
	err := s.guard(func() error {
		moved = 0
		// Back to the queue, once. A new token is minted on the next claim, so
		// the previous holder cannot report on it any more.
		res, err := s.db.Exec(
			`UPDATE dispatch SET state = ?, token = '', claimed_at = ''
				WHERE state = ? AND claimed_at != '' AND claimed_at < ? AND attempts < ?`,
			DispatchQueued, DispatchClaimed, cutoff, MaxDispatchAttempts)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		moved += int(n)

		res, err = s.db.Exec(
			`UPDATE dispatch SET state = ?, token = '', settled_at = ?, error = ?
				WHERE state = ? AND claimed_at != '' AND claimed_at < ? AND attempts >= ?`,
			DispatchFailed, ts(now()),
			"handed over twice and never heard back. that room took it and said nothing",
			DispatchClaimed, cutoff, MaxDispatchAttempts)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		moved += int(n)
		return nil
	})
	return moved, err
}

// SweepDispatch deletes settled items older than an age.
//
// Only settled ones. An open item is a promise and ages out through the lease,
// never through here.
func (s *Store) SweepDispatch(age time.Duration) (int, error) {
	if age <= 0 {
		return 0, nil
	}
	n := 0
	err := s.guard(func() error {
		n = 0
		res, err := s.db.Exec(
			`DELETE FROM dispatch WHERE state NOT IN (?, ?) AND settled_at != '' AND settled_at < ?`,
			DispatchQueued, DispatchClaimed, ts(now().Add(-age)))
		if err != nil {
			return err
		}
		got, _ := res.RowsAffected()
		n = int(got)
		return nil
	})
	return n, err
}
