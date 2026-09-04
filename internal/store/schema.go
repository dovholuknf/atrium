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
	{
		// Things to say to a running session. There is no channel into a
		// claude session from outside, so a message waits here until a hook
		// fires and can carry it back to the model.
		name: "0012_message",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS message (
				id           TEXT PRIMARY KEY,
				task_id      TEXT NOT NULL REFERENCES task(id) ON DELETE CASCADE,
				text         TEXT NOT NULL,
				created_at   TEXT NOT NULL,
				delivered_at TEXT,
				via          TEXT NOT NULL DEFAULT ''
			)`,
			`CREATE INDEX IF NOT EXISTS message_pending ON message (task_id, delivered_at)`,
		},
	},
	{
		// Auto mode: approve without asking, still routed through atrium so
		// everything is written down.
		//
		// Per task, so one session is trusted for a stretch while the others
		// are not. Durable: losing it on a restart would start interrupting
		// again with no sign of why.
		name: "0013_task_auto_approve",
		stmts: []string{
			`ALTER TABLE task ADD COLUMN auto_approve INTEGER NOT NULL DEFAULT 0`,
		},
	},
	{
		// Rules can now cover a directory instead of a command shape.
		//
		// "Let it work anywhere under this folder" was possible before only by
		// hand-writing a glob that also had to account for the quoting around
		// the path, which nobody gets right. A path rule says what it means.
		//
		// Existing rules default to command, which is what they all were.
		// Rules can cover a directory instead of a command shape, so the
		// uniqueness constraint has to include the kind: the same text is both
		// a valid command pattern and a valid directory, answering different
		// requests, and one must not overwrite the other.
		//
		// A rebuild rather than an ALTER: SQLite cannot change a UNIQUE
		// constraint in place. Same as 0010.
		//
		// The column add is a separate statement so a database that ran it in
		// an earlier build is not asked twice. The runner tolerates a duplicate
		// column.
		name: "0014_rule_kind",
		stmts: []string{
			`ALTER TABLE perm_rule ADD COLUMN kind TEXT NOT NULL DEFAULT 'command'`,
			`CREATE TABLE perm_rule_rebuilt (
				id         TEXT PRIMARY KEY,
				tool       TEXT NOT NULL,
				prefix     TEXT NOT NULL,
				decision   TEXT NOT NULL CHECK (decision IN ('approve','block')),
				reason     TEXT NOT NULL DEFAULT '',
				scope      TEXT NOT NULL DEFAULT '',
				kind       TEXT NOT NULL DEFAULT 'command'
				             CHECK (kind IN ('command','path')),
				created_at TEXT NOT NULL,
				hits       INTEGER NOT NULL DEFAULT 0,
				UNIQUE (tool, prefix, scope, kind)
			)`,
			`INSERT INTO perm_rule_rebuilt
				(id, tool, prefix, decision, reason, scope, kind, created_at, hits)
			 SELECT id, tool, prefix, decision, reason, scope,
				CASE WHEN kind = '' THEN 'command' ELSE kind END, created_at, hits
			 FROM perm_rule`,
			`DROP TABLE perm_rule`,
			`ALTER TABLE perm_rule_rebuilt RENAME TO perm_rule`,
		},
	},
	{
		// Atrium owns every runner it starts.
		//
		// Window mode hands the session to a terminal that then owns itself,
		// which costs attach, terminate and liveness: atrium never learns the
		// runner's process id, so the card cannot be stopped, watched or
		// checked. It was the seeded default before pty mode worked, so rows
		// created then are still on it.
		//
		// The column stays, and a row can be set back by hand for a runner
		// that insists on its own window.
		name: "0015_pty_by_default",
		stmts: []string{
			`UPDATE harness SET launch_mode = 'pty' WHERE launch_mode = 'window'`,
		},
	},
	{
		// How to ask a runner to exit, per runner.
		//
		// There is no common answer. A shell takes `exit` and a newline. Claude
		// takes control-d twice. Ollama and codex take it once. Sending the
		// wrong one leaves the process sitting there until the terminal is
		// closed underneath it, which is the thing an exit button exists to
		// avoid.
		//
		// Stored as tokens rather than bytes so the field is writable by hand:
		// `ctrl-d`, `enter`, or any literal text to type.
		name: "0016_harness_exit_keys",
		stmts: []string{
			`ALTER TABLE harness ADD COLUMN exit_keys TEXT NOT NULL DEFAULT '[]'`,
			`UPDATE harness SET exit_keys = '["ctrl-d","ctrl-d"]' WHERE id = 'claude'`,
			`UPDATE harness SET exit_keys = '["ctrl-d"]' WHERE id IN ('ollama','codex')`,
			`UPDATE harness SET exit_keys = '["exit","enter"]' WHERE id = 'shell'`,
		},
	},
	{
		// A command to run before the runner, whose environment the runner
		// inherits.
		//
		// The habit this replaces: open a terminal, run a shell function that
		// puts some toolchain on PATH, then start an agent from that shell so
		// it can see them. That works and cannot be done from a board.
		name: "0017_harness_prepare",
		stmts: []string{
			`ALTER TABLE harness ADD COLUMN prepare TEXT NOT NULL DEFAULT ''`,
		},
	},
	{
		// Settings that belong to the daemon rather than to any one card.
		//
		// A table rather than a column somewhere, because the first of these
		// is global auto mode and there will be more. Keys are strings and so
		// are values: nothing here is worth a schema change to add.
		name: "0018_setting",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS setting (
				key        TEXT PRIMARY KEY,
				value      TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
		},
	},
	{
		// What the operator calls a card, and whether it is a fixture.
		//
		// Tags are a JSON array in a text column rather than a table. The
		// board already holds every card in memory to group and sort them, so
		// a join would buy nothing, and this keeps the shape Postgres portable
		// the same way `overrides` already is.
		name: "0019_task_tags_pinned",
		stmts: []string{
			`ALTER TABLE task ADD COLUMN tags TEXT NOT NULL DEFAULT '[]'`,
			`ALTER TABLE task ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0`,
		},
	},
	{
		// Terminals that come up with the daemon.
		//
		// A row per fixture rather than a JSON blob in settings, because these
		// are ordered, individually enabled, and edited one at a time. `sort`
		// is what "the dotfiles one is always first" means.
		//
		// task_id is a soft reference on purpose, with no foreign key. A
		// fixture describes what to start; the card it started last time may
		// have been swept, and that must not delete the fixture.
		name: "0020_fixture",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS fixture (
				id         TEXT PRIMARY KEY,
				label      TEXT NOT NULL DEFAULT '',
				harness    TEXT NOT NULL,
				cwd        TEXT NOT NULL DEFAULT '',
				resume     INTEGER NOT NULL DEFAULT 1,
				enabled    INTEGER NOT NULL DEFAULT 1,
				sort       REAL NOT NULL DEFAULT 0,
				theme      TEXT NOT NULL DEFAULT '',
				task_id    TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL
			)`,
		},
	},
	{
		// What a terminal looks like. Held on the card so a session keeps its
		// colors across a restart, which is the whole reason the operator
		// colors terminals in the first place: telling them apart at a
		// glance, permanently.
		name: "0021_task_theme",
		stmts: []string{
			`ALTER TABLE task ADD COLUMN theme TEXT NOT NULL DEFAULT ''`,
		},
	},
	{
		// Which tone this card rings with. Stored on the card rather than in
		// the browser for the same reason its theme is: a session you know by
		// its sound has to sound the same in another browser and after a
		// restart, and the whole point is telling agents apart without
		// looking. Empty means the board-wide default for that kind of alert.
		name: "0022_task_sound",
		stmts: []string{
			`ALTER TABLE task ADD COLUMN sound TEXT NOT NULL DEFAULT ''`,
		},
	},
	{
		// Off the board, still on the record.
		//
		// A dead card is swept so the finished column does not fill up all
		// day. Deleting it is the obvious way to do that and the wrong one:
		// the card and its whole audit log are the only account of what that
		// session ran and what it was allowed to do, and a board that throws
		// that away a minute after a session ends cannot answer "what have I
		// had running this week".
		//
		// Archiving separates the two questions. The board asks "what wants my
		// attention", and archived cards are not that. The history asks "what
		// has ever run here", and nothing has been lost to answer it.
		//
		// Indexed because every list query now excludes archived rows, which
		// on a machine that has been running for months is most of them.
		name: "0023_task_archived",
		stmts: []string{
			`ALTER TABLE task ADD COLUMN archived_at TEXT NOT NULL DEFAULT ''`,
			`CREATE INDEX IF NOT EXISTS idx_task_archived ON task(archived_at)`,
		},
	},
	{
		// Where this work came from, when it came from somewhere.
		//
		// `external_id` has existed since 0005 and is written by nothing. It
		// survived the abandoned ledger adoption and it is exactly the right
		// column for "the identifier the system this came from already uses",
		// so it gets used rather than replaced.
		//
		// `source` is what that identifier means to whoever issued it, and
		// atrium never interprets it: `github`, `zendesk`, `ci` are strings
		// that become a badge. `url` is the way back to the thing itself,
		// which is the entire reverse direction worth building. See
		// docs/intake-design.md.
		//
		// The index is on the pair, because the pair is what deduplicates: the
		// same issue number means different work in two different trackers.
		//
		// `prompt_args` is on the harness for the same reason `resume_args`
		// is. There is no common way to hand a runner an opening instruction:
		// claude and codex take it as a bare argument and a shell would
		// try to execute it. A runner with nothing here cannot be given one,
		// and says so instead of being started with a prompt it will not read.
		name: "0024_intake_origin",
		stmts: []string{
			`ALTER TABLE task ADD COLUMN source TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE task ADD COLUMN url TEXT NOT NULL DEFAULT ''`,
			`CREATE INDEX IF NOT EXISTS idx_task_source_external ON task(source, external_id)`,
			`ALTER TABLE harness ADD COLUMN prompt_args TEXT NOT NULL DEFAULT '[]'`,
			// Seeding only reaches a row that does not exist yet, so an
			// existing database would keep an empty list forever. Guarded on
			// the empty list so an operator who has already written their own
			// is not overwritten by a later run.
			`UPDATE harness SET prompt_args = '["{prompt}"]'
			   WHERE id IN ('claude','codex') AND (prompt_args = '[]' OR prompt_args = '')`,
		},
	},
	{
		// An inbox: work that is real and has no session yet.
		//
		// No new status. `backlog` has been in the CHECK since 0001, has a
		// constant, and nothing has ever created a card in it, which is what
		// the board says in the comment above COLUMNS. An offered item is
		// exactly what that status was named for: on the board, not started.
		//
		// docs/intake-design.md argued for a new `offered` status and
		// enumerated the six it was not, skipping this one. Following that
		// would have meant rebuilding `task` to change a CHECK constraint, and
		// `task` is the parent of four ON DELETE CASCADE relationships. With
		// foreign keys on, DROP TABLE performs the implicit delete and fires
		// those cascades, so the rebuild pattern 0010 and 0014 use on child
		// tables would have taken every event, permission, message and launch
		// spec in the database with it. An unused status that already exists
		// is a better answer than a correct-looking migration with that in it.
		//
		// `prompt` is the instruction the card was raised with, held until
		// somebody presses start, because an offered card has no runner to
		// hand it to yet. Kept after the start rather than cleared: it is what
		// this card is for, and a start that failed should be repeatable.
		// `worktree` carries the suggested directory, which may not exist yet.
		//
		// `intake_key` is the deduplication key, and it is a separate column
		// rather than a unique index over (source, external_id) so that
		// uniqueness applies to intake and not to launching. Two deliberate
		// launches naming the same ticket are two pieces of work a human asked
		// for twice; two poll ticks reporting the same ticket are one. Only
		// intake writes this. Same shape and same reason as
		// permission.dedup_key, which 0001 already has.
		//
		// Partial so that everything not raised by intake shares the empty
		// string without colliding. SQLite and Postgres both take a partial
		// unique index, which the header of this file asks of every migration.
		name: "0025_intake",
		stmts: []string{
			`ALTER TABLE task ADD COLUMN prompt TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE task ADD COLUMN intake_key TEXT NOT NULL DEFAULT ''`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_task_intake_key
			   ON task(intake_key) WHERE intake_key <> ''`,
		},
	},
	{
		// A source is a command on a timer whose stdout is intake items.
		//
		// Shaped like `harness` on purpose, and for the same reason. A harness
		// row says how to start a runner without atrium knowing what claude
		// is. A source row says how to find work without atrium knowing what
		// GitHub is. Atrium holds an argv and an interval; `gh` holds the
		// token, in the keyring it already uses.
		//
		// That is the rule docs/intake-design.md states and this is what
		// enforces it: there is nowhere in this table to put a credential.
		//
		// The failure bookkeeping is three columns rather than a log, because
		// what the operator needs is on the row: when it last ran, what it
		// said when it broke, and how many times in a row. A source that has
		// failed three times running is switched off with the reason still
		// attached, since a source retrying forever against a script somebody
		// deleted is a daemon spawning a process every fifteen minutes to
		// produce the same error nobody is reading.
		name: "0026_source",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS source (
				id            TEXT PRIMARY KEY,
				label         TEXT NOT NULL DEFAULT '',
				enabled       INTEGER NOT NULL DEFAULT 0,
				cmd           TEXT NOT NULL,
				args          TEXT NOT NULL DEFAULT '[]',
				cwd           TEXT NOT NULL DEFAULT '',
				interval_secs INTEGER NOT NULL DEFAULT 900,
				last_run_at   TEXT NOT NULL DEFAULT '',
				last_error    TEXT NOT NULL DEFAULT '',
				last_count    INTEGER NOT NULL DEFAULT 0,
				failures      INTEGER NOT NULL DEFAULT 0,
				notes         TEXT NOT NULL DEFAULT '',
				created_at    TEXT NOT NULL
			)`,
		},
	},
	{
		// A session compacting is a thing that happened to it.
		//
		// Compaction is the moment a session forgets, and nothing on a card
		// recorded it. It answers a question that comes up by itself: why did
		// this agent stop knowing something it clearly knew an hour ago.
		//
		// A timeline event and not a status. It is a moment rather than a
		// state, so there is nothing for a card to sit in and nothing for a
		// lane to hold.
		//
		// This means rebuilding `event` to widen a CHECK, which SQLite cannot
		// alter in place. Safe here in a way it would not be on `task`:
		// `event` is a CHILD, nothing references it, so DROP TABLE cascades to
		// nothing. Same pattern and same reasoning as 0010.
		//
		// Worth knowing before adding the next kind: this constraint guards
		// against a typo in atrium's own code, since no caller ever supplies a
		// kind, and extending it costs a rebuild of the largest table in the
		// database. That is a fair price once and a bad habit.
		name: "0027_event_compacted",
		stmts: []string{
			`CREATE TABLE event_rebuilt (
				id      TEXT PRIMARY KEY,
				task_id TEXT NOT NULL REFERENCES task(id) ON DELETE CASCADE,
				at      TEXT NOT NULL,
				kind    TEXT NOT NULL
				          CHECK (kind IN ('created','submitted','prompted','perm-requested','perm-decided',
				                          'status-changed','output','notified','launched','exited',
				                          'compacted')),
				payload TEXT NOT NULL DEFAULT '{}'
			)`,
			`INSERT INTO event_rebuilt (id, task_id, at, kind, payload)
			 SELECT id, task_id, at, kind, payload FROM event`,
			`DROP TABLE event`,
			`ALTER TABLE event_rebuilt RENAME TO event`,
			`CREATE INDEX IF NOT EXISTS event_task_at ON event (task_id, at)`,
		},
	},
	{
		// Auto mode until a time, rather than until somebody remembers.
		//
		// "For the next hour" is the shape a temporary switch wants, and the
		// only reminder it had was a badge on a card.
		//
		// The deadline is checked when the permission chain runs and not by a
		// timer that turns it off. A timer that has to fire is a timer that
		// does not fire across a restart, and auto mode surviving a restart it
		// should not have survived is the failure that matters here. Reading
		// the clock at the moment of the decision cannot get that wrong.
		//
		// Empty means the switch has no deadline, which is what it did before
		// and still the default for turning it on by hand.
		name: "0028_auto_until",
		stmts: []string{
			`ALTER TABLE task ADD COLUMN auto_until TEXT NOT NULL DEFAULT ''`,
		},
	},
	{
		// What the session says it did, in its own words.
		//
		// This closes the largest hole in what atrium does. Everything an
		// agent reported landed in `needs-input`, so the board could not tell
		// "finished, go and look at the result" from "stuck, answer me", and
		// only a human moving a card by hand ever produced `done`. Sorting
		// those two apart afterwards means reading each one, which is the
		// thing a board exists to avoid.
		//
		// A recap is not a transcript and must not become one. It is the two
		// or three sentences a session would say if you asked it what it just
		// did, written once at the end, by the only party that knows.
		//
		// Bounded when it is written rather than here, because a TEXT column
		// with no limit is how a card ends up holding a diff.
		name: "0029_task_recap",
		stmts: []string{
			`ALTER TABLE task ADD COLUMN recap TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE task ADD COLUMN recap_at TEXT NOT NULL DEFAULT ''`,
		},
	},
	{
		// Things you say to an agent often enough to have got tired of typing.
		//
		// A card can be terminated, shelved, attached to and messaged, and all
		// of those are things ATRIUM does. None of them is the thing the
		// operator does repeatedly, which is send the same instruction to
		// whichever agent is in front of them: run the tests, write it up,
		// commit what you have.
		//
		// `after` is why this is not a saved snippet. `keep` leaves the
		// session up for whatever comes next; `exit` sends the prompt and then
		// the harness's own exit keys, which is the "write it up and go away"
		// case and the reason this needed building at all.
		//
		// Stored daemon side, which makes it the first operator-authored
		// content atrium keeps and hands back. The grouping-expression entry
		// in docs/backlog.md is about what that costs, and the answer here is
		// that these are different in kind: a grouping expression is CODE
		// evaluated in a browser with full page scope, and an action is TEXT
		// delivered to a runner. Storing the second one centrally does not
		// create the stored-XSS shape the first one would.
		name: "0030_card_action",
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS card_action (
				id         TEXT PRIMARY KEY,
				label      TEXT NOT NULL,
				prompt     TEXT NOT NULL,
				after      TEXT NOT NULL DEFAULT 'keep'
				             CHECK (after IN ('keep','exit')),
				tag        TEXT NOT NULL DEFAULT '',
				runner     TEXT NOT NULL DEFAULT '',
				enabled    INTEGER NOT NULL DEFAULT 1,
				sort       INTEGER NOT NULL DEFAULT 0,
				created_at TEXT NOT NULL
			)`,
		},
	},
	{
		// A note is for YOU. Sending it is a second, separate act.
		//
		// The message queue already exists and does the opposite thing: it
		// fires as soon as there is anything to deliver, reaching the session
		// on its next tool call. A note is written now and sent when you say.
		//
		// What that buys is ordering. Three things thought of during a long
		// turn, sent as one instruction at the end, rather than three
		// interruptions in the middle. Claude Code takes input while it is
		// thinking, so the send is less urgent than it once was, and the
		// ordering is the part that still matters.
		//
		// One column rather than a note table, because the thing being asked
		// for is a scratch pad per card and not a list with its own lifecycle.
		// It accumulates while you type and empties when you send.
		name: "0031_task_note",
		stmts: []string{
			`ALTER TABLE task ADD COLUMN note TEXT NOT NULL DEFAULT ''`,
			// Who a message is from.
			//
			// Empty means the operator, which is what every message written
			// before this was, and what `messageBanner` still says by default.
			//
			// Added HERE rather than when the peer bus needs it, because both
			// touch this one table and shipping two migrations against it two
			// patches apart is how a column ends up meaning slightly different
			// things depending on when the row was written.
			//
			// The banner has to be able to say who, and it has to say that it
			// was not the human, or a model reads a peer's request as an
			// instruction from you and acts on it with that authority.
			`ALTER TABLE message ADD COLUMN from_peer TEXT NOT NULL DEFAULT ''`,
		},
	},
	{
		// Why a card is waiting, which is not the same question as how long.
		//
		// `needs-input` is reached two ways and they mean opposite things. A
		// session that has just started is ready because it has done nothing
		// yet; a session that has finished a turn is ready because it has
		// done what you asked. The board said "finished its turn and wants
		// your next instruction" for both, so starting a session announced
		// that it had completed work it had not begun.
		//
		// A column rather than a read of the event log. The waiting list is
		// polled every five seconds and the answer is one fact per card, so
		// asking `event` for the last thing that happened before each status
		// change is a query per card per poll to learn something the status
		// change already knew.
		//
		// Empty means a turn ended, which is what every row written before
		// this was, and what the board still says by default.
		name: "0032_task_waiting_reason",
		stmts: []string{
			`ALTER TABLE task ADD COLUMN waiting_reason TEXT NOT NULL DEFAULT ''`,
		},
	},
	{
		// The mark a card wears on a desktop notification.
		//
		// Same argument as `theme` and `sound`, and it belongs beside them:
		// telling sessions apart without reading only works if the answer is
		// the same tomorrow and in another browser. A notification arrives
		// with the operating system's own chrome around it and one small
		// image, and until now every one of them carried the same A.
		//
		// Free text rather than a name from a list, holding whatever renders
		// in one glyph: a letter, a digit, an emoji. A fixed set would be
		// atrium deciding what a project can look like, which is the same
		// mistake `tags` already refuses to make. Bounded on the way in, and
		// drawn to a canvas, so nothing here is ever interpreted as markup.
		//
		// Empty means the atrium mark, which is what every card has until it
		// is given one.
		name: "0033_task_icon",
		stmts: []string{
			`ALTER TABLE task ADD COLUMN icon TEXT NOT NULL DEFAULT ''`,
		},
	},
	{
		// What happened the last time a fixture was asked to start.
		//
		// Fixtures start in the background so a slow runner cannot hold the
		// board up, which means a failure had nowhere to go but the daemon's
		// log. A fixture pointing at a directory that no longer exists was
		// therefore silent: the terminal you expect every morning is simply
		// not there, and finding out why meant reading stdout of a process
		// you did not start in a window you do not have.
		//
		// The same shape `source` already uses for the same reason: the row
		// that failed carries its own reason, so the page listing them is
		// also the page that answers why one is missing.
		name: "0034_fixture_last_run",
		stmts: []string{
			`ALTER TABLE fixture ADD COLUMN last_error TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE fixture ADD COLUMN last_run_at TEXT NOT NULL DEFAULT ''`,
		},
	},
}

// migrate applies any migration not already recorded. This runs before the
// store can halt, so failures return an error and the daemon refuses to start.
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
