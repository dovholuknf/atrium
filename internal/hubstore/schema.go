package hubstore

import (
	"database/sql"
	"fmt"
	"strings"
)

// The hub's schema.
//
// ── read this before touching it ────────────────────────
//
// Migrations are applied in slice order and recorded by name. A migration
// already recorded will never run again, so editing one is the same as not
// shipping it. Two rules follow, and both are here because both were broken in
// the room's schema:
//
//   - ADD AT THE END OF THE SLICE. Adding in the middle means an existing
//     database silently skips it.
//   - WRITE EVERY STATEMENT TO TOLERATE ALREADY BEING THERE.
//     `CREATE TABLE IF NOT EXISTS`, and the runner swallows "duplicate column
//     name" for `ADD COLUMN`, which SQLite has no `IF NOT EXISTS` for.
//
// ── written for Postgres ────────────────────────────────
//
// Text keys instead of AUTOINCREMENT, RFC3339 text instead of a native
// timestamp type, CHECK constraints instead of enums, TEXT instead of JSONB,
// `?` placeholders. Nothing has ever run this against Postgres, and the point
// is that the day somebody needs to, the schema is not what stops them.
var migrations = []struct {
	name  string
	stmts []string
}{
	{
		name: "0001_rooms",
		stmts: []string{
			// A ROOM IS DURABLE. `add a room` writes this row, and the room
			// appears in the list from that moment, before it has ever
			// connected, and stays there when it is offline.
			//
			// The reason is inventory. "Have I already made that room" has to
			// be answerable, and a list that only shows what is currently
			// dialled in cannot answer it.
			`CREATE TABLE IF NOT EXISTS room (
				id            TEXT PRIMARY KEY,
				-- THE HUB'S NAME FOR THE ROOM, and the only name that routes.
				-- Minted into the join token, so a secret authorises one name
				-- and there is nothing for a room to claim.
				name          TEXT NOT NULL,
				-- The folded form, which is what UNIQUE is on. Two people
				-- typing Sparta and sparta mean one room.
				name_key      TEXT NOT NULL,
				-- WHAT THE ROOM CALLS ITSELF, observed and never authoritative.
				-- This is docs/architecture-v2.md's observed-versus-overrides
				-- rule, not a new one: what a machine reports never overwrites
				-- what a human typed. Shown beside the name, never instead of
				-- it.
				self_name     TEXT NOT NULL DEFAULT '',
				-- Ancillary noise that earns a badge and never a concept: how
				-- this room reaches the hub.
				-- "local" is the hub's own room, which reaches the hub over a
				-- pipe inside one process and crosses no network at all. It is
				-- a row like any other, because decision 3 says the list
				-- describes everything that can run agents, and a settings cog
				-- with nothing to attach to was the problem that started all
				-- of this.
				transport     TEXT NOT NULL DEFAULT 'direct'
				                CHECK (transport IN ('direct','ziti','zrok','zrok-public','local')),
				state         TEXT NOT NULL DEFAULT 'active'
				                CHECK (state IN ('active','marked-for-deletion')),
				created_at    TEXT NOT NULL,
				-- Empty until the room has ever dialled in. A room that has
				-- never connected cannot have cards, so it draws nowhere but
				-- the rooms tab, and this column is how that is known.
				first_seen_at TEXT NOT NULL DEFAULT '',
				last_seen_at  TEXT NOT NULL DEFAULT '',
				-- Observed, like self_name: what it was running when last
				-- heard from.
				version       TEXT NOT NULL DEFAULT '',
				UNIQUE (name_key)
			)`,

			// THE JOIN SECRET, HASHED, AND SHOWN ONCE.
			//
			// Hashed the way a password is: the hub only ever compares, so it
			// has no business holding the original. If the string is lost the
			// hub mints another rather than revealing the old one, which is
			// what makes "copy once" true rather than a label.
			//
			// One live secret per room. Minting again replaces what was there,
			// so a room cannot accumulate a drawer full of working credentials
			// nobody remembers issuing.
			`CREATE TABLE IF NOT EXISTS room_secret (
				room_id    TEXT PRIMARY KEY REFERENCES room(id) ON DELETE CASCADE,
				hash       TEXT NOT NULL,
				created_at TEXT NOT NULL,
				expires_at TEXT NOT NULL
			)`,

			// THE CACHE, AND IT IS NEVER AUTHORITATIVE.
			//
			// What a room last said, so a hub whose room is offline can show
			// what was there rather than nothing. Read only while that room is
			// offline; written and never read while it is connected.
			//
			// ONE OPAQUE PAYLOAD RATHER THAN A COLUMN PER FIELD. The rule is
			// that the hub caches exactly what the room itself persists and
			// never what the room declines to persist, and a field list here
			// would be a second copy of the room's schema that drifts from it.
			// Status is lifted out because the board groups on it and nothing
			// else is.
			`CREATE TABLE IF NOT EXISTS room_card (
				room_id   TEXT NOT NULL REFERENCES room(id) ON DELETE CASCADE,
				card_id   TEXT NOT NULL,
				status    TEXT NOT NULL DEFAULT '',
				payload   TEXT NOT NULL DEFAULT '{}',
				cached_at TEXT NOT NULL,
				PRIMARY KEY (room_id, card_id)
			)`,
			`CREATE INDEX IF NOT EXISTS room_card_room ON room_card (room_id, status)`,

			// WHAT HAPPENED TO A ROOM, KEPT AFTER THE ROOM IS GONE.
			//
			// No foreign key on purpose. Forcing out a machine that is never
			// coming back removes its row, and the record of having done that
			// is exactly what somebody will want afterwards. A cascade would
			// delete the explanation along with the thing it explains.
			//
			// It also carries the discard notice from a reconnect: coming back
			// replaces a room's cache wholesale, which is the right rule and
			// also the one that can quietly lose something a person remembers
			// seeing.
			`CREATE TABLE IF NOT EXISTS room_audit (
				id        TEXT PRIMARY KEY,
				at        TEXT NOT NULL,
				room_id   TEXT NOT NULL DEFAULT '',
				room_name TEXT NOT NULL DEFAULT '',
				kind      TEXT NOT NULL,
				detail    TEXT NOT NULL DEFAULT ''
			)`,
			`CREATE INDEX IF NOT EXISTS room_audit_at ON room_audit (at)`,

			// The hub's own settings, which are neither a room's nor the
			// board's. Keyed by name for the same reason the room's are: a
			// column per setting is a migration per setting.
			`CREATE TABLE IF NOT EXISTS hub_setting (
				name       TEXT PRIMARY KEY,
				value      TEXT NOT NULL DEFAULT '',
				updated_at TEXT NOT NULL
			)`,
		},
	},
}

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
