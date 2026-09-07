# Postgres: what actually broke

The schema has carried a claim since `0001`: written to stay Postgres portable, text ULID-ish keys, RFC3339
text timestamps, `CHECK` instead of enums, TEXT instead of JSONB, `?` placeholders. Nothing had ever run it
there. This is the result of running it there.

**This is a probe and not a port.** Nothing in `internal/store` changed, no driver was added, and atrium still
speaks to SQLite and only SQLite. What follows is a list of what a port would hit, in the order it would hit
it, with the error text.

## Verdict in one paragraph

The DDL is portable and the claim holds: all 80 statements across all 38 migrations apply to PostgreSQL 17
unmodified and in order, with zero errors. Every one of the 82 static queries and all nine runtime-assembled
ones parse and type-check once `?` becomes `$n`. What does NOT survive is everything AROUND the SQL. The
migration runner's tolerance rule is written against a SQLite error string and cannot work inside a Postgres
transaction at all. The store's failure posture treats every non-lock error as permanent, which is right for a
file and wrong for a socket. Four writers depend on `SetMaxOpenConns(1)` for correctness rather than for
throughput, and say so in comments that would become false. The port is not the schema. The port is the halt,
the runner, and the four read-then-write pairs.

## How this was run

PostgreSQL 17.11 in Docker, `postgres:17-alpine`, database collation `en_US.utf8`, torn down afterwards.

The migration statements and the query strings were lifted straight out of the Go source by parsing it, rather
than retyped, so what Postgres saw is what the code sends. Two throwaway programs did that, both outside the
atrium module. The queries were fed to Postgres as `PREPARE`, which parses, resolves every column against the
schema the migrations just built, and infers a type for every parameter, without running anything.

The live database was inspected as a COPY, per `internal/store/CLAUDE.md`, and the copy was deleted.

## 1. The schema

### It applies. All of it.

38 migrations, 80 statements, applied in slice order against an empty Postgres 17 database:

```
--- errors: 0
```

Nothing needed changing. `CHECK` constraints, partial unique indexes (`idx_task_intake_key`, which `0025` notes
both engines accept and which both do), `TEXT NOT NULL DEFAULT ''`, `REAL`, `INTEGER` standing in for a
boolean, `ON DELETE CASCADE`, the rename-aside table rebuilds in `0010`, `0014` and `0027`. All of it.

That is the good news and it is most of what backlog item 10 asked. The rest of this section is what sits
around those 80 statements.

### The runner's tolerance rule is written against a SQLite error string

`migrate()` swallows one error and only one:

```go
if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
    continue
}
```

Postgres does not say that. It says:

```
ERROR:  column "external_id" of relation "task" already exists
```

Replaying the migrations twice against one database produces 37 of those, one for every `ADD COLUMN` in the
file. None of them is swallowed.

This is not academic. `0005` says in a comment that its `ADD COLUMN`s are tolerated when already present, which
is why its `CREATE INDEX` had to be made `IF NOT EXISTS` too, or a partially applied migration could never
finish. `0014` says the same about its column add. Both of those guarantees evaporate on Postgres.

### Worse: continuing past a failed statement cannot work inside a Postgres transaction

Each migration runs inside `tx`. On SQLite, a statement that fails leaves the transaction usable, so `continue`
means the next statement still runs. On Postgres a failed statement poisons the whole transaction:

```
BEGIN;
CREATE TABLE probe_abort (id TEXT PRIMARY KEY);
ALTER TABLE probe_abort ADD COLUMN id TEXT;      -- the duplicate the runner swallows
ALTER TABLE probe_abort ADD COLUMN wanted TEXT;  -- the statement that had to run
COMMIT;
```

```
CREATE TABLE
ERROR:  column "id" of relation "probe_abort" already exists
ERROR:  current transaction is aborted, commands ignored until end of transaction block
ROLLBACK
```

The table does not exist afterwards. So on Postgres the `continue` does not merely fail to help, it guarantees
that the rest of the migration is discarded AND that the `INSERT INTO schema_migration` at the end is discarded
with it, which means the migration is retried forever and the daemon never starts.

