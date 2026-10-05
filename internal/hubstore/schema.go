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
	{
		// THE ROOM SAYING IT IS DONE, which is the step decision 9 puts between
		// marking a room and removing it.
		//
		// The hub holds nothing and cannot see whether a directory was cleaned
		// up, a throwaway deleted or a session really ended, and it must not
		// decide those from the outside. The room is the only thing that knows.
		//
		// What it means in practice is the room announcing, while connected,
		// that it is holding no cards. That IS the confirmation: it is the room
		// speaking about its own state, which is the only kind of answer the
		// design accepts about a room.
		//
		// Set when that happens and cleared the moment the room says it has
		// work again, so it cannot go stale into a removal.
		name: "0002_room_confirms_it_is_clear",
		stmts: []string{
			`ALTER TABLE room ADD COLUMN cleared_at TEXT NOT NULL DEFAULT ''`,
		},
	},
	{
		// WHAT THE NOTIFY TRIGGER LAST ACTED ON, per card. See notify.go: a row
		// is kept until its identity changes, not until its card leaves the
		// cache, so a card that returns unchanged does not notify twice. No
		// foreign key on purpose, for the same reason room_audit has none.
		name: "0003_notify_sent",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS notify_sent (
				room_id  TEXT NOT NULL,
				card_id  TEXT NOT NULL,
				identity TEXT NOT NULL,
				at       TEXT NOT NULL,
				PRIMARY KEY (room_id, card_id)
			)`,
		},
	},
	{
		// WORK THAT WAITS ON OTHER WORK. See deps.go and
		// docs/runtime/item-dependencies-design.md. On the hub because the hub sees
		// every room and the integration branch, so a gate across rooms is one row.
		//
		// The partial index is what lets a met gate be added again: only OPEN rows
		// are unique. SQLite and Postgres both have partial indexes.
		name: "0004_item_gate",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS item_gate (
				id        TEXT PRIMARY KEY,
				repo      TEXT NOT NULL,
				item      TEXT NOT NULL,
				kind      TEXT NOT NULL CHECK (kind IN ('item','cond')),
				target    TEXT NOT NULL,
				why       TEXT NOT NULL DEFAULT '',
				added_by  TEXT NOT NULL,
				added_at  TEXT NOT NULL,
				met_at    TEXT NOT NULL DEFAULT '',
				met_by    TEXT NOT NULL DEFAULT '' CHECK (met_by IN ('','atrium','human')),
				met_why   TEXT NOT NULL DEFAULT '',
				told_at   TEXT NOT NULL DEFAULT ''
			)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS item_gate_open ON item_gate (repo, item, kind, target) WHERE met_at = ''`,
			`CREATE INDEX IF NOT EXISTS item_gate_item ON item_gate (repo, item)`,
			`CREATE TABLE IF NOT EXISTS item_rename (
				repo    TEXT NOT NULL,
				from_id TEXT NOT NULL,
				to_id   TEXT NOT NULL,
				at      TEXT NOT NULL,
				by      TEXT NOT NULL,
				PRIMARY KEY (repo, from_id)
			)`,
		},
	},
	{
		// SOMETHING WAITING ON A HUMAN, SHARED BY EVERY SCREEN. See growl.go and
		// docs/rnd/persistent-growler-design.md. The id is the notify identity
		// with the room in front, so a republished card is the same row. No
		// foreign key, for the reason room_audit has none: the record of a
		// dismissal outlives the room it was about until the prune takes it.
		//
		// `ended_at` is not in the design's table. It is when the REASON ended.
		// An ended reason makes every row `resolved`, a dismissed one included,
		// so a dismissal can only be undone while its reason stands (agreed with
		// @ui). The row's `changed_*` then say the hub ended it, and who dismissed
		// it is on the board's toast log, not here.
		name: "0005_growl",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS growl (
				id          TEXT PRIMARY KEY,
				room_id     TEXT NOT NULL,
				card_id     TEXT NOT NULL DEFAULT '',
				reason      TEXT NOT NULL CHECK (reason IN ('permission','halt','blocked','question','deploy-hold')),
				title       TEXT NOT NULL,
				body        TEXT NOT NULL DEFAULT '',
				subject     TEXT NOT NULL DEFAULT '',
				raised_at   TEXT NOT NULL,
				state       TEXT NOT NULL CHECK (state IN ('open','snoozed','dismissed','acted','resolved')),
				until       TEXT NOT NULL DEFAULT '',
				reminders   INTEGER NOT NULL DEFAULT 0,
				changed_at  TEXT NOT NULL,
				changed_via TEXT NOT NULL DEFAULT '',
				changed_tab TEXT NOT NULL DEFAULT '',
				ended_at    TEXT NOT NULL DEFAULT ''
			)`,
			`CREATE INDEX IF NOT EXISTS growl_room ON growl (room_id, reason, ended_at)`,
			`CREATE INDEX IF NOT EXISTS growl_state ON growl (state)`,
		},
	},
	{
		// WHETHER THIS ROOM HAS EVER PROVED ITS NAME WITH A CERTIFICATE. The hub
		// refuses the old, certificate-less overlay path for a name that has, so a
		// peer the overlay admits cannot wear it (f-026). Set when a join secret is
		// spent and when a room attaches with a certificate.
		//
		// BACKFILLED FROM THE `joined` AUDIT LINES, which were the only record
		// before this. They are trimmed with the rest of the audit window, which
		// is why this is a column: a record that ages out turns a proven name
		// back into one anybody may claim. The backfill only fills an empty
		// column, so running it twice changes nothing.
		name: "0006_room_enrolled",
		stmts: []string{
			`ALTER TABLE room ADD COLUMN enrolled_at TEXT NOT NULL DEFAULT ''`,
			`UPDATE room SET enrolled_at = (
				SELECT MIN(a.at) FROM room_audit a WHERE a.room_id = room.id AND a.kind = 'joined')
			  WHERE enrolled_at = ''
			    AND EXISTS (SELECT 1 FROM room_audit a WHERE a.room_id = room.id AND a.kind = 'joined')`,
		},
	},
	{
		// HUB DOCUMENTS, the rows half. See docs.go and docs/rnd/hub-documents-design.md.
		// The BYTES ARE NOT HERE: they are files named for their SHA-256 under
		// `<hub data dir>/docs/`, so the ten-minute snapshots stay small. A version row
		// whose file is gone says so and does not fail the page.
		//
		// The slug is the key and never changes. A rename changes only `title`. No
		// foreign key on the card, for the reason room_audit has none: a document
		// outlives the card that wrote it, which is the point.
		//
		// ON DELETE CASCADE ON doc_version IS INERT. Nothing deletes a `doc` row: a delete
		// is a tombstone, and a purge removes bytes and leaves rows. Foreign keys are ON for
		// the store's one connection (Open sets the pragma), but a cascade is not relied on
		// anywhere, and a future delete of a doc must remove its versions itself. The flags
		// below are 0 or 1 by CHECK because they are read as `= 1`, and a 2 would be both
		// not purged and not live.
		name: "0007_docs",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS doc (
				slug       TEXT PRIMARY KEY,
				title      TEXT NOT NULL,
				created_at TEXT NOT NULL,
				-- A tombstone, not a removal. Empty means live.
				deleted_at TEXT NOT NULL DEFAULT '',
				deleted_by TEXT NOT NULL DEFAULT ''
			)`,
			`CREATE TABLE IF NOT EXISTS doc_version (
				doc      TEXT NOT NULL REFERENCES doc(slug) ON DELETE CASCADE,
				n        INTEGER NOT NULL,
				at       TEXT NOT NULL,
				by       TEXT NOT NULL,
				-- Decided by the hub from the request and never read from a form.
				origin   TEXT NOT NULL CHECK (origin IN ('local','share','card')),
				-- room~id, the card URL form. Empty unless origin is card.
				card     TEXT NOT NULL DEFAULT '',
				size     INTEGER NOT NULL,
				kind     TEXT NOT NULL CHECK (kind IN ('markdown','text','image','diff','other')),
				mime     TEXT NOT NULL,
				name     TEXT NOT NULL DEFAULT '',
				sha      TEXT NOT NULL,
				-- The operator deleted the bytes. The row stays.
				purged   INTEGER NOT NULL DEFAULT 0 CHECK (purged IN (0,1)),
				-- The operator let this one past a secret rule.
				override INTEGER NOT NULL DEFAULT 0 CHECK (override IN (0,1)),
				PRIMARY KEY (doc, n)
			)`,
			`CREATE INDEX IF NOT EXISTS doc_version_sha ON doc_version (sha)`,
			`CREATE INDEX IF NOT EXISTS doc_version_card ON doc_version (card, at)`,
		},
	},
	{
		// THE HUB STORE'S PUSH LOG: one row per ref a push to the hub's own git store updated, and a marker
		// row when a branch is let go. The owner of a branch is the first push row after the latest release
		// marker, so nothing here is edited or deleted by hand: a release is a new row.
		//
		// A push is written as `pending` BEFORE git runs and settled after, so a hub that dies between git
		// moving a ref and the row being settled still has the branch owned. A pending row counts as a push for
		// ownership. Settling turns it into `done` (and writes the release marker of the branch it took over,
		// in the same transaction) or deletes it when git took nothing; internal/gitsync reconciles what a
		// crash left.
		//
		// id is a text key that sorts in the order rows were WRITTEN, and the order is the whole design: it is
		// taken inside the transaction as the larger of the clock and the table's highest id plus one, so a
		// restart, or a machine whose clock is behind, cannot put a release ahead of the owner it releases.
		// (Text, and not a rowid, so that a Postgres move has nothing to lean on.) Room and card are both empty
		// for the operator. On a release row they are the owner that was let go, and released_by says why.
		name: "0008_git_push",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS git_push (
				id          TEXT PRIMARY KEY,
				kind        TEXT NOT NULL CHECK (kind IN ('push','release')),
				state       TEXT NOT NULL DEFAULT 'done' CHECK (state IN ('done','pending')),
				-- The rows of one push, while it is pending. Empty on a done row.
				batch       TEXT NOT NULL DEFAULT '',
				repo        TEXT NOT NULL,
				ref         TEXT NOT NULL,
				old_sha     TEXT NOT NULL DEFAULT '',
				new_sha     TEXT NOT NULL DEFAULT '',
				room        TEXT NOT NULL DEFAULT '',
				card        TEXT NOT NULL DEFAULT '',
				at          TEXT NOT NULL,
				released_by TEXT NOT NULL DEFAULT ''
			)`,
			`CREATE INDEX IF NOT EXISTS git_push_ref ON git_push (repo, ref, id)`,
			`CREATE INDEX IF NOT EXISTS git_push_pending ON git_push (state, repo)`,
		},
	},
	{
		// CHANGE REQUESTS BETWEEN ROOMS: one row per request, kept for good. See changerequest.go and
		// docs/rnd/hub-forge-design.md section 6. Nothing is deleted: a request ends by changing `state`, once.
		//
		// `id` is `cr_<n>` and `n` is its own column, taken inside the transaction as the table's highest n plus
		// one (the rule 0008 gives its ids), so an id is never reused and the order of ids is the order of
		// creation. Rooms and cards are TEXT with no foreign key, for the reason room_audit has none: a request
		// outlives the card and the room it was about.
		//
		// A source room of '' is a branch pushed to the hub. A card of 'operator' is the operator. `closed_at` of
		// '' is a request still open, and the closer is the closed_* pair. The partial index is the rule that only
		// one OPEN request exists for a source and a target, as 0004's does for gates: a closed one may be asked
		// again.
		name: "0009_change_request",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS change_request (
				id            TEXT PRIMARY KEY,
				n             INTEGER NOT NULL UNIQUE,
				repo          TEXT NOT NULL,
				source_room   TEXT NOT NULL DEFAULT '',
				source_branch TEXT NOT NULL,
				source_sha    TEXT NOT NULL DEFAULT '',
				target_branch TEXT NOT NULL,
				title         TEXT NOT NULL,
				why           TEXT NOT NULL DEFAULT '',
				change_id     TEXT NOT NULL DEFAULT '',
				state         TEXT NOT NULL DEFAULT 'open' CHECK (state IN ('open','merged','closed','withdrawn')),
				created_room  TEXT NOT NULL DEFAULT '',
				created_card  TEXT NOT NULL,
				created_at    TEXT NOT NULL,
				closed_at     TEXT NOT NULL DEFAULT '',
				closed_room   TEXT NOT NULL DEFAULT '',
				closed_card   TEXT NOT NULL DEFAULT '',
				note          TEXT NOT NULL DEFAULT '',
				merged_sha    TEXT NOT NULL DEFAULT '',
				owner_room    TEXT NOT NULL DEFAULT '',
				owner_card    TEXT NOT NULL DEFAULT ''
			)`,
			`CREATE INDEX IF NOT EXISTS change_request_state ON change_request (state)`,
			`CREATE INDEX IF NOT EXISTS change_request_repo ON change_request (repo)`,
			`CREATE INDEX IF NOT EXISTS change_request_source_room ON change_request (source_room)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS change_request_open
				ON change_request (repo, source_room, source_branch, target_branch) WHERE state = 'open'`,
		},
	},
	{
		// ONE OWNER PER PR ACROSS ROOMS: an index, never a copy. The findings, the diff and the run folder stay on the
		// room that holds the row. `key` is host/org/repo/number, lower case. `room` is the room NAME, with no foreign
		// key, for the reason room_audit has none: a claim outlives the room's attachment, and a claim whose room is
		// offline stays where it is, because a PR is never re-placed automatically. `warned` and `warn_n` belong to
		// the offline warning: `warned` is 1 while a growler for the current spell is up, and `warn_n` makes each
		// spell's growler id new, since an id that was ever raised is never raised again. See prclaim.go.
		name: "0010_pr_claim",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS pr_claim (
				key        TEXT PRIMARY KEY,
				room       TEXT NOT NULL,
				source     TEXT NOT NULL DEFAULT '',
				claimed_at TEXT NOT NULL,
				warned     INTEGER NOT NULL DEFAULT 0,
				warn_n     INTEGER NOT NULL DEFAULT 0
			)`,
			`CREATE INDEX IF NOT EXISTS pr_claim_room ON pr_claim (room)`,
		},
	},
	{
		// BACKLOG ITEMS AND DIRECTOR REPORTS THAT EVERY ROOM CAN READ AND WRITE. See backlog.go and
		// docs/rnd/reports-channel-design.md. They replace a file in one room's checkout that no other room could see.
		//
		// An item's id is the one the filer gives (`f-new-x`, `r-037`), as the markdown files' ids are, and is never
		// reused. Status is typed by whoever changes it, or follows the card linked by `card`: built on a done report, blocked or
		// incomplete on those. A report is append-only: it
		// is read once and marked, and nothing edits its words. Rooms and cards are TEXT with no foreign key, for the
		// reason room_audit has none.
		name: "0011_backlog_reports",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS backlog_item (
					id          TEXT PRIMARY KEY,
					dept        TEXT NOT NULL,
					title       TEXT NOT NULL,
					body        TEXT NOT NULL DEFAULT '',
					priority    TEXT NOT NULL DEFAULT '',
					status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','held','in-progress','built','blocked','incomplete','done','dropped')),
					card        TEXT NOT NULL DEFAULT '',
					filed_by    TEXT NOT NULL DEFAULT '',
					filed_room  TEXT NOT NULL DEFAULT '',
					created_at  TEXT NOT NULL,
					changed_at  TEXT NOT NULL,
					changed_by  TEXT NOT NULL DEFAULT ''
				)`,
			`CREATE INDEX IF NOT EXISTS backlog_item_dept ON backlog_item (dept, status)`,
			`CREATE TABLE IF NOT EXISTS director_report (
					n          INTEGER PRIMARY KEY,
					id         TEXT NOT NULL UNIQUE,
					to_dept    TEXT NOT NULL DEFAULT '',
					from_by    TEXT NOT NULL DEFAULT '',
					from_room  TEXT NOT NULL DEFAULT '',
					subject    TEXT NOT NULL,
					body       TEXT NOT NULL DEFAULT '',
					at         TEXT NOT NULL,
					read_at    TEXT NOT NULL DEFAULT '',
					read_by    TEXT NOT NULL DEFAULT ''
				)`,
			`CREATE INDEX IF NOT EXISTS director_report_unread ON director_report (read_at, to_dept)`,
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
