package hubstore

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

// The hub store's push log, behind gitsync.PushLog. See migration 0008 for the rules of the table, and
// internal/gitsync/pushlog.go for the interface.

var _ gitsync.PushLog = (*Store)(nil)

func pushTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parsePushTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

type pushTx interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

// idSource hands out the ids of one transaction, in order. EACH ID IS LARGER THAN EVERY ID IN THE TABLE: the
// clock is a way to make them look like times, and has no say when it is behind.
type idSource struct{ last int64 }

func (s *Store) ids(ctx context.Context, x pushTx) (*idSource, error) {
	var top string
	if err := x.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), '') FROM git_push`).Scan(&top); err != nil {
		return nil, err
	}
	var last int64
	if top != "" {
		n, err := strconv.ParseInt(top, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("git_push has an id that is not a number: %q", top)
		}
		last = n
	}
	return &idSource{last: last}, nil
}

func (s *Store) pushNow() time.Time {
	if s.gitPushClock != nil {
		return s.gitPushClock()
	}
	return time.Now()
}

func (s *Store) nextID(src *idSource) string {
	n := s.pushNow().UnixNano()
	if n <= src.last {
		n = src.last + 1
	}
	src.last = n
	return fmt.Sprintf("%020d", n)
}

func (s *Store) insertPush(ctx context.Context, x pushTx, src *idSource, r gitsync.PushRow, state, batch string) (string, error) {
	at := r.At
	if at.IsZero() {
		at = time.Now()
	}
	kind := "push"
	if r.Release {
		kind = "release"
	}
	id := s.nextID(src)
	_, err := x.ExecContext(ctx,
		`INSERT INTO git_push (id, kind, state, batch, repo, ref, old_sha, new_sha, room, card, at, released_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, kind, state, batch, r.Repo, r.Ref, r.Old, r.New, r.Room, r.Card, pushTime(at), r.ReleasedBy)
	return id, err
}