The fix is not a second error string. Postgres has `ADD COLUMN IF NOT EXISTS`, which is idempotent by
construction:

```
ALTER TABLE task ADD COLUMN IF NOT EXISTS probe_col TEXT NOT NULL DEFAULT '';
ALTER TABLE task ADD COLUMN IF NOT EXISTS probe_col TEXT NOT NULL DEFAULT '';
NOTICE:  column "probe_col" of relation "task" already exists, skipping
```

SQLite has no such form, so this is one of the few places where one statement cannot serve both. Either the
statement is written per engine, or the `continue` is replaced by a SAVEPOINT around each statement, which
Postgres supports and SQLite also supports. The SAVEPOINT route keeps one statement list and is the one worth
doing, because it also fixes the general case rather than just `ADD COLUMN`.

**The recorded-by-name property survives either way.** Nothing here needs a migration edited or reordered.

### `migrate()` itself uses a `?`

```go
s.db.QueryRow(`SELECT name FROM schema_migration WHERE name = ?`, m.name)
```

First statement executed at startup, and it fails on Postgres before any migration is considered. Listed
separately because it sits outside the `migrations` slice and a sweep over the slice alone would miss it.

### `DROP TABLE task` fails on Postgres rather than cascading

`0025` contains a careful argument for why `task` was not rebuilt to widen its status `CHECK`: with foreign
keys on, SQLite's `DROP TABLE` performs the implicit delete and fires four `ON DELETE CASCADE` relationships,
taking every event, permission, message and launch spec with it.

On Postgres the same statement does not delete anything. It refuses:

```
ERROR:  cannot drop table task because other objects depend on it
DETAIL:  constraint launch_spec_task_id_fkey on table launch_spec depends on table task
         constraint permission_rebuilt_task_id_fkey on table permission depends on table task
         constraint message_task_id_fkey on table message depends on table task
         constraint event_rebuilt_task_id_fkey on table event depends on table task
```

And the thing `0025` gave up on is free:

```
ALTER TABLE task DROP CONSTRAINT task_status_check;
ALTER TABLE task ADD CONSTRAINT task_status_check CHECK (status IN (..., 'offered'));
```

Both succeeded, and the widened constraint is enforced immediately: `'offered'` inserts, `'nonsense'` is
refused. The same applies to `0027`, which spent a rebuild of the largest table in the database to add one
event kind and warned that this is "a fair price once and a bad habit". On Postgres it is two ALTERs and no
rebuild.

This does not argue for changing anything now. It argues that the constraint-widening cost that shapes several
design decisions in this file is a SQLite cost, and a port inherits none of it.

Note the constraint names in that error: `permission_rebuilt_task_id_fkey`, `event_rebuilt_task_id_fkey`.
Postgres keeps constraint and index names through `ALTER TABLE ... RENAME TO`, so the rebuild pattern leaves
the scaffolding name behind on every constraint it created. Cosmetic until a later migration wants to drop one
by name, at which point the name it wants is not the one that is there.

### `REAL` is not the same width

`task.rank` and `fixture.sort` are declared `REAL`. In SQLite that is an 8-byte IEEE double. In Postgres `REAL`
is `float4`, four bytes and about seven significant digits. Confirmed on the probe database:

```
 task    | rank | real
 fixture | sort | real
```

`SetRank` says what this costs: "Callers compute the midpoint of the two neighbors a card was dropped between,
so an insert never renumbers the cards around it." Repeated midpoint insertion between one pair of neighbours
exhausts float4 after roughly 24 halvings instead of 53, after which two cards hold the same rank and the drag
stops taking. Nobody would reach 24 by hand, and a script dropping cards in a loop would. `DOUBLE PRECISION` is
the column type that means what the code assumes.

## 2. The queries

82 SQL strings are passed to `Exec`, `Query` or `QueryRow` as constants or as constant concatenations. Nine
more are assembled at runtime and had to be read by hand. The tally, from the extractor:

```
static: 82  dynamic: 9
```

### The placeholders, which is the whole of it

