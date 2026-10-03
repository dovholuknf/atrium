package hubstore

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Growlers: something waiting on a human, one row the laptop, the desktop and
// the phone all read. See docs/rnd/persistent-growler-design.md.
//
// ── who decides what ────────────────────────────────────
//
// internal/link decides WHAT waits (which card, which reason, which identity)
// and when a reminder is due. This package keeps the rows and enforces the two
// rules that have to hold whoever calls it:
//
//   - A REASON THAT ENDS ENDS THE ROW, WHATEVER A HUMAN SAID. Dismissed and
//     acted become `resolved` too, so a dismissal can only be undone while its
//     reason stands, and undoing one never brings back a growler for something
//     already answered. `ended_at` is when, and it is what lets the next
//     waiting spell on the same card be a new row.
//   - A CARD MISSING FROM AN ANNOUNCEMENT IS NOT A REASON ENDED. Only a card
//     the room still announces, with no reason any more, ends its growler. This
//     is notify_sent's rule: a room offline, restarting or a card shelved all
//     take a card out of the cache and put it back.
//
// ── pruning ─────────────────────────────────────────────
//
// A row goes seven days after its reason ended, when a human is no longer
// looking at it. A row whose reason never ended is kept while its card is
// still cached, because deleting it would raise a dismissed growler again on
// the next announcement. A card gone from the cache for seven days, or a room
// removed, takes its rows with it.

// Growl reasons, as the CHECK constraint spells them.
const (
	GrowlPermission = "permission"
	GrowlHalt       = "halt"
	GrowlBlocked    = "blocked"
	GrowlQuestion   = "question"
	GrowlDeployHold = "deploy-hold"
)

// Growl states.
const (
	GrowlOpen      = "open"
	GrowlSnoozed   = "snoozed"
	GrowlDismissed = "dismissed"
	GrowlActed     = "acted"
	GrowlResolved  = "resolved"
)

// growlKeep is how long a row lasts once its reason has ended.
const growlKeep = 7 * 24 * time.Hour

// ErrGrowlNotFound is an id the hub has never held, or has pruned.
var ErrGrowlNotFound = errors.New("no growler has that id")

// ErrGrowlStale is an action on a growler that is no longer open or snoozed.
// The row comes back with it, so the screen can say what happened instead.
var ErrGrowlStale = errors.New("that growler is already handled")

// Growl is one row.
type Growl struct {
	ID string
	// RoomName is joined from room, and empty when that room has been removed.
	RoomID, RoomName string
	CardID           string
	Reason           string
	Title, Body      string
	Subject          string
	RaisedAt         time.Time
	State            string
	Until            *time.Time
	Reminders        int
	ChangedAt        time.Time
	ChangedVia       string
	ChangedTab       string
	EndedAt          *time.Time
}

// Live reports whether the growler is still one a screen shows.
func (g Growl) Live() bool { return g.State == GrowlOpen || g.State == GrowlSnoozed }

const growlCols = `g.id, g.room_id, COALESCE(r.name, ''), g.card_id, g.reason, g.title, g.body, g.subject,
	g.raised_at, g.state, g.until, g.reminders, g.changed_at, g.changed_via, g.changed_tab, g.ended_at`

const growlFrom = ` FROM growl g LEFT JOIN room r ON r.id = g.room_id`

func scanGrowl(sc scanner) (Growl, error) {
	var g Growl
	var raised, until, changed, ended string
	err := sc.Scan(&g.ID, &g.RoomID, &g.RoomName, &g.CardID, &g.Reason, &g.Title, &g.Body, &g.Subject,
		&raised, &g.State, &until, &g.Reminders, &changed, &g.ChangedVia, &g.ChangedTab, &ended)
	if err != nil {
		return g, err
	}
	if p := parseOrNil(raised); p != nil {
		g.RaisedAt = *p
	}
	if p := parseOrNil(changed); p != nil {
		g.ChangedAt = *p
	}
	g.Until, g.EndedAt = parseOrNil(until), parseOrNil(ended)
	return g, nil
}

