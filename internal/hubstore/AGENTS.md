# internal/hubstore

The hub's own SQLite: which rooms exist, their names, transports and join secrets, a cache of what each room last
said, and the hub-side rows of the backlog, reports, documents, the file drop, growlers, web push, PR claims, the merge
flow and the git push log. Read the package comment in `store.go` first.

## The hub holds no work, and this package does not break that

The hub's founding rule is that it holds no WORK: no sessions, no pseudo terminals, no agent processes. Stopping it
costs nobody a session. Nothing here is work. What is here is the hub's own truth, which a room cannot answer, such as
whether a room by that name was ever made.

## Configuration versus cache

- **Configuration is the hub's truth.** Rooms, names, transports, secrets, deletion state. Written by a person.
- **The cache is never authoritative.** `room_card` is what a room last said. It is written while the room is
  connected and read only when it is not. Nothing read from it is ever written back as fact. There is no merge and no
  way to update one cached card: a room says everything or says nothing. `docs/fabric/hub-decisions.md`, 11 to 15.
- **The cache holds exactly what the room itself persists.** What a runner is doing right now, subagent chatter and
  typing state are streamed through and never land here, for the same reason the room never stores them.
- **Observed never overwrites desired.** A room's `self_name` is shown beside the hub's name for it, never instead.
  A room's spec is desired and its lock is observed, and `SetRoomSpec` and `PutRoomLock` never touch each other.
- **Liveness is a socket in `internal/link`, and this package never learns it.** `LikelyAttached` is an inference from
  `last_seen_at` and says so in its name. It is enough for a CLI to refuse to force out a room that is plainly there,
  and not enough for anything that must be right.

## The halt, and the one way to refuse without causing it

Storage failure halts the hub, exactly as `internal/store` halts a room. Every method goes through `guard`, which
retries contention (12 attempts, backing off from 2ms) and halts on anything else. Contention that never clears halts
too. Once halted, every call returns `ErrHalted` and `OnHalt` tells the hub.

**A caller that gave up is not a broken database.** `guard` hands back `context.Canceled`, `DeadlineExceeded` and
SQLite's interrupt without halting (f-001: a dropped request once halted the hub and every room was let go). Writes
that take a context run on `detached(ctx)`, so they land whoever stopped waiting. A halted hub keeps the rooms it
knew attached, from memory, in `internal/cli/atrium_hubrooms.go`.

**A deliberate refusal must be wrapped in `refuse(...)`.** `guard` lets through `sql.ErrNoRows`, a constraint
violation, and anything wrapped by `refuse`. Any other error returned from inside a guarded operation is treated as
the database breaking, and the hub halts. A new validation error returned bare is a typo that takes the hub down.

## Migrations

`schema.go` has the same two rules as the room's store and its header says why. **Add at the END of the slice**, and
**write every statement to tolerate already being there** (the runner swallows "duplicate column name"). A migration
already recorded never runs again, so editing one is the same as not shipping it. The schema is written to stay
Postgres portable and nothing has ever run it against Postgres.

## Rules that hold whoever calls

- **A join secret is shown once and stored as its SHA-256.** Minting replaces whatever the room had, and a secret
  authorises exactly one name, so there is nothing for a room to claim.
- **A backlog item's id is given by the filer and never reused.** Filing a taken id answers `ErrItemExists` with the
  row. A report is append-only.
- **A change request ends once**, and there is one open request per source and target. Nothing here reads a branch.
- **A gate met is never re-opened** (`deps.go`), so a re-signed branch cannot un-land an item. Whether a gate is met
  is decided in `internal/link`.
- **The first PR claim wins**, and nothing releases one because its room went away. Only the operator's move does.
- **A flow stage only moves forward** through a report. Only the operator's reset lowers it. One epoch is open at a
  time.
- **Web push holds at most `PushMax` rows and evicts nothing**, so a stranger cannot push the operator's phone out.
  The endpoint is the proof of ownership, never the id.
- **A notify row is not deleted because its card left the cache.** A restart or a shelve takes a card out and puts it
  back, and the same identity must not fire twice. The growler table follows the same rule.
- **Document and drop bytes live in files named for their SHA-256** beside the database, written through a temp file
  and a rename. `DocAdd` and `FileAdd` are the only ways in and apply every rule, the secret checks included.
- **A drop file is disposable.** The snapshots cover the rows and not the blobs. Nothing may treat one as kept.
- **The secret checks are a speed bump, not a guarantee.** They catch unambiguous shapes. Check the RESOLVED target of
  a path by name, and check content on every way in.

## Backups

`BackUp` snapshots the store every ten minutes with `VACUUM INTO`, which writes one file and reads every page. Copying
`hub.db` with the operating system while the hub runs is wrong, since recent rows sit in `hub.db-wal`. Snapshots are
kept in tiers: everything from the last hour, hourly for a day, daily for a week, weekly for a month.

## The files

| | |
| --- | --- |
| `store.go` | the package comment, `Open`, `guard`, `refuse` and the halt |
| `schema.go` | migrations, applied in slice order. Read the header before editing |
| `rooms.go` / `secrets.go` | the room inventory, and the one-time join secret |
| `cache.go` | what each room last said. Never authoritative |
| `audit.go` | what happened to a room, kept after the room is gone. No foreign key on purpose |
| `backlog.go` / `backlog_follow.go` | backlog items and reports, ids, refresh from a file, following a card |
| `deps.go` | item gates, loop refusal, renames |
| `item.go` | atrium items: one row per item, an idempotent upsert on `(item_id, revision, state)`, owner resolution (never a tester). See `docs/changes/items-stage2a.md` |
| `task.go` | the tasks of an item: rows, every transition, learnings. `docs/runtime/item-tasks-design.md` |
| `changerequest.go` | change requests between rooms |
| `flow.go` / `flow_epoch.go` | the merge flow's roles, stages, checks and epochs. |
| `merge.go` | the merge queue: requests, their states and output, the merge room. `docs/rnd/merge-queue.md` |
| `gitpush.go` | the push log behind `gitsync.PushLog` |
| `docs.go` / `docs_secrets.go` | hub documents and the secret checks every way in passes |
| `files.go` | the hub file drop |
| `growl.go` / `notify.go` | what waits on a human, and what the hub last notified about |
| `push.go` | web push subscriptions |
| `prclaim.go` | one owning room per pull request |
| `roomspec.go` | a room's desired spec and observed lock. No credentials in either |
| `settings.go` | hub settings keyed by name, the board skin and the legacy overlay switch |
| `backup.go` / `vacuum.go` | tiered snapshots, and handing freed pages back to disk |
