# internal/store

SQLite. Every table, every query, and the halt. Nothing else in atrium talks to the database.

## Migrations: read this before touching schema.go

Migrations are applied in slice order and recorded by name. **A migration already recorded will never run
again**, so editing one is the same as not shipping it. Two rules follow:

- **Add at the END of the slice.** Adding in the middle means an existing database silently skips it.
- **Write every statement to tolerate already being there.** `CREATE TABLE IF NOT EXISTS`, and the runner
  swallows "duplicate column name" for `ADD COLUMN`, which SQLite has no `IF NOT EXISTS` for.

Both rules exist because they were broken. Adding `perm_rule` to `0001` meant every existing database had
`0001` recorded, never created the table, and halted on the first rule lookup.

**Changing a `UNIQUE` constraint means rebuilding the table.** SQLite cannot alter one in place. See `0010` and
`0014` for the pattern.

**Widening a `CHECK` costs a table rebuild too, and the event table is the largest one here.** `0027` did it to
add one event kind and says at length that this is "a fair price once and a bad habit". Before adding a kind,
ask whether the daemon's own log would do: a shell opening is a fact about a process, not about what a session
was allowed to do.

**Verify a migration against a COPY of the live database, never the real one.** Copy it out, open the copy,
count what survived, delete the copy.

## The halt

Storage failure is not a degraded mode. Open or migration failure means the daemon refuses to start. A later
failure closes the agent listener and leaves it closed, so runners see connection-refused and park on the
backoff they already have, burning nothing. The process stays alive and the board reports the cause.

Every call returns `ErrHalted` once halted. Running without durable state is worse than not running.

## Portability

The schema is written to stay Postgres portable: text ULID-ish keys, RFC3339 text timestamps, `CHECK` instead
of enums, TEXT instead of JSONB, `?` placeholders. Nothing has ever run it against Postgres, and that is a
hedge rather than a commitment.

## What is stored and what is not

**Stored:** cards, their event log, permissions and their decisions, standing rules, harness rows, fixtures,
sources, actions, messages, recaps, settings.

**Never stored:** what a runner is doing right now. It would be a lie the moment the daemon restarted. See
`docs/runtime/activity-design.md`.

## The files

| | |
| --- | --- |
| `schema.go` | migrations, applied in slice order. Read the header before editing |
| `tasks.go` | cards, their columns, and the event log |
| `permissions.go` / `rules.go` | what was asked, what was decided, and the standing answers |
| `harness.go` | what atrium knows how to start |
| `fixtures.go` | what starts with the daemon, and adopting a card already there |
| `settings.go` | daemon-wide settings, keyed by name |
| `intake.go` | the inbox. Its dedup key is `source` plus `external_id`, decided in `docs/runtime/intake-design.md` |
| `sources.go` | a command on a timer, shaped like harness because it is the same idea |
| `actions.go` | named prompts the operator wrote, and which card each belongs on |
| `recap.go` | what a session says it did, bounded |
| `cardhome.go` | the folders a task of an item ran in, one ledger row each, so only atrium removes one |
| `history.go` | everything ever run here, cut by whether it was written up |