func queryGrowls(q interface {
	Query(string, ...any) (*sql.Rows, error)
}, where string, args ...any) ([]Growl, error) {
	rows, err := q.Query(`SELECT `+growlCols+growlFrom+` WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Growl
	for rows.Next() {
		g, err := scanGrowl(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// endGrowl marks a growler's reason over. Reports whether a screen would
// change, which is only when it was open or snoozed.
func endGrowl(t *sql.Tx, g Growl, at time.Time) (bool, error) {
	_, err := t.Exec(`UPDATE growl SET ended_at = ?, state = 'resolved', until = '',
		changed_at = ?, changed_via = 'hub', changed_tab = '' WHERE id = ?`, ts(at), ts(at), g.ID)
	return g.Live(), err
}

// GrowlSync applies what a room's cards want right now, for the card reasons
// in `reasons`. `want` is every growler those cards should have, keyed by id.
// `present` is every card id the room announced. It returns the ids newly
// raised, and whether the live set changed at all.
//
// An existing row keeps its state. Its title is refreshed, and its body and
// subject only when `want` carries one, because a permission's are filled in
// afterwards from the room (GrowlFill) and an announcement does not know them.
func (s *Store) GrowlSync(roomID string, reasons []string, want []Growl, present map[string]bool) (
	raised []string, changed bool, err error) {
	if len(reasons) == 0 {
		return nil, false, nil
	}
	at := now()
	err = s.tx(func(t *sql.Tx) error {
		raised, changed = nil, false
		marks := strings.TrimSuffix(strings.Repeat("?,", len(reasons)), ",")
		args := []any{roomID}
		for _, r := range reasons {
			args = append(args, r)
		}
		had, err := queryGrowls(t, `g.room_id = ? AND g.ended_at = '' AND g.reason IN (`+marks+`)`, args...)
		if err != nil {
			return err
		}
		byID := map[string]Growl{}
		for _, g := range had {
			byID[g.ID] = g
		}
		wanted := map[string]bool{}
		for _, w := range want {
			wanted[w.ID] = true
			if old, ok := byID[w.ID]; ok {
				body, subject := old.Body, old.Subject
				if w.Body != "" {
					body = w.Body
				}
				if w.Subject != "" {
					subject = w.Subject
				}
				if old.Title == w.Title && old.Body == body && old.Subject == subject {
					continue
				}
				if _, err := t.Exec(`UPDATE growl SET title = ?, body = ?, subject = ? WHERE id = ?`,
					w.Title, body, subject, w.ID); err != nil {
					return err
				}
				changed = changed || old.Live()
				continue
			}
			// ON CONFLICT DO NOTHING: an identity whose reason already ended once
			// is that same spell, and it does not come back.
			res, err := t.Exec(`INSERT INTO growl (id, room_id, card_id, reason, title, body, subject,
				raised_at, state, changed_at, changed_via) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'open', ?, 'hub')
				ON CONFLICT (id) DO NOTHING`,
				w.ID, roomID, w.CardID, w.Reason, w.Title, w.Body, w.Subject, ts(at), ts(at))
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 1 {
				raised = append(raised, w.ID)
				changed = true
			}
		}
		for _, g := range had {
			if wanted[g.ID] || !present[g.CardID] {
				continue
			}
			live, err := endGrowl(t, g, at)
			if err != nil {
				return err
			}
			changed = changed || live
		}
		return nil
	})
	return raised, changed, err
}

// GrowlRoom is a growler about a room rather than a card, `halt` or
// `deploy-hold`, from the room's own answer. On raises g unless the room
// already has a current one of that reason, which then keeps its id and takes
// g's title, body and subject. Off ends the current one. A room that did not
// answer is never passed here: silence is neither.
func (s *Store) GrowlRoom(roomID, reason string, on bool, g Growl) (raised, changed bool, err error) {
	at := now()
	err = s.tx(func(t *sql.Tx) error {
		raised, changed = false, false
		had, err := queryGrowls(t, `g.room_id = ? AND g.reason = ? AND g.ended_at = ''`, roomID, reason)
		if err != nil {
			return err
		}
		if !on {
			for _, h := range had {
				live, err := endGrowl(t, h, at)
				if err != nil {
					return err
				}
				changed = changed || live
			}
			return nil
		}
		if len(had) > 0 {
			h := had[0]
			if h.Title == g.Title && h.Body == g.Body && h.Subject == g.Subject {
				return nil
			}
			if _, err := t.Exec(`UPDATE growl SET title = ?, body = ?, subject = ? WHERE id = ?`,
				g.Title, g.Body, g.Subject, h.ID); err != nil {
				return err
			}
			changed = h.Live()
			return nil
		}
		res, err := t.Exec(`INSERT INTO growl (id, room_id, reason, title, body, subject, raised_at, state,
			changed_at, changed_via) VALUES (?, ?, ?, ?, ?, ?, ?, 'open', ?, 'hub') ON CONFLICT (id) DO NOTHING`,
			g.ID, roomID, reason, g.Title, g.Body, g.Subject, ts(at), ts(at))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			raised, changed = true, true
		}
		return nil
	})
	return raised, changed, err
}

// GrowlRaise is a question that is about neither a card nor a room's health: one thing waiting on the operator,
// under an id the caller chose (a change request's). It has no card, so a room's card sync never touches it, and
// it ends only when GrowlEnd says its reason did, or a human handles it. An id that was ever raised is not raised
// again, ended or not, so a request that is finished does not come back as a growler.
func (s *Store) GrowlRaise(roomID string, g Growl) (raised bool, err error) {
	at := now()
	err = s.tx(func(t *sql.Tx) error {
		raised = false
		res, err := t.Exec(`INSERT INTO growl (id, room_id, reason, title, body, subject, raised_at, state,
			changed_at, changed_via) VALUES (?, ?, 'question', ?, ?, ?, ?, 'open', ?, 'hub') ON CONFLICT (id) DO NOTHING`,
			g.ID, roomID, g.Title, g.Body, g.Subject, ts(at), ts(at))
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		raised = n == 1
		return nil
	})
	return raised, err
}

// GrowlEnd ends a growler's reason by id, whatever a human did with it. Reports whether a screen would change. An
// id the hub never held, or one already ended, is not an error.
func (s *Store) GrowlEnd(id string) (changed bool, err error) {
	at := now()
	err = s.tx(func(t *sql.Tx) error {
		changed = false
		had, err := queryGrowls(t, `g.id = ? AND g.ended_at = ''`, id)
		if err != nil {
			return err
		}
		for _, h := range had {
			live, err := endGrowl(t, h, at)
			if err != nil {
				return err
			}
			changed = changed || live
		}
		return nil
	})
	return changed, err
}

// GrowlFill sets what only the room could say about a growler, its subject
// and body, while its reason has not ended. Reports whether a live row moved.
func (s *Store) GrowlFill(id, subject, body string) (bool, error) {
	var n int64
	err := s.guard(func() error {
		res, err := s.db.Exec(`UPDATE growl SET subject = ?, body = ?
			WHERE id = ? AND ended_at = '' AND state IN ('open','snoozed') AND (subject != ? OR body != ?)`,
			subject, body, id, subject, body)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	return n > 0, err
}

// GrowlLive is every open and snoozed growler.
func (s *Store) GrowlLive() ([]Growl, error) {
	var out []Growl
	err := s.guard(func() error {
		var err error
		out, err = queryGrowls(s.db, `g.state IN ('open','snoozed') ORDER BY g.raised_at, g.id`)
		return err
	})
	return out, err
}

// GrowlGet is one row, live or not.
func (s *Store) GrowlGet(id string) (Growl, error) {
	var g Growl
	err := s.guard(func() error {
		var err error
		g, err = scanGrowl(s.db.QueryRow(`SELECT `+growlCols+growlFrom+` WHERE g.id = ?`, id))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrGrowlNotFound
	}
	return g, err
}

// GrowlAct is a human's answer: dismiss, snooze until a time, or acted, each
// taken only by an open or snoozed growler. Or GrowlOpen, which is undoing a
// dismissal, taken only by a dismissed one: the same growler back, its
// raised_at and reminders as they were. Anything else answers ErrGrowlStale
// with the row as it is, which is how a click on an old screen learns another
// screen, or the reason ending, got there first.
func (s *Store) GrowlAct(id, state string, until time.Time, via, tab string) (Growl, error) {
	switch state {
	case GrowlDismissed, GrowlSnoozed, GrowlActed, GrowlOpen:
	default:
		return Growl{}, errors.New("a growler can be dismissed, snoozed, acted on or undismissed")
	}
	var g Growl
	at := now()
	err := s.tx(func(t *sql.Tx) error {
		var err error
		g, err = scanGrowl(t.QueryRow(`SELECT `+growlCols+growlFrom+` WHERE g.id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return refuse(ErrGrowlNotFound)
		}
		if err != nil {
			return err
		}
		takes := g.Live()
		if state == GrowlOpen {
			takes = g.State == GrowlDismissed
		}
		if !takes || g.EndedAt != nil {
			return refuse(ErrGrowlStale)
		}
		u := ""
		if state == GrowlSnoozed {
			u = ts(until)
		}
		if _, err := t.Exec(`UPDATE growl SET state = ?, until = ?, changed_at = ?, changed_via = ?,
			changed_tab = ? WHERE id = ?`, state, u, ts(at), via, tab, id); err != nil {
			return err
		}
		g, err = scanGrowl(t.QueryRow(`SELECT `+growlCols+growlFrom+` WHERE g.id = ?`, id))
		return err
	})
	switch {
	case errors.Is(err, ErrGrowlNotFound):
		return Growl{}, ErrGrowlNotFound
	case errors.Is(err, ErrGrowlStale):
		return g, ErrGrowlStale
	}
	return g, err
}