Handed to Postgres unchanged, 71 of the 82 fail. The 11 that pass are the ones with no parameters:

```
prepared ok: 11
errors:      71
ERROR:  syntax error at or near ";"
LINE 1: ...er, enabled, sort, created_at FROM card_action WHERE id = ?;
```

Rewritten mechanically, `?` to `$1`, `$2` and so on in order of appearance:

```
--- errors: 0
```

All 82. Plus the nine runtime-assembled ones, written out in their widest form and prepared by hand: all nine.

So the placeholder is the only structural incompatibility in the query layer, and it is the kind a driver can
absorb. `pgx` in its stdlib mode does not translate `?`, so the translation is either a wrapper around `*sql.DB`
or a build-time rewrite of the strings.

**One trap.** `placeholders(n)` in `tasks.go` emits `?,?,?` for an `IN` list. `$n` is POSITIONAL, so it cannot
be produced by that helper in isolation. It has to be numbered against the assembled statement, which for
`EverRun` means the same `args` slice feeding a `COUNT(*)` and a `SELECT` gets two different numberings. Any
rewrite has to happen on the final string.

### What is NOT a problem, having been checked

- **`INSERT ... ON CONFLICT ... DO UPDATE`.** Six of them, in `actions.go`, `fixtures.go`, `harness.go`,
  `settings.go`, `shares.go`, `sources.go`. All Postgres syntax already, `excluded` and all. `INSERT OR REPLACE`
  appears nowhere in the package.
- **`sources.go` passing a Go `bool` into `CASE WHEN ? THEN 0 ELSE source.failures END`.** Postgres infers
  `boolean` for that parameter and the driver sends one. It also resolves `source.failures` inside `DO UPDATE`,
  because the target table really is named `source`.
- **No SQLite-only functions.** No `julianday`, no `strftime`, no `datetime()`, no `randomblob`, no
  `sqlite_*`. `LOWER`, `TRIM`, `COUNT`, `MIN`, `LIKE`, `LIMIT`, `OFFSET` and `COALESCE` are all standard.
- **`LastInsertId` is never called.** Text ULID-ish keys did their job. `RowsAffected` is called three times and
  both `lib/pq` and `pgx` support it.
- **Timestamp comparison holds up under a non-C collation.** Every timestamp in this schema is TEXT, so
  `last_activity_at <= ?` and `ORDER BY created_at` are text comparisons, and Postgres compares text under the
  database collation rather than byte order. Checked under `en_US.utf8`: the RFC3339 shape is fixed width with
  punctuation in identical positions in every value, so it orders correctly, the empty string still sorts
  before every real timestamp, and the range cutoff still lands where it should. This is a property of
  `TimeFormat` being fixed width, not luck, and it is worth knowing that a format change could break it.

### The one thing I went looking for and did not find

The brief said this code relies on SQLite's loose typing in at least one place, a card inserted with an empty
string into a column that holds a number. I could not find it, and I looked three ways.

- Postgres reports 20 non-text columns across the schema. Of the 91 queries, exactly 13 bind a parameter to
  one, and Postgres names the position and the type for each. Every one of the 13 call sites passes a Go `int`,
  `float64` or `bool` at those positions. `insertTask` in particular lines up 40 values against 40 columns with
  every `""` landing on a TEXT column, and the two numeric ones taking `t.PID` and `t.Rank`.
- The live database, on a copy, has no value of the wrong storage class in any of those columns. 53 cards, and
  `typeof(pid)`, `typeof(rank)`, `typeof(gated)`, `typeof(auto_approve)` and `typeof(pinned)` come back
  `integer`, `real`, `integer`, `integer`, `integer` for all of them. Same for `harness`, `fixture`,
  `perm_rule`, `card_action` and `card_share`.
- Nothing outside `internal/store` opens the database for writing. The only other `sql.Open` is
  `daemon/whichdb.go`, and it is read only.

So either it was fixed, or it was in a path that has not run. It is worth naming anyway, because Postgres would
have caught it the first time and SQLite never will:

```
INSERT INTO task (..., pid) VALUES (..., '');
ERROR:  invalid input syntax for type integer: ""
```

That is the shape of the whole class. A port does not need to find these by reading. It needs one test that
exercises every write path, and Postgres will name them.

### `launch_spec` is dead

Created in `0001`, referenced by no code outside `migrate_test.go`. It is the only table in the schema with an
`INTEGER` column nothing writes. Mentioned because it is one of the four foreign keys in that `DROP TABLE task`
error above, so it is not free.

## 3. The halt on Postgres

`internal/store/CLAUDE.md` states it plainly, and so does the daemon:

```
[atrium] HALTED: %v
[atrium] agent listener closing. runners will park on connection-refused and burn nothing.
[atrium] fix the cause and restart. atrium will not recover on its own.
```

The rule is right. On a local file, a failure that is not lock contention is a corrupt database, a full disk or
a bug, and none of those get better by being retried. Refusing to run is better than running without durable
state.

**A network database fails a way a file cannot: it goes away and comes back.** Measured on the probe container:

```
restart returned after 2s
accepting connections again after 2s
```

Two seconds. A managed Postgres failover, a rolling upgrade, a container reschedule, a laptop's wifi dropping
between the daemon and a database on another host, all sit in that same band, and all of them end by
themselves.

Here is what the store does with one, exactly:

```go
if !transient(err) {
    s.halt(err)
    return fmt.Errorf("%w: %v", ErrHalted, err)
}
```

and `transient` is:

```go
var se *sqlite.Error
if errors.As(err, &se) {
    switch se.Code() {
    case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED, ...
```

with a fallback that looks for the strings "database is locked" and "database table is locked".

Every part of that is SQLite. A Postgres connection error matches none of it, so the first query issued during
those two seconds halts the store permanently. The agent listener closes and stays closed. Every runner parks
on connection-refused, which is the behaviour the design wants for a REAL failure. A human has to notice and
restart the daemon. For a two second blip that had already ended before anybody could look at the board.

That is strictly worse than SQLite, where the same posture is correct, and it is the single finding here that
would make a Postgres deployment worse than the file it replaced.

### What the halt has to become, and what it must not become

It must not become "degrade and carry on". That is the rule the whole store is built on and it is right.

The distinction that has to be added is **is this failure over**, not **how bad is this failure**. Three
classes instead of two:

1. **Contention.** SQLite `BUSY`/`LOCKED`. Postgres `40001` serialization failure and `40P01` deadlock
   detected. Retried in place, never surfaced, which is what `guard` already does. Note that Postgres's two
   codes are not in the list today either, so even a healthy Postgres under concurrent load would halt atrium.
2. **Unreachable.** Connection refused, connection reset, a closed pool, `driver.ErrBadConn` that survived
   `database/sql`'s own two internal retries, `57P01` admin shutdown, `57P03` cannot connect now. This is the
   new class and it has no SQLite member. The right response is the halt's OUTWARD behaviour, closing the agent
   listener so runners park and burn nothing, WITHOUT the halt's permanence. A probe on a timer reopens the
   listener when the database answers again, and the board says "waiting for the database, down since X"
   instead of "halted".
3. **Everything else.** Constraint violation, syntax error, a column that is not there, disk full. Permanent,
   halts, needs a human. Unchanged.

Class 2 is the whole of the Postgres halt problem and it is a genuine design question rather than plumbing:
how long a database has to be gone before "it is coming back" becomes "somebody has to look at this". A
suggestion rather than a decision, because it belongs to whoever ports this. The current answer for a file is
zero seconds, and zero is the one answer that is definitely wrong for a socket.

### The four read-then-write pairs, and the comment that would become a lie

`Offer` in `intake.go`:

```go
// Checked and inserted inside one guarded call. The store holds a
// single connection, so this is serialized against every other writer
// and two ticks arriving together cannot both find nothing.
```

That is true because of one line in `Open`:

```go
db.SetMaxOpenConns(1)
```

whose own comment explains it as a throughput and WAL decision. It is doing double duty as the isolation
mechanism for four check-then-act sequences that are not in a transaction:

| | |
| --- | --- |
| `intake.go` `Offer` | `getBy(intake_key)` then `insertTask` |
| `sources.go` `SourceRan` | `SELECT failures` then `UPDATE ... failures = ?` |
| `sources.go` `SaveSource` | `SELECT created_at, enabled` then upsert |
| `actions.go` / `harness.go` save | `SELECT created_at` then upsert |

On Postgres with a connection pool, all four race. `Offer` racing is the one that matters, because it is
reached from a poller that runs on a timer and its entire job is to not create the card twice. The unique index
`idx_task_intake_key` catches the duplicate, so the failure is not a duplicate card, it is a unique-violation
error, which under the current `guard` is class 3 and halts the daemon. A source tick would take atrium down.

Two ways out. Keep `SetMaxOpenConns(1)`, which works and gives up every reason to be on Postgres. Or wrap the
four in a transaction and make `Offer` an `ON CONFLICT (intake_key) DO NOTHING RETURNING`, which is what the
partial unique index was already built for. The second is right and it is four small changes rather than a
rewrite. Either way, the comment quoted above has to stop claiming a property the connection pool provides.

### Two file-shaped things that have no Postgres form

- **`Store.Fresh()`** is an `os.Stat` on the database path before opening, and the daemon shouts when it is
  true because `WORKTREE_ROOT` being unset once made a hundred and twenty five rules appear to vanish. There is
  no file to stat. The Postgres equivalent is "did the migration runner create `schema_migration`", which is
  knowable and is a different piece of code.
- **`atrium preview --from live`** copies the database file plus its `-wal` and `-shm` sidecars, and
  `internal/cli/preview.go` documents at length why all three have to come. The whole feature is a file copy.
  On Postgres it is `pg_dump` into a fresh database, which is slower, needs the client tools present, and is a
  different failure surface. This is the daily workflow of every agent in this repo, so it is not a footnote.

## What was deliberately not done

**No driver was added, and it could not have been added cleanly behind a build tag.** A tagged Go file is
excluded from `go build ./...` and `go vet ./...`, so the compile is untouched, but the `require` line and the
`go.sum` entries live in files the default build reads. `scripts/ci.sh` does not run `go mod tidy`, so it would
not have been removed, but "the default build is untouched" would have been false. Everything above was
obtained by parsing the source and talking to Postgres with `psql`, which needs nothing in `go.mod`.

The cost of that choice is the one thing this probe cannot tell you: how `database/sql` plus a real driver
behaves at the boundaries. Specifically, whether `pgx` or `lib/pq` scans an `INTEGER` column into a Go `bool`
the way modernc's SQLite driver does, which `scanTask` and five other scanners rely on for `gated`,
`auto_approve`, `pinned`, `enabled`, `resume` and `wanted`. `database/sql`'s `convertAssign` handles int64 to
bool, so it should, and "should" is exactly the word this document exists to avoid. That is the first thing to
check when somebody does add the driver.

## What backlog item 10 should say

Suggested replacement for the body of item 10, since this document may not edit `docs/backlog.md`:

> The schema is portable and that is now checked rather than claimed: all 80 statements across 38 migrations
> apply to Postgres 17 unmodified, and all 91 queries parse and type-check once `?` becomes `$n`. See
> `docs/postgres-probe.md`. What a port actually costs is three things that are not the schema. The migration
> runner tolerates a duplicate column by matching a SQLite error string, and its `continue` cannot work inside
> a Postgres transaction at all. The halt treats every non-lock error as permanent, which is correct for a file
> and wrong for a socket that comes back in two seconds. Four read-then-write pairs depend on
> `SetMaxOpenConns(1)` for correctness, and `Offer` racing would halt the daemon on a unique violation. Add
> `atrium preview --from live`, which is a file copy, and `Store.Fresh`, which is an `os.Stat`.
>
> **Why it is still low.** Nothing needs it until multi-tenant does. What changed is that the unknown is now a
> list.
>
> **Effort:** the schema is done. A week to trust the halt.
