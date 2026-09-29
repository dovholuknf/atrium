package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// The cache keep-alive's durable half: each card's switch, the room's default
// and suspension, and the ledger of every refresh. See
// docs/cache-keepalive-design.md. The daemon decides when to refresh. This file
// only keeps the rows.

// SettingKeepaliveDefault is whether a NEW Claude card starts with keep-alive
// on. `off` turns it off. Anything else, unset included, is on. It is read at
// launch and never applied to a card that already exists.
const SettingKeepaliveDefault = "cache_keepalive_default"

// SettingKeepaliveSuspended holds why keep-alive is suspended for the whole
// room, or nothing. Suspended means no card is refreshed, whatever its switch
// says, until a person clears it. Set by the daemon when the mechanism itself
// looks wrong: two misses on two cards, or a fork that acted.
const SettingKeepaliveSuspended = "cache_keepalive_suspended"

// Keep-alive card states. The `stopped:` ones are set by the daemon. The first
// three clear on the card's next real turn, and `stopped:acted` only by hand.
const (
	KeepaliveOn        = "on"
	KeepaliveOff       = "off"
	KeepaliveBreakEven = "stopped:break-even"
	KeepaliveMiss      = "stopped:miss"
	KeepaliveFailing   = "stopped:failing"
	KeepaliveActed     = "stopped:acted"
)

// KeepaliveStates lists every value a card's state may hold.
var KeepaliveStates = []string{
	KeepaliveOn, KeepaliveOff, KeepaliveBreakEven, KeepaliveMiss, KeepaliveFailing, KeepaliveActed,
}

// ErrKeepaliveState is a state that is not one of KeepaliveStates.
var ErrKeepaliveState = errors.New("keep-alive state must be on, off or one of the stopped reasons")

// KeepaliveCard is one card's switch.
type KeepaliveCard struct {
	TaskID  string    `json:"task_id"`
	State   string    `json:"state"`
	StateAt time.Time `json:"state_at"`
}

// KeepaliveRefresh is one ledger row: an attempt, and what it cost.
type KeepaliveRefresh struct {
	ID          string    `json:"id"`
	TaskID      string    `json:"task_id"`
	ResumeID    string    `json:"resume_id"`
	ForkSession string    `json:"fork_session,omitempty"`
	At          time.Time `json:"at"`
	Model       string    `json:"model"`
	Outcome     string    `json:"outcome"`
	Context     int64     `json:"context"`
	TTLLeftS    int64     `json:"ttl_left_s"`
	SpentBefore float64   `json:"spent_before"`
	Budget      float64   `json:"budget"`
	CacheRead   int64     `json:"cache_read"`
	CacheWrite  int64     `json:"cache_write"`
	Input       int64     `json:"input"`
	Output      int64     `json:"output"`
	Cost        float64   `json:"-"`
	Prices      string    `json:"prices"`
}

// KeepaliveDefaultOn reports whether a new Claude card starts with keep-alive
// on. A read failure answers on, the default clint asked for.
func (s *Store) KeepaliveDefaultOn() bool {
	v, err := s.Setting(SettingKeepaliveDefault)
	if err != nil {
		return true
	}
	return !strings.EqualFold(strings.TrimSpace(v), "off")
}

// SetKeepaliveDefault writes the default for new cards.
func (s *Store) SetKeepaliveDefault(on bool) error {
	v := "off"
	if on {
		v = "on"
	}
	return s.SetSetting(SettingKeepaliveDefault, v)
}

// KeepaliveSuspended returns why keep-alive is suspended, or "" when it is not.
// A read failure answers suspended: refusing to spend is the safe way to be
// wrong about this.
func (s *Store) KeepaliveSuspended() string {
	v, err := s.Setting(SettingKeepaliveSuspended)
	if err != nil {
		return "the setting could not be read"
	}
	return strings.TrimSpace(v)
}

// SetKeepaliveSuspended suspends the room with a reason, or clears the
// suspension with "".
func (s *Store) SetKeepaliveSuspended(reason string) error {
	return s.SetSetting(SettingKeepaliveSuspended, strings.TrimSpace(reason))
}

// KeepaliveCardOf returns a card's switch, or nil when it has none.
func (s *Store) KeepaliveCardOf(taskID string) (*KeepaliveCard, error) {
	var c *KeepaliveCard
	err := s.guard(func() error {
		c = nil
		got, err := scanKeepaliveCard(s.db.QueryRow(
			`SELECT task_id, state, state_at FROM keepalive_card WHERE task_id = ?`, taskID))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		c = got
		return err
	})
	return c, err
}