// Append writes settled rows in one transaction: all of them or none. The hub does not use it, it begins and
// settles. It is for putting a log in a known state in a test.
func (s *Store) Append(ctx context.Context, rows ...gitsync.PushRow) error {
	if len(rows) == 0 {
		return nil
	}
	return s.guard(func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		src, err := s.ids(ctx, tx)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if _, err := s.insertPush(ctx, tx, src, r, "done", ""); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
}

// Begin writes a push as pending, before git runs. All rows or none.
func (s *Store) Begin(ctx context.Context, rows ...gitsync.PushRow) (string, error) {
	if len(rows) == 0 {
		return "", nil
	}
	var batch string
	err := s.guard(func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		src, err := s.ids(ctx, tx)
		if err != nil {
			return err
		}
		// The batch is named by the first id, which no other batch has.
		batch = "b" + fmt.Sprintf("%020d", src.last+1)
		for _, r := range rows {
			if _, err := s.insertPush(ctx, tx, src, r, "pending", batch); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
	if err != nil {
		return "", err
	}
	return batch, nil
}

// Settle ends a batch: the pending row of each landed ref becomes a push, after the release marker of the owner
// it took the branch from, and the other pending rows of the batch are dropped. One transaction.
func (s *Store) Settle(ctx context.Context, batch string, landed ...string) error {
	did := map[string]bool{}
	for _, ref := range landed {
		did[ref] = true
	}
	return s.guard(func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		type pend struct {
			id, repo, ref, by string
		}
		rows, err := tx.QueryContext(ctx,
			`SELECT id, repo, ref, released_by FROM git_push WHERE batch = ? AND state = 'pending' ORDER BY id`, batch)
		if err != nil {
			return err
		}
		var mine []pend
		for rows.Next() {
			var p pend
			if err := rows.Scan(&p.id, &p.repo, &p.ref, &p.by); err != nil {
				rows.Close()
				return err
			}
			mine = append(mine, p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		src, err := s.ids(ctx, tx)
		if err != nil {
			return err
		}
		for _, p := range mine {
			if !did[p.ref] {
				if _, err := tx.ExecContext(ctx, `DELETE FROM git_push WHERE id = ?`, p.id); err != nil {
					return err
				}
				continue
			}
			if p.by != "" {
				room, card, ok, err := ownerOf(ctx, tx, p.repo, p.ref)
				if err != nil {
					return err
				}
				if ok {
					if _, err := s.insertPush(ctx, tx, src, gitsync.PushRow{Repo: p.repo, Ref: p.ref, Room: room, Card: card,
						ReleasedBy: p.by, Release: true}, "done", ""); err != nil {
						return err
					}
				}
			}
			// The push row takes a new id, after the marker, which is what puts it first after the release.
			if _, err := tx.ExecContext(ctx, `UPDATE git_push SET id = ?, state = 'done', batch = '' WHERE id = ?`,
				s.nextID(src), p.id); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
}

// Pending is the rows still pending, in one repository or all of them.
func (s *Store) Pending(ctx context.Context, repo string) ([]gitsync.PushRow, error) {
	var out []gitsync.PushRow
	err := s.guard(func() error {
		out = nil
		q := `SELECT batch, repo, ref, old_sha, new_sha, room, card, at, released_by FROM git_push
		      WHERE state = 'pending'`
		var args []any
		if repo != "" {
			q += ` AND repo = ?`
			args = append(args, repo)
		}
		rows, err := s.db.QueryContext(ctx, q+` ORDER BY id`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r gitsync.PushRow
			var at string
			if err := rows.Scan(&r.Batch, &r.Repo, &r.Ref, &r.Old, &r.New, &r.Room, &r.Card, &at, &r.ReleasedBy); err != nil {
				return err
			}
			r.At, r.Pending = parsePushTime(at), true
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// Owner is the first pusher of ref since it was last released, a pending push counted.
func (s *Store) Owner(ctx context.Context, repo, ref string) (room, card string, ok bool, err error) {
	err = s.guard(func() error {
		var e error
		room, card, ok, e = ownerOf(ctx, s.db, repo, ref)
		return e
	})
	return room, card, ok, err
}

// ownerOf is the first push row, a pending one counted, with no release marker after it.
func ownerOf(ctx context.Context, q pushTx, repo, ref string) (room, card string, ok bool, err error) {
	err = q.QueryRowContext(ctx,
		`SELECT p.room, p.card FROM git_push p
		 WHERE p.repo = ? AND p.ref = ? AND p.kind = 'push'
		   AND p.id > COALESCE((SELECT MAX(r.id) FROM git_push r
		                        WHERE r.repo = p.repo AND r.ref = p.ref AND r.kind = 'release'), '')
		 ORDER BY p.id LIMIT 1`, repo, ref).Scan(&room, &card)
	if err == sql.ErrNoRows {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return room, card, true, nil
}

// Release lets a branch go: a marker row naming the owner that was released. A branch with no owner has
// nothing to release and gets no row.
func (s *Store) Release(ctx context.Context, repo, ref, by string) error {
	return s.guard(func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		room, card, ok, err := ownerOf(ctx, tx, repo, ref)
		if err != nil || !ok {
			return err
		}
		src, err := s.ids(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := s.insertPush(ctx, tx, src, gitsync.PushRow{Repo: repo, Ref: ref, Room: room, Card: card,
			ReleasedBy: by, Release: true}, "done", ""); err != nil {
			return err
		}
		return tx.Commit()
	})
}

// Branches is each branch of the repository the log has rows for, with its owner and latest push.
func (s *Store) Branches(ctx context.Context, repo string) ([]gitsync.BranchRecord, error) {
	var out []gitsync.BranchRecord
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.QueryContext(ctx,
			`SELECT kind, ref, room, card, at FROM git_push
			 WHERE repo = ? AND ref LIKE 'refs/heads/%' ORDER BY ref, id`, repo)
		if err != nil {
			return err
		}
		defer rows.Close()
		var cur *gitsync.BranchRecord
		hasOwner := false
		for rows.Next() {
			var kind, ref, room, card, at string
			if err := rows.Scan(&kind, &ref, &room, &card, &at); err != nil {
				return err
			}
			if cur == nil || cur.Ref != ref {
				out = append(out, gitsync.BranchRecord{Ref: ref})
				cur = &out[len(out)-1]
				hasOwner = false
			}
			if kind == "release" {
				cur.Released, hasOwner = true, false
				continue
			}
			if !hasOwner {
				cur.Room, cur.Card, hasOwner = room, card, true
			}
			cur.Released, cur.At = false, parsePushTime(at)
		}
		return rows.Err()
	})
	return out, err
}

// LastOperatorPush is the time of the operator's latest settled push to ref.
func (s *Store) LastOperatorPush(ctx context.Context, repo, ref string) (time.Time, bool, error) {
	var at time.Time
	found := false
	err := s.guard(func() error {
		var text string
		err := s.db.QueryRowContext(ctx,
			`SELECT at FROM git_push WHERE repo = ? AND ref = ? AND kind = 'push' AND state = 'done'
			   AND room = '' AND card = ''
			 ORDER BY id DESC LIMIT 1`, repo, ref).Scan(&text)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		at, found = parsePushTime(text), true
		return nil
	})
	return at, found, err
}
