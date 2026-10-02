package hubstore

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

// The hub store's push log, behind gitsync.PushLog. Rows are only ever added: the owner of a branch is the
// first push row after the latest release marker, and a release is a marker row. See migration 0008.

var _ gitsync.PushLog = (*Store)(nil)

// pushIDs hands out ids that sort in write order. An id is the time in nanoseconds, zero padded, and a clock
// that steps back or a second row in the same nanosecond still gets a larger one than the last.
var pushIDs struct {
	sync.Mutex
	last int64
}

func nextPushID(at time.Time) string {
	pushIDs.Lock()
	defer pushIDs.Unlock()
	n := at.UnixNano()
	if n <= pushIDs.last {
		n = pushIDs.last + 1
	}
	pushIDs.last = n
	return fmt.Sprintf("%020d", n)
}

func pushTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parsePushTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// Append writes the rows in one transaction: all of them or none.
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
		for _, r := range rows {
			if err := insertPush(ctx, tx, r); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
}

type pushExec interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

func insertPush(ctx context.Context, x pushExec, r gitsync.PushRow) error {
	at := r.At
	if at.IsZero() {
		at = time.Now()
	}
	kind := "push"
	if r.Release {
		kind = "release"
	}
	_, err := x.ExecContext(ctx,
		`INSERT INTO git_push (id, kind, repo, ref, old_sha, new_sha, room, card, at, released_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nextPushID(at), kind, r.Repo, r.Ref, r.Old, r.New, r.Room, r.Card, pushTime(at), r.ReleasedBy)
	return err
}

// Owner is the first pusher of ref since it was last released.
func (s *Store) Owner(ctx context.Context, repo, ref string) (room, card string, ok bool, err error) {
	err = s.guard(func() error {
		var e error
		room, card, ok, e = ownerOf(ctx, s.db, repo, ref)
		return e
	})
	return room, card, ok, err
}

type pushQuery interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

func ownerOf(ctx context.Context, q pushQuery, repo, ref string) (room, card string, ok bool, err error) {
	// The first push row with no release marker after it.
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
		if err := insertPush(ctx, tx, gitsync.PushRow{Repo: repo, Ref: ref, Room: room, Card: card,
			ReleasedBy: by, Release: true}); err != nil {
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

// LastOperatorPush is the time of the operator's latest push to ref.
func (s *Store) LastOperatorPush(ctx context.Context, repo, ref string) (time.Time, bool, error) {
	var at time.Time
	found := false
	err := s.guard(func() error {
		var text string
		err := s.db.QueryRowContext(ctx,
			`SELECT at FROM git_push WHERE repo = ? AND ref = ? AND kind = 'push' AND room = '' AND card = ''
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
