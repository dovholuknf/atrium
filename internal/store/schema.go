package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// migrations are applied in order and recorded by name. Everything here is
// written to run unchanged on Postgres: text ULID-ish keys instead of
// AUTOINCREMENT, RFC3339 text instead of a native timestamp type, CHECK
// constraints instead of enums, and TEXT instead of JSONB.
var migrations = []struct {
	name  string
	stmts []string
}{
	{
		name: "0001_initial",
		stmts: []string{
			`CREATE TABLE task (
				id               TEXT PRIMARY KEY,
				title            TEXT NOT NULL,
				why              TEXT NOT NULL DEFAULT '',
				repo             TEXT NOT NULL DEFAULT '',
				worktree         TEXT NOT NULL DEFAULT '',
				runner           TEXT NOT NULL,
				hostname         TEXT NOT NULL DEFAULT '',
				pid              INTEGER NOT NULL DEFAULT 0,
				status           TEXT NOT NULL
				                   CHECK (status IN ('backlog','running','needs-input',
				                                     'needs-permission','done','shelved','dead')),
				created_at       TEXT NOT NULL,
				last_activity_at TEXT NOT NULL,
				waiting_since    TEXT,
				wire_name        TEXT,
				overrides        TEXT NOT NULL DEFAULT '{}',
				rank             REAL NOT NULL,
				UNIQUE (wire_name)
			)`,
			`CREATE INDEX task_status_rank ON task (status, rank)`,
			`CREATE TABLE event (
				id      TEXT PRIMARY KEY,
				task_id TEXT NOT NULL REFERENCES task(id) ON DELETE CASCADE,
				at      TEXT NOT NULL,
				kind    TEXT NOT NULL
				          CHECK (kind IN ('created','submitted','prompted','perm-requested','perm-decided',
				                          'status-changed','output','notified','launched','exited')),
				payload TEXT NOT NULL DEFAULT '{}'
			)`,
			`CREATE INDEX event_task_at ON event (task_id, at)`,
			`CREATE TABLE permission (
				id           TEXT PRIMARY KEY,
				task_id      TEXT NOT NULL REFERENCES task(id) ON DELETE CASCADE,
				tool         TEXT NOT NULL,
				command      TEXT NOT NULL,
				requested_at TEXT NOT NULL,
				decided_at   TEXT,
				decision     TEXT CHECK (decision IS NULL OR decision IN ('approve','block')),
				reason       TEXT NOT NULL DEFAULT '',
				dedup_key    TEXT,
				UNIQUE (dedup_key)
			)`,
			`CREATE INDEX permission_pending ON permission (decided_at, requested_at)`,
			`CREATE TABLE perm_rule (
				id         TEXT PRIMARY KEY,
				tool       TEXT NOT NULL,
				prefix     TEXT NOT NULL,
				decision   TEXT NOT NULL CHECK (decision IN ('approve','block')),
				reason     TEXT NOT NULL DEFAULT '',
				scope      TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				hits       INTEGER NOT NULL DEFAULT 0,
				UNIQUE (tool, prefix, scope)
			)`,
			`CREATE TABLE launch_spec (
				task_id    TEXT PRIMARY KEY REFERENCES task(id) ON DELETE CASCADE,
				cmd        TEXT NOT NULL,
				args       TEXT NOT NULL DEFAULT '[]',
				cwd        TEXT NOT NULL,
				env        TEXT NOT NULL DEFAULT '{}',
				pid        INTEGER,
				started_at TEXT,
				exited_at  TEXT,
				exit_code  INTEGER
			)`,
		},
	},
	{
		// perm_rule also appears in 0001 for the benefit of fresh databases.
		// A database created before standing rules existed already has 0001
		// recorded, so it would never see that statement and would fail at
		// runtime the first time a rule was looked up. IF NOT EXISTS makes
		// this safe to run against both.
		name: "0002_perm_rule",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS perm_rule (
				id         TEXT PRIMARY KEY,
				tool       TEXT NOT NULL,
				prefix     TEXT NOT NULL,
				decision   TEXT NOT NULL CHECK (decision IN ('approve','block')),
				reason     TEXT NOT NULL DEFAULT '',
				scope      TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				hits       INTEGER NOT NULL DEFAULT 0,
				UNIQUE (tool, prefix, scope)
			)`,
		},
	},
	{
		// Lets the decision log say who answered: you, or the standing rule
		// that matched.
		name: "0003_permission_decided_by",
		stmts: []string{
			`ALTER TABLE permission ADD COLUMN decided_by TEXT NOT NULL DEFAULT ''`,
		},
	},
	{
		// Without this the audit log cannot tell "approve once" from "always":
		// both look like a hand decision, and the rule they created is
		// invisible at the point it was agreed to.
		name: "0004_permission_rule_created",
		stmts: []string{
			`ALTER TABLE permission ADD COLUMN rule_created TEXT NOT NULL DEFAULT ''`,
		},
	},
	{
		// Lets a card be tied to a session that atrium did not start: the gwt
		// ledger's own id, and the id claude needs to resume that conversation.
		name: "0005_external_session",
		stmts: []string{
			`ALTER TABLE task ADD COLUMN external_id TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE task ADD COLUMN resume_id TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE task ADD COLUMN branch TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE task ADD COLUMN window_name TEXT NOT NULL DEFAULT ''`,
			// The ADD COLUMNs above are tolerated when already present, so this
			// has to be too, or a partially applied migration can never finish.
			`CREATE INDEX IF NOT EXISTS task_external ON task (external_id)`,
		},
	},
	{
		// Runners are rows, not code. Adding claude, codex, ollama or anything
		// else is configuration.
		name: "0006_harness",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS harness (
				id           TEXT PRIMARY KEY,
				label        TEXT NOT NULL DEFAULT '',
				enabled      INTEGER NOT NULL DEFAULT 0,
				cmd          TEXT NOT NULL,
				args         TEXT NOT NULL DEFAULT '[]',
				cwd          TEXT NOT NULL DEFAULT '',
				env          TEXT NOT NULL DEFAULT '{}',
				launch_mode  TEXT NOT NULL DEFAULT 'window'
				               CHECK (launch_mode IN ('window','pty')),
				resume_args  TEXT NOT NULL DEFAULT '[]',
				rules_source TEXT NOT NULL DEFAULT '',
				notes        TEXT NOT NULL DEFAULT '',
				sort         INTEGER NOT NULL DEFAULT 100,
				created_at   TEXT NOT NULL
			)`,
			`ALTER TABLE task ADD COLUMN harness TEXT NOT NULL DEFAULT ''`,
		},
	},
	{
		// Adopting from the gwt session ledger is gone. It filled the board
		// with hundreds of sessions atrium could watch but never talk to, and
		// the hook already brings a session in the moment it does something.
		// The columns stay: resume_id is worth keeping for when a card can
		// relaunch its own runner.
		name: "0007_drop_adopted",
		stmts: []string{
			`DELETE FROM task WHERE external_id != '' AND (wire_name IS NULL OR wire_name = '')`,
		},
	},
	{
		// The seeded note still pointed at the gwt ledger, which no longer
		// feeds anything. A seeded row is only inserted once, so fixing the
		// default is not enough for a database that already has it.
		name: "0008_harness_note",
		stmts: []string{
			`UPDATE harness SET notes =
				'resume needs a session id, which only a runner that reports one can supply'
			 WHERE id = 'claude' AND notes LIKE '%gwt ledger%'`,
		},
	},
	{
		// A path tells you which file, not what is changing. The diff is what
		// a decision actually rests on.
		name: "0009_permission_details",
		stmts: []string{
			`ALTER TABLE permission ADD COLUMN details TEXT NOT NULL DEFAULT ''`,
		},
	},
	{
		// The dedup key was unique across the whole table. That is wrong the
		// moment two agents derive keys the same way, say from a hash of the
		// command: agent B's identical request would collide with agent A's
		// row and be handed A's answer. The key only ever means "this agent's
		// same request", so scope it to the task.
		//
		// SQLite cannot drop a UNIQUE declared in CREATE TABLE, so the table is
		// rebuilt. Done inside the migration's transaction, so a failure part
		// way through leaves the original in place.
		name: "0010_dedup_key_per_task",
		stmts: []string{
			`CREATE TABLE permission_rebuilt (
				id           TEXT PRIMARY KEY,
				task_id      TEXT NOT NULL REFERENCES task(id) ON DELETE CASCADE,
				tool         TEXT NOT NULL,
				command      TEXT NOT NULL,
				requested_at TEXT NOT NULL,
				decided_at   TEXT,
				decision     TEXT CHECK (decision IS NULL OR decision IN ('approve','block')),
				reason       TEXT NOT NULL DEFAULT '',
				dedup_key    TEXT,
				decided_by   TEXT NOT NULL DEFAULT '',
				rule_created TEXT NOT NULL DEFAULT '',
				details      TEXT NOT NULL DEFAULT '',
				UNIQUE (task_id, dedup_key)
			)`,
			`INSERT INTO permission_rebuilt
				(id, task_id, tool, command, requested_at, decided_at, decision, reason,
				 dedup_key, decided_by, rule_created, details)
			 SELECT id, task_id, tool, command, requested_at, decided_at, decision, reason,
				 dedup_key, decided_by, rule_created, details FROM permission`,
			`DROP TABLE permission`,
			`ALTER TABLE permission_rebuilt RENAME TO permission`,
			`CREATE INDEX IF NOT EXISTS permission_pending ON permission (decided_at, requested_at)`,
		},
	},
	{
		// Whether a session has joined atrium. Until now gating was decided
		// once, when the session started, from its environment, so a session
		// that was not gated stayed ungated for its whole life. This lets a
		// session opt in or out while it is running, which means the gate has
		// to be state rather than an environment variable.
		name: "0011_task_gated",
		stmts: []string{
			`ALTER TABLE task ADD COLUMN gated INTEGER NOT NULL DEFAULT 0`,
		},
	},
}

// migrate applies any migration not already recorded. This runs before the
// wedge exists, so failures return an error and the daemon refuses to start.
func (s *Store) migrate() error {
	if _, err := s.db.Exec(
		`CREATE TABLE IF NOT EXISTS schema_migration (
			name       TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return err
	}
	for _, m := range migrations {
		var seen string
		err := s.db.QueryRow(`SELECT name FROM schema_migration WHERE name = ?`, m.name).Scan(&seen)
		if err == nil {
			continue
		}
		if err != sql.ErrNoRows {
			return fmt.Errorf("check %s: %w", m.name, err)
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		for _, stmt := range m.stmts {
			if _, err := tx.Exec(stmt); err != nil {
				// ADD COLUMN has no IF NOT EXISTS in SQLite, and a column that
				// is already there is the state we wanted anyway.
				if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
					continue
				}
				tx.Rollback()
				return fmt.Errorf("%s: %w", m.name, err)
			}
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migration (name, applied_at) VALUES (?, ?)`,
			m.name, ts(now())); err != nil {
			tx.Rollback()
			return fmt.Errorf("record %s: %w", m.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", m.name, err)
		}
	}
	return nil
}