// KeepaliveCards lists every card that has a switch.
func (s *Store) KeepaliveCards() ([]*KeepaliveCard, error) {
	var out []*KeepaliveCard
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT task_id, state, state_at FROM keepalive_card ORDER BY task_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanKeepaliveCard(rows)
			if err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

// SetKeepaliveState writes a card's switch and stamps it now. A card that does
// not exist is not given a row.
func (s *Store) SetKeepaliveState(taskID, state string) (*KeepaliveCard, error) {
	return s.SetKeepaliveStateAt(taskID, state, now())
}

// SetKeepaliveStateAt is SetKeepaliveState on the caller's clock. The daemon's
// loop compares the stamp with transcript times and ledger rows it stamped
// itself, so it stamps this with the same clock.
func (s *Store) SetKeepaliveStateAt(taskID, state string, at time.Time) (*KeepaliveCard, error) {
	known := false
	for _, k := range KeepaliveStates {
		if state == k {
			known = true
		}
	}
	if !known {
		return nil, ErrKeepaliveState
	}
	c := &KeepaliveCard{TaskID: taskID, State: state, StateAt: at.UTC().Truncate(time.Millisecond)}
	err := s.guard(func() error {
		res, err := s.db.Exec(`INSERT INTO keepalive_card (task_id, state, state_at)
			SELECT ?, ?, ? WHERE EXISTS (SELECT 1 FROM task WHERE id = ?)
			ON CONFLICT (task_id) DO UPDATE SET state = excluded.state, state_at = excluded.state_at`,
			taskID, state, ts(c.StateAt), taskID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// AddKeepaliveRefresh writes one ledger row. The id and time are filled in when
// empty.
func (s *Store) AddKeepaliveRefresh(r *KeepaliveRefresh) error {
	if r.ID == "" {
		r.ID = newID()
	}
	if r.At.IsZero() {
		r.At = now()
	}
	return s.guard(func() error {
		_, err := s.db.Exec(`INSERT INTO keepalive_refresh (id, task_id, resume_id, fork_session, at, model,
			outcome, context, ttl_left_s, spent_before, budget, cache_read, cache_write, input, output, cost, prices)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.TaskID, r.ResumeID, r.ForkSession, ts(r.At), r.Model, r.Outcome, r.Context, r.TTLLeftS,
			r.SpentBefore, r.Budget, r.CacheRead, r.CacheWrite, r.Input, r.Output, r.Cost, r.Prices)
		return err
	})
}

// KeepaliveRefreshesSince lists a card's ledger rows after a moment, oldest
// first. The daemon reads the current idle stretch with it: the budget spent,
// the refresh count, and the last warmed refresh.
func (s *Store) KeepaliveRefreshesSince(taskID string, since time.Time) ([]*KeepaliveRefresh, error) {
	var out []*KeepaliveRefresh
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT id, task_id, resume_id, fork_session, at, model, outcome, context,
			ttl_left_s, spent_before, budget, cache_read, cache_write, input, output, cost, prices
			FROM keepalive_refresh WHERE task_id = ? AND at > ? ORDER BY at, id`, taskID, ts(since))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				r  KeepaliveRefresh
				at string
			)
			if err := rows.Scan(&r.ID, &r.TaskID, &r.ResumeID, &r.ForkSession, &at, &r.Model, &r.Outcome,
				&r.Context, &r.TTLLeftS, &r.SpentBefore, &r.Budget, &r.CacheRead, &r.CacheWrite, &r.Input,
				&r.Output, &r.Cost, &r.Prices); err != nil {
				return err
			}
			if r.At, err = parseTS(at); err != nil {
				return err
			}
			out = append(out, &r)
		}
		return rows.Err()
	})
	return out, err
}

// KeepaliveSpendSince is the room's total refresh cost after a moment, and how
// many attempts reached the API. The settings cog shows the last week of it.
func (s *Store) KeepaliveSpendSince(since time.Time) (float64, int, error) {
	var (
		cost float64
		n    int
	)
	err := s.guard(func() error {
		return s.db.QueryRow(`SELECT COALESCE(SUM(cost), 0), COUNT(*) FROM keepalive_refresh
			WHERE at > ? AND cost > 0`, ts(since)).Scan(&cost, &n)
	})
	return cost, n, err
}

func scanKeepaliveCard(r rowScanner) (*KeepaliveCard, error) {
	var (
		c  KeepaliveCard
		at string
	)
	if err := r.Scan(&c.TaskID, &c.State, &at); err != nil {
		return nil, err
	}
	var err error
	if c.StateAt, err = parseTS(at); err != nil {
		return nil, err
	}
	return &c, nil
}