// GrowlWake ends every snooze that is due. A woken growler comes back as if
// new: open, raised now, no reminders yet. It returns the ids woken.
func (s *Store) GrowlWake() ([]string, error) {
	at := now()
	var woke []string
	err := s.tx(func(t *sql.Tx) error {
		woke = nil
		due, err := queryGrowls(t, `g.state = 'snoozed' AND g.until != '' AND g.until <= ?`, ts(at))
		if err != nil {
			return err
		}
		for _, g := range due {
			if _, err := t.Exec(`UPDATE growl SET state = 'open', until = '', raised_at = ?, reminders = 0,
				changed_at = ?, changed_via = 'hub', changed_tab = '' WHERE id = ?`, ts(at), ts(at), g.ID); err != nil {
				return err
			}
			woke = append(woke, g.ID)
		}
		return nil
	})
	return woke, err
}

// GrowlReminded records how many reminders a growler has had.
func (s *Store) GrowlReminded(id string, n int) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE growl SET reminders = ? WHERE id = ?`, n, id)
		return err
	})
}

// GrowlPrune drops what nobody will look at again. See the header.
func (s *Store) GrowlPrune() (int64, error) {
	var n int64
	cut := ts(now().Add(-growlKeep))
	err := s.guard(func() error {
		res, err := s.db.Exec(`DELETE FROM growl WHERE
			(ended_at != '' AND ended_at < ? AND state NOT IN ('open','snoozed'))
			OR (card_id != '' AND changed_at < ? AND NOT EXISTS (
				SELECT 1 FROM room_card c WHERE c.room_id = growl.room_id AND c.card_id = growl.card_id))
			OR NOT EXISTS (SELECT 1 FROM room r WHERE r.id = growl.room_id)`, cut, cut)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	return n, err
}
