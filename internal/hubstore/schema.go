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
				-- This is docs/archive/architecture-v2.md's observed-versus-overrides
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
		// docs/fabric/hub-forge-design.md section 6. Nothing is deleted: a request ends by changing `state`, once.
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
		// WEB PUSH SUBSCRIPTIONS: one row per browser that asked to be told when a card wants it. See push.go and
		// docs/rnd/web-push-design.md. `endpoint` is the proof of ownership and is unique, so a browser that
		// subscribes twice is one row. `p256dh` and `auth` are the browser's own keys for encrypting to it. They
		// are the browser's secrets, handed over for exactly this use, and are never returned by a list.
		// `failures` counts sends that failed in a row, and `disabled_reason` is set when three did and the
		// subscription was switched off. The VAPID private key is NOT here: it is a hub_setting.
		name: "0011_push_subscription",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS push_subscription (
				id              TEXT PRIMARY KEY,
				endpoint        TEXT NOT NULL UNIQUE,
				p256dh          TEXT NOT NULL,
				auth            TEXT NOT NULL,
				label           TEXT NOT NULL DEFAULT '',
				origin          TEXT NOT NULL DEFAULT '',
				created_at      TEXT NOT NULL,
				failures        INTEGER NOT NULL DEFAULT 0,
				disabled_reason TEXT NOT NULL DEFAULT ''
			)`,
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
		name: "0012_backlog_reports",
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
	{
		// WHAT A ROOM IS, AS DATA. room_spec is the desired room.yaml the hub keeps, verbatim, with its hash and who set
		// it. room_lock is the observed room.lock the room last posted, verbatim, with when it arrived. They are two
		// tables so observed can never overwrite desired. See roomspec.go.
		name: "0013_room_spec",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS room_spec (
					room_id  TEXT PRIMARY KEY REFERENCES room(id) ON DELETE CASCADE,
					yaml     TEXT NOT NULL,
					sha256   TEXT NOT NULL,
					set_by   TEXT NOT NULL DEFAULT '',
					set_at   TEXT NOT NULL
				)`,
			`CREATE TABLE IF NOT EXISTS room_lock (
					room_id      TEXT PRIMARY KEY REFERENCES room(id) ON DELETE CASCADE,
					lock_json    TEXT NOT NULL,
					received_at  TEXT NOT NULL
				)`,
		},
	},
	{
		// A BACKLOG ITEM'S PRIORITY IS ONE OF high, medium, low OR '' (unset), by CHECK. It was free text. SQLite cannot
		// add a CHECK in place, so backlog_item is rebuilt, the way the room store's 0010 and 0014 do it. Nothing
		// references backlog_item, so the DROP cascades to nothing. The old text is folded to lower case and trimmed
		// as it is copied: high, medium and low stay, anything else (blank, `p2`, `urgent`) becomes unset.
		name: "0014_backlog_priority",
		stmts: []string{
			`DROP TABLE IF EXISTS backlog_item_rebuilt`,
			`CREATE TABLE backlog_item_rebuilt (
				id          TEXT PRIMARY KEY,
				dept        TEXT NOT NULL,
				title       TEXT NOT NULL,
				body        TEXT NOT NULL DEFAULT '',
				priority    TEXT NOT NULL DEFAULT '' CHECK (priority IN ('','high','medium','low')),
				status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','held','in-progress','built','blocked','incomplete','done','dropped')),
				card        TEXT NOT NULL DEFAULT '',
				filed_by    TEXT NOT NULL DEFAULT '',
				filed_room  TEXT NOT NULL DEFAULT '',
				created_at  TEXT NOT NULL,
				changed_at  TEXT NOT NULL,
				changed_by  TEXT NOT NULL DEFAULT ''
			)`,
			`INSERT INTO backlog_item_rebuilt (id, dept, title, body, priority, status, card, filed_by, filed_room,
					created_at, changed_at, changed_by)
				SELECT id, dept, title, body,
					CASE WHEN LOWER(TRIM(priority)) IN ('high','medium','low') THEN LOWER(TRIM(priority)) ELSE '' END,
					status, card, filed_by, filed_room, created_at, changed_at, changed_by
				FROM backlog_item`,
			`DROP TABLE backlog_item`,
			`ALTER TABLE backlog_item_rebuilt RENAME TO backlog_item`,
			`CREATE INDEX IF NOT EXISTS backlog_item_dept ON backlog_item (dept, status)`,
		},
	},
	{
		// THE MERGE FLOW'S STATE. See flow.go and docs/design/merge-flow.md. flow_role is who holds each role, one row per
		// holder (only the tester may have several; flow.go enforces that, so the table need not). flow_item is a backlog
		// item's stage, kept apart from backlog_item.status so nothing that reads that column changes. flow_check is a
		// verdict on a sha of main, and flow_deploy is the sha a room runs. Cards and rooms are TEXT with no foreign key.
		name: "0015_merge_flow",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS flow_role (
					role    TEXT NOT NULL CHECK (role IN ('orchestrator','merger','tester','director')),
					room    TEXT NOT NULL,
					card    TEXT NOT NULL,
					since   TEXT NOT NULL,
					set_by  TEXT NOT NULL DEFAULT '',
					PRIMARY KEY (role, room, card)
				)`,
			`CREATE TABLE IF NOT EXISTS flow_item (
					item_id    TEXT PRIMARY KEY,
					stage      TEXT NOT NULL DEFAULT 'idea' CHECK (stage IN ('idea','designed','implementing','implemented','merged','tested','deployed','pushed-by-clint')),
					branch     TEXT NOT NULL DEFAULT '',
					sha        TEXT NOT NULL DEFAULT '',
					note       TEXT NOT NULL DEFAULT '',
					changed_at TEXT NOT NULL,
					changed_by TEXT NOT NULL DEFAULT ''
				)`,
			`CREATE TABLE IF NOT EXISTS flow_check (
					n       INTEGER PRIMARY KEY,
					sha     TEXT NOT NULL,
					name    TEXT NOT NULL,
					result  TEXT NOT NULL CHECK (result IN ('pass','fail','timeout')),
					by      TEXT NOT NULL DEFAULT '',
					at      TEXT NOT NULL
				)`,
			`CREATE INDEX IF NOT EXISTS flow_check_sha ON flow_check (sha, name)`,
			`CREATE TABLE IF NOT EXISTS flow_deploy (
					room    TEXT PRIMARY KEY,
					sha     TEXT NOT NULL,
					at      TEXT NOT NULL,
					by      TEXT NOT NULL DEFAULT ''
				)`,
		},
	},
	{
		// ONE ROW PER ROUND OF clint FETCHING claude/main, re-signing and pushing, so what landed in between can be
		// accounted for. See flow_epoch.go and docs/design/merge-flow.md, "Between clint's fetch and clint's push".
		name: "0016_flow_epoch",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS flow_epoch (
					n            INTEGER PRIMARY KEY,
					state        TEXT NOT NULL DEFAULT 'open' CHECK (state IN ('open','closed')),
					fetched_sha  TEXT NOT NULL,
					old_tip      TEXT NOT NULL DEFAULT '',
					new_tip      TEXT NOT NULL DEFAULT '',
					opened_at    TEXT NOT NULL,
					opened_by    TEXT NOT NULL DEFAULT '',
					closed_at    TEXT NOT NULL DEFAULT '',
					closed_by    TEXT NOT NULL DEFAULT ''
				)`,
		},
	},
	{
		// THE HUB FILE DROP: a card on any room hands files to a card on any other. One row per name, bytes in
		// <hub dir>/files/<sha256>. See files.go and docs/rnd/hub-file-drop.md.
		name: "0017_hub_files",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS hub_files (
				name      TEXT PRIMARY KEY,
				class     TEXT NOT NULL DEFAULT 'drop' CHECK (class IN ('drop','artifact')),
				size      INTEGER NOT NULL,
				sha       TEXT NOT NULL,
				origin    TEXT NOT NULL CHECK (origin IN ('local','share','card')),
				by        TEXT NOT NULL DEFAULT '',
				card      TEXT NOT NULL DEFAULT '',
				room      TEXT NOT NULL DEFAULT '',
				note      TEXT NOT NULL DEFAULT '',
				at        TEXT NOT NULL,
				expires   TEXT NOT NULL,
				downloads INTEGER NOT NULL DEFAULT 0,
				last_get  TEXT NOT NULL DEFAULT ''
			)`,
			`CREATE INDEX IF NOT EXISTS hub_files_sha ON hub_files (sha)`,
			`CREATE INDEX IF NOT EXISTS hub_files_card ON hub_files (card, at)`,
			`CREATE INDEX IF NOT EXISTS hub_files_expires ON hub_files (expires)`,
		},
	},
	{
		// ITEMS, OWNED HERE: one assignment of work, pushed by the room that runs it. item_event holds every
		// (item_id, revision, state) ever applied, which is what makes a replayed push a no-op. See item.go.
		name: "0018_item",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS item (
				item_id    TEXT PRIMARY KEY,
				room       TEXT NOT NULL,
				card       TEXT NOT NULL,
				generation INTEGER NOT NULL DEFAULT 1,
				state      TEXT NOT NULL CHECK (state IN ('queued','running','blocked','finished')),
				revision   INTEGER NOT NULL,
				owner_kind TEXT NOT NULL CHECK (owner_kind IN ('session','role','human')),
				owner      TEXT NOT NULL DEFAULT '',
				created_by TEXT NOT NULL DEFAULT '',
				note       TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS item_owner ON item (owner_kind, owner, state)`,
			`CREATE INDEX IF NOT EXISTS item_room ON item (room, updated_at)`,
			`CREATE TABLE IF NOT EXISTS item_event (
				item_id  TEXT NOT NULL,
				revision INTEGER NOT NULL,
				state    TEXT NOT NULL,
				at       TEXT NOT NULL,
				PRIMARY KEY (item_id, revision, state)
			)`,
		},
	},
	{
		// A ROOM'S SSH LOGIN, AS PUBLISHED BY THE ROOM: the user other rooms log in as and the public key this room logs
		// in with. Gone with the room by cascade, so a removed room's key leaves the set. See roomssh.go.
		name: "0019_room_ssh",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS room_ssh (
				room_id    TEXT PRIMARY KEY REFERENCES room(id) ON DELETE CASCADE,
				login_user TEXT NOT NULL,
				pubkey     TEXT NOT NULL,
				port       INTEGER NOT NULL DEFAULT 22,
				ssh_on     INTEGER NOT NULL DEFAULT 0,
				updated_at TEXT NOT NULL
			)`,
		},
	},
	{
		// ITEM LIFECYCLE L1: THE ITEM IS THE UNIT OF WORK. `work` is one row per backlog id, and `work_card` is every card
		// attached to it, so two cards launched on one id are one item with two cards. The old `item` table keeps its job as
		// one card's run (an assignment). See work.go and docs/rnd/item-lifecycle-design.md.
		name: "0020_work",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS work (
				backlog_id TEXT PRIMARY KEY,
				title      TEXT NOT NULL DEFAULT '',
				body       TEXT NOT NULL DEFAULT '',
				dept       TEXT NOT NULL DEFAULT '',
				priority   TEXT NOT NULL DEFAULT '',
				kind       TEXT NOT NULL DEFAULT 'code' CHECK (kind IN ('code','doc','mixed')),
				owner_kind TEXT NOT NULL DEFAULT '',
				owner      TEXT NOT NULL DEFAULT '',
				branch     TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS work_card (
				backlog_id  TEXT NOT NULL,
				room        TEXT NOT NULL,
				card        TEXT NOT NULL,
				role        TEXT NOT NULL DEFAULT '',
				step        TEXT NOT NULL DEFAULT '',
				attached_at TEXT NOT NULL,
				PRIMARY KEY (backlog_id, room, card)
			)`,
			`CREATE INDEX IF NOT EXISTS work_card_card ON work_card (room, card)`,
		},
	},
	{
		// WHICH ITEM AN ASSIGNMENT BELONGS TO. Its own migration: nothing here changes a CHECK or a UNIQUE, so no rebuild.
		name: "0021_item_work_id",
		stmts: []string{
			`ALTER TABLE item ADD COLUMN work_id TEXT NOT NULL DEFAULT ''`,
			`CREATE INDEX IF NOT EXISTS item_work ON item (work_id)`,
		},
	},
	{
		// THE BACKFILL: every assignment from before this has no backlog id, so it becomes an `adhoc-<item_id>` item, titled
		// with its card's title as the room last cached it (the item id when the cache has none), with its one card
		// attached. OR IGNORE and the `work_id = ''` guard make a second run do nothing.
		name: "0022_work_backfill",
		stmts: []string{
			`INSERT OR IGNORE INTO work (backlog_id, title, kind, created_at, updated_at)
				SELECT 'adhoc-' || i.item_id,
					COALESCE(NULLIF(CASE WHEN json_valid(rc.payload) THEN json_extract(rc.payload, '$.display_title') END, ''), i.item_id),
					'code', i.created_at, i.updated_at
				FROM item i
				LEFT JOIN room r ON r.name_key = i.room
				LEFT JOIN room_card rc ON rc.room_id = r.id AND rc.card_id = i.card
				WHERE i.work_id = ''`,
			`INSERT OR IGNORE INTO work_card (backlog_id, room, card, role, step, attached_at)
				SELECT 'adhoc-' || item_id, room, card, '', 'building', created_at FROM item WHERE work_id = ''`,
			`UPDATE item SET work_id = 'adhoc-' || item_id WHERE work_id = ''`,
		},
	},
	{
		// ITEM LIFECYCLE L3: THE DERIVED STEP ROWS, a cache of what the hub reads from git and the room builds (landed,
		// deployed, pushed). Nothing in it is the only copy of anything: a re-run of the derivation rewrites it. Another
		// stage creates the same table, so it is IF NOT EXISTS and the columns are the design's.
		name: "0023_work_step",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS work_step (
				backlog_id TEXT NOT NULL,
				step       TEXT NOT NULL,
				state      TEXT NOT NULL CHECK (state IN ('pending','active','blocked','done','stale','skipped')),
				started_at TEXT NOT NULL DEFAULT '',
				ended_at   TEXT NOT NULL DEFAULT '',
				sha        TEXT NOT NULL DEFAULT '',
				cards      TEXT NOT NULL DEFAULT '[]',
				reason     TEXT NOT NULL DEFAULT '',
				PRIMARY KEY (backlog_id, step)
			)`,
		},
	},
	{
		// THE FILES AN ITEM CHANGED, remembered when they are first read: once the branch is on origin's main its own range
		// is empty, and `deployed` still has to know where the item's code runs. A cache, rewritten by the derivation.
		name: "0024_work_paths",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS work_paths (
				backlog_id TEXT PRIMARY KEY,
				paths      TEXT NOT NULL DEFAULT '[]'
			)`,
		},
	},
	{
		// ITEM LIFECYCLE L2: THE STEP ENGINE'S EVIDENCE, and who a step waits on. `work_fact` is append-only, and the unique
		// index is what makes a replayed approval, or a design noticed twice, a no-op. `work_step` is L3's table, which
		// CREATE IF NOT EXISTS would not give a column, so `party` is added to it here (the runner tolerates it being there).
		name: "0025_work_fact_party",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS work_fact (
				seq        INTEGER PRIMARY KEY,
				backlog_id TEXT NOT NULL,
				kind       TEXT NOT NULL,
				source     TEXT NOT NULL DEFAULT 'seen' CHECK (source IN ('seen','reported')),
				card       TEXT NOT NULL DEFAULT '',
				ref        TEXT NOT NULL DEFAULT '',
				at_sha     TEXT NOT NULL DEFAULT '',
				dirty      INTEGER NOT NULL DEFAULT 0,
				at         TEXT NOT NULL,
				payload    TEXT NOT NULL DEFAULT ''
			)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS work_fact_once ON work_fact (backlog_id, kind, ref, at_sha)`,
			`ALTER TABLE work_step ADD COLUMN party TEXT NOT NULL DEFAULT ''`,
		},
	},
	{
		// ITEM LIFECYCLE L5: WHAT TOKENS COST. `price` is the list price per million tokens of each model, a row per model
		// and the time it took effect, so a repricing is a new row and a recompute, never a data fix. `work_usage` is the
		// rooms' usage rows (and the deliveries the orchestrator spent on an item) as the hub ingested them. THE KEY
		// INCLUDES THE ITEM because one row of a card on two items, or one orchestrator turn about three, is a row for each.
		// A replay of a pull hits the key and adds nothing. No dollar column anywhere: a cost is worked out from the tokens
		// when it is read.
		name: "0026_price_work_usage",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS price (
				model          TEXT NOT NULL,
				valid_from     TEXT NOT NULL,
				input          REAL NOT NULL,
				cache_write_5m REAL NOT NULL,
				cache_write_1h REAL NOT NULL,
				cache_read     REAL NOT NULL,
				output         REAL NOT NULL,
				PRIMARY KEY (model, valid_from)
			)`,
			`INSERT OR IGNORE INTO price (model, valid_from, input, cache_write_5m, cache_write_1h, cache_read, output) VALUES
				('claude-haiku-4-5',  '2026-09-28T00:00:00Z', 1,  1.25,  2,  0.10, 5),
				('claude-sonnet-5',   '2026-09-28T00:00:00Z', 2,  2.5,   4,  0.20, 10),
				('claude-sonnet-5-5', '2026-09-28T00:00:00Z', 2,  2.5,   4,  0.20, 10),
				('claude-opus-5-5',   '2026-09-28T00:00:00Z', 4,  5,     8,  0.20, 20),
				('claude-fable-5-1',  '2026-09-28T00:00:00Z', 10, 12.5,  20, 0.25, 50)`,
			`CREATE TABLE IF NOT EXISTS work_usage (
				room           TEXT NOT NULL,
				usage_id       TEXT NOT NULL,
				backlog_id     TEXT NOT NULL,
				card           TEXT NOT NULL,
				class          TEXT NOT NULL DEFAULT 'step' CHECK (class IN ('step','coord')),
				source         TEXT NOT NULL DEFAULT 'usage' CHECK (source IN ('usage','delivery')),
				model          TEXT NOT NULL DEFAULT '',
				started_at     TEXT NOT NULL,
				ended_at       TEXT NOT NULL,
				input          INTEGER NOT NULL DEFAULT 0,
				cache_write_5m INTEGER NOT NULL DEFAULT 0,
				cache_write_1h INTEGER NOT NULL DEFAULT 0,
				cache_read     INTEGER NOT NULL DEFAULT 0,
				output         INTEGER NOT NULL DEFAULT 0,
				PRIMARY KEY (room, usage_id, backlog_id)
			)`,
			`CREATE INDEX IF NOT EXISTS work_usage_item ON work_usage (backlog_id, ended_at)`,
			`CREATE TABLE IF NOT EXISTS work_usage_cursor (
				room      TEXT PRIMARY KEY,
				since     TEXT NOT NULL,
				pulled_at TEXT NOT NULL
			)`,
		},
	},
	{
		// ITEM LIFECYCLE L4: HOW STRONG A STEP'S EVIDENCE IS. `tag` is `observed` when atrium saw it itself and `reported` when a
		// card said so, empty where the step has no evidence yet. The index finds an item's facts of one kind (its tests, its
		// reviews) without reading the rest.
		name: "0027_work_step_tag",
		stmts: []string{
			`ALTER TABLE work_step ADD COLUMN tag TEXT NOT NULL DEFAULT ''`,
			`CREATE INDEX IF NOT EXISTS work_fact_kind ON work_fact (backlog_id, kind)`,
		},
	},
	{
		// ITEM LIFECYCLE L9: EFFICACY, how well the process worked. `work_event` is what went right or wrong, one row per
		// thing, and THE KEY (work_id, kind, at, sha) IS WHAT MAKES A REPLAYED COLLECT INSERT NOTHING: a send-back noticed
		// twice is one row. `work_outcome` is DERIVED from the events, the steps and the token rows on every tick, never
		// edited by hand and recomputable from scratch, so a changed rule (the 7 days, the noise floor) is a recompute and
		// not a data fix. The cost columns are in 1e-10 dollar and are worked out from the tokens each tick, never kept.
		name: "0028_work_efficacy",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS work_event (
				id      INTEGER PRIMARY KEY,
				work_id TEXT NOT NULL,
				kind    TEXT NOT NULL,
				step    TEXT NOT NULL DEFAULT '',
				at      TEXT NOT NULL,
				sha     TEXT NOT NULL DEFAULT '',
				note    TEXT NOT NULL DEFAULT '',
				source  TEXT NOT NULL DEFAULT 'observed' CHECK (source IN ('observed','reported'))
			)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS work_event_once ON work_event (work_id, kind, at, sha)`,
			`CREATE INDEX IF NOT EXISTS work_event_at ON work_event (at)`,
			`CREATE TABLE IF NOT EXISTS work_outcome (
				work_id            TEXT PRIMARY KEY,
				landed_at          TEXT NOT NULL DEFAULT '',
				clean_at           TEXT NOT NULL DEFAULT '',
				watch_ends_at      TEXT NOT NULL DEFAULT '',
				verdict            TEXT NOT NULL DEFAULT '',
				first_pass         TEXT NOT NULL DEFAULT '',
				first_pass_source  TEXT NOT NULL DEFAULT '',
				escaped_bugs       INTEGER NOT NULL DEFAULT 0,
				reverted           INTEGER NOT NULL DEFAULT 0,
				ci_red_seconds     INTEGER NOT NULL DEFAULT 0,
				wait_human_seconds INTEGER NOT NULL DEFAULT 0,
				work_seconds       INTEGER NOT NULL DEFAULT 0,
				step_cost          INTEGER NOT NULL DEFAULT 0,
				coord_cost         INTEGER NOT NULL DEFAULT 0,
				cost_known         INTEGER NOT NULL DEFAULT 0,
				abandoned_at       TEXT NOT NULL DEFAULT '',
				abandoned_cost     INTEGER NOT NULL DEFAULT 0,
				detail             TEXT NOT NULL DEFAULT '{}',
				computed_at        TEXT NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS work_outcome_landed ON work_outcome (landed_at)`,
		},
	},
	{
		// SHEPHERDS: one row per `atrium shepherd run <item>`, written before each phase so a killed shepherd resumes. The
		// phase is text the shepherd package owns, so a new phase is not a migration. See shepherd.go.
		name: "0029_shepherd",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS shepherd (
				item_id    TEXT PRIMARY KEY,
				phase      TEXT NOT NULL,
				room       TEXT NOT NULL DEFAULT '',
				base_sha   TEXT NOT NULL DEFAULT '',
				worktree   TEXT NOT NULL DEFAULT '',
				branch     TEXT NOT NULL DEFAULT '',
				card       TEXT NOT NULL DEFAULT '',
				work_item  TEXT NOT NULL DEFAULT '',
				ref        TEXT NOT NULL DEFAULT '',
				stage      TEXT NOT NULL DEFAULT '',
				packet     TEXT NOT NULL DEFAULT '',
				last_error TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
		},
	},
	{
		// CI RUNS THE HUB HEARD ABOUT, and one delivery per target. Never the request body. See cihook.go.
		name: "0030_ci_hook",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS ci_run (
				id          TEXT PRIMARY KEY,
				repo        TEXT NOT NULL,
				workflow    TEXT NOT NULL DEFAULT '',
				branch      TEXT NOT NULL DEFAULT '',
				sha         TEXT NOT NULL DEFAULT '',
				run_id      INTEGER NOT NULL,
				attempt     INTEGER NOT NULL,
				conclusion  TEXT NOT NULL,
				url         TEXT NOT NULL DEFAULT '',
				auth        TEXT NOT NULL DEFAULT '',
				received_at TEXT NOT NULL,
				UNIQUE (repo, run_id, attempt)
			)`,
			`CREATE INDEX IF NOT EXISTS ci_run_stream ON ci_run (repo, workflow, branch, received_at)`,
			`CREATE TABLE IF NOT EXISTS ci_delivery (
				id         TEXT PRIMARY KEY,
				run_ref    TEXT NOT NULL,
				target     TEXT NOT NULL,
				kind       TEXT NOT NULL DEFAULT '',
				wake       INTEGER NOT NULL DEFAULT 0,
				text       TEXT NOT NULL,
				state      TEXT NOT NULL DEFAULT 'pending'
				             CHECK (state IN ('pending','delivered','unconfirmed','expired','refused','folded')),
				answer     TEXT NOT NULL DEFAULT '',
				card       TEXT NOT NULL DEFAULT '',
				attempts   INTEGER NOT NULL DEFAULT 0,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS ci_delivery_state ON ci_delivery (state)`,
			`CREATE INDEX IF NOT EXISTS ci_delivery_run ON ci_delivery (run_ref)`,
		},
	},
	{
		// A GROWLER MAY BE ABOUT A CI POST. The reason CHECK gains 'ci', and SQLite cannot change one in place, so growl is
		// rebuilt as 0014 did backlog_item. Nothing references growl.
		name: "0031_growl_ci",
		stmts: []string{
			`DROP TABLE IF EXISTS growl_rebuilt`,
			`CREATE TABLE growl_rebuilt (
				id          TEXT PRIMARY KEY,
				room_id     TEXT NOT NULL,
				card_id     TEXT NOT NULL DEFAULT '',
				reason      TEXT NOT NULL CHECK (reason IN ('permission','halt','blocked','question','deploy-hold','ci')),
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
			`INSERT INTO growl_rebuilt (id, room_id, card_id, reason, title, body, subject, raised_at, state, until, reminders,
					changed_at, changed_via, changed_tab, ended_at)
				SELECT id, room_id, card_id, reason, title, body, subject, raised_at, state, until, reminders,
					changed_at, changed_via, changed_tab, ended_at
				FROM growl`,
			`DROP TABLE growl`,
			`ALTER TABLE growl_rebuilt RENAME TO growl`,
			`CREATE INDEX IF NOT EXISTS growl_room ON growl (room_id, reason, ended_at)`,
			`CREATE INDEX IF NOT EXISTS growl_state ON growl (state)`,
		},
	},
	{
		// ITEM TASKS: a task belongs to one item, with a brief, a state and a result. `work_task` is the cache of the
		// task facts in work_fact. The two columns on `work` are the owner's context every task's brief carries and how
		// many tasks may run at once. See task.go and docs/runtime/item-tasks-design.md, sections 1 and 8.
		name: "0032_work_task",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS work_task (
				backlog_id TEXT NOT NULL,
				task_id    TEXT NOT NULL,
				title      TEXT NOT NULL,
				brief      TEXT NOT NULL,
				kind       TEXT NOT NULL DEFAULT 'scratch' CHECK (kind IN ('scratch','code')),
				repo       TEXT NOT NULL DEFAULT '',
				base       TEXT NOT NULL DEFAULT '',
				after      TEXT NOT NULL DEFAULT '[]',
				outputs    TEXT NOT NULL DEFAULT '[]',
				room       TEXT NOT NULL DEFAULT '',
				model      TEXT NOT NULL DEFAULT '',
				state      TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','held','running','capturing',
				             'captured','capture-failed','failed','unconfirmed','cancelled')),
				attempt    INTEGER NOT NULL DEFAULT 0,
				card       TEXT NOT NULL DEFAULT '',
				folder     TEXT NOT NULL DEFAULT '',
				branch     TEXT NOT NULL DEFAULT '',
				sha        TEXT NOT NULL DEFAULT '',
				diffstat   TEXT NOT NULL DEFAULT '',
				line       TEXT NOT NULL DEFAULT '',
				why        TEXT NOT NULL DEFAULT '',
				learned    TEXT NOT NULL DEFAULT '',
				recap      TEXT NOT NULL DEFAULT '',
				files      TEXT NOT NULL DEFAULT '[]',
				reason     TEXT NOT NULL DEFAULT '',
				created_by TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				started_at TEXT NOT NULL DEFAULT '',
				ended_at   TEXT NOT NULL DEFAULT '',
				PRIMARY KEY (backlog_id, task_id)
			)`,
			`CREATE INDEX IF NOT EXISTS work_task_state ON work_task (backlog_id, state)`,
			`ALTER TABLE work ADD COLUMN task_context TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE work ADD COLUMN task_parallel INTEGER NOT NULL DEFAULT 2`,
		},
	},
	{
		// HUB EVENTS, WHO SUBSCRIBED, AND ONE DELIVERY PER SUBSCRIPTION. Never the request body. See events.go.
		name: "0033_hub_events",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS hub_event (
				id          TEXT PRIMARY KEY,
				kind        TEXT NOT NULL,
				repo        TEXT NOT NULL DEFAULT '',
				branch      TEXT NOT NULL DEFAULT '',
				outcome     TEXT NOT NULL DEFAULT '',
				from_sha    TEXT NOT NULL DEFAULT '',
				to_sha      TEXT NOT NULL DEFAULT '',
				commits     INTEGER NOT NULL DEFAULT 0,
				reason      TEXT NOT NULL DEFAULT '',
				source      TEXT NOT NULL DEFAULT '',
				received_at TEXT NOT NULL,
				UNIQUE (kind, repo, branch, outcome, from_sha, to_sha)
			)`,
			`CREATE TABLE IF NOT EXISTS event_sub (
				id         TEXT PRIMARY KEY,
				kind       TEXT NOT NULL,
				repo       TEXT NOT NULL DEFAULT '',
				branch     TEXT NOT NULL DEFAULT '',
				card       TEXT NOT NULL,
				wake       INTEGER NOT NULL DEFAULT 0,
				created_by TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				UNIQUE (kind, repo, branch, card)
			)`,
			`CREATE INDEX IF NOT EXISTS event_sub_card ON event_sub (card)`,
			`CREATE TABLE IF NOT EXISTS event_delivery (
				id         TEXT PRIMARY KEY,
				event_ref  TEXT NOT NULL,
				target     TEXT NOT NULL,
				kind       TEXT NOT NULL DEFAULT '',
				wake       INTEGER NOT NULL DEFAULT 0,
				text       TEXT NOT NULL,
				state      TEXT NOT NULL DEFAULT 'pending'
				             CHECK (state IN ('pending','delivered','unconfirmed','expired','refused','folded')),
				answer     TEXT NOT NULL DEFAULT '',
				card       TEXT NOT NULL DEFAULT '',
				attempts   INTEGER NOT NULL DEFAULT 0,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS event_delivery_state ON event_delivery (state)`,
			`CREATE INDEX IF NOT EXISTS event_delivery_event ON event_delivery (event_ref)`,
		},
	},
	{
		// THE MERGE QUEUE. A request is never deleted, so the table is the history. See merge.go and
		// docs/rnd/merge-queue.md.
		name: "0034_merge_queue",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS merge_request (
				n              INTEGER PRIMARY KEY,
				repo           TEXT NOT NULL,
				branch         TEXT NOT NULL,
				sha            TEXT NOT NULL,
				requester      TEXT NOT NULL DEFAULT '',
				requester_room TEXT NOT NULL DEFAULT '',
				launcher       TEXT NOT NULL DEFAULT '',
				launcher_room  TEXT NOT NULL DEFAULT '',
				state          TEXT NOT NULL DEFAULT 'queued'
				                 CHECK (state IN ('queued','merging','testing','conflict','landed','red','refused','cancelled')),
				head           INTEGER NOT NULL DEFAULT 0,
				continues      INTEGER NOT NULL DEFAULT 0,
				retries        INTEGER NOT NULL DEFAULT 0,
				base           TEXT NOT NULL DEFAULT '',
				candidate      TEXT NOT NULL DEFAULT '',
				landed         TEXT NOT NULL DEFAULT '',
				room           TEXT NOT NULL DEFAULT '',
				merger         TEXT NOT NULL DEFAULT '',
				reason         TEXT NOT NULL DEFAULT '',
				failing        TEXT NOT NULL DEFAULT '',
				output         TEXT NOT NULL DEFAULT '',
				land_n         INTEGER NOT NULL DEFAULT 0,
				asked_at       TEXT NOT NULL,
				started_at     TEXT NOT NULL DEFAULT '',
				tested_at      TEXT NOT NULL DEFAULT '',
				ended_at       TEXT NOT NULL DEFAULT ''
			)`,
			`CREATE INDEX IF NOT EXISTS merge_request_open ON merge_request (repo, state)`,
			`CREATE INDEX IF NOT EXISTS merge_request_branch ON merge_request (repo, branch)`,
			`CREATE TABLE IF NOT EXISTS merge_land (
				n            INTEGER PRIMARY KEY,
				repo         TEXT NOT NULL,
				commit_sha   TEXT NOT NULL,
				main_before  TEXT NOT NULL,
				claude_before TEXT NOT NULL,
				requests     TEXT NOT NULL DEFAULT '',
				by           TEXT NOT NULL DEFAULT '',
				at           TEXT NOT NULL
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
			// OR IGNORE: another hub opening this file at the same moment may have recorded it.
			`INSERT OR IGNORE INTO schema_migration (name, applied_at) VALUES (?, ?)`,
			m.name, ts(s.now())); err != nil {
			tx.Rollback()
			return fmt.Errorf("record %s: %w", m.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", m.name, err)
		}
	}
	return nil
}
