package store

import (
	"context"
	"database/sql"
)

// One change, one transaction. See docs/work-ledger-design.md, "Transactions".
//
// `guard` retries contention and halts on anything else, and it is not a
// transaction: a method that runs three statements through it commits each one
// on its own. That was enough while every write stood alone. The work ledger
// needs an exit, the item it ends and the notice it queues to commit together
// or not at all, because a crash between them is exactly the case the ledger
// exists for.

// querier is what a statement runs on: the database for a write that stands
// alone, or a Tx for one that is part of a change. `*sql.DB` satisfies it
// already, so a helper written against it serves both.
type querier interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// Tx is one `BEGIN IMMEDIATE` transaction on the store's only connection.
//
// NOTHING INSIDE ONE MAY CALL `s.db`. The pool holds one connection and the
// transaction has it, so a stray `s.db` call waits for a connection that is
// only released when the function it is inside returns.
type Tx struct {
	c   *sql.Conn
	ctx context.Context
	// after runs once the transaction has committed: cold sink fan-out, and
	// telling the daemon what the ledger did. Never on a rollback.
	after []func()
}

func (t *Tx) Exec(query string, args ...any) (sql.Result, error) {
	return t.c.ExecContext(t.ctx, query, args...)
}

func (t *Tx) Query(query string, args ...any) (*sql.Rows, error) {
	return t.c.QueryContext(t.ctx, query, args...)
}

func (t *Tx) QueryRow(query string, args ...any) *sql.Row {
	return t.c.QueryRowContext(t.ctx, query, args...)
}

// afterCommit queues work for once the change is durable.
func (t *Tx) afterCommit(fn func()) { t.after = append(t.after, fn) }

// inTx runs fn inside one immediate transaction, under guard.
//
// Contention anywhere, the BEGIN included, rolls back and runs fn again from
// the start, so fn must not carry state from one attempt to the next. Any
// other error rolls back and halts, the same posture as every other write.
//
// A refusal is not an error. A caller that decides not to write returns nil
// and reports why through its own variables, so a stale revision or a wrong
// caller never reads to guard as a broken database.
func (s *Store) inTx(fn func(tx *Tx) error) error {
	var after []func()
	err := s.guard(func() error {
		after = nil
		ctx := context.Background()
		c, err := s.db.Conn(ctx)
		if err != nil {
			return err
		}
		defer c.Close()
		if _, err := c.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
			return err
		}
		tx := &Tx{c: c, ctx: ctx}
		if err := fn(tx); err != nil {
			_, _ = c.ExecContext(ctx, "ROLLBACK")
			return err
		}
		if _, err := c.ExecContext(ctx, "COMMIT"); err != nil {
			_, _ = c.ExecContext(ctx, "ROLLBACK")
			return err
		}
		after = tx.after
		return nil
	})
	if err != nil {
		return err
	}
	for _, fn := range after {
		fn()
	}
	return nil
}
