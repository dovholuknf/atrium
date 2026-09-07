# A second board, on a copy, that acts on nothing

`atrium preview` starts a whole second daemon: its own database, its own ports, and its own address file. It
exists so a change to the board can be looked at before it is installed.

## What it is for

The board is `internal/api/web/index.html`. Changing it means installing the binary and restarting the daemon
that every live session on the machine is attached to. So a session working on the board has to interrupt
everybody else to show its work, and the operator has to take that interruption before knowing whether the work
was any good. Judging the change from a diff instead is judging a picture of a picture.

A preview is the same daemon serving the same board out of your worktree's build, on a port nobody else is
using, against a copy of the cards you actually have.

```
atrium preview --http 50022 --from live
```

It prints where it is listening, and that address is the whole output anybody needs:

```
preview of d:/worktrees/github/dovholuknf/atrium/preview-docs
  board   http://127.0.0.1:50022
  cards   d:/worktrees/.../.atrium/preview/preview.db
  hooks   untouched. this is not the machine's atrium.
  stop    ctrl-c
```

Build in the worktree you are previewing. A binary from another tree serves that tree's board, and the change
you came to look at will not be in it.

## The flags

| Flag | What it does |
| --- | --- |
| `--http` / `--agent` | Ports. Both default to whatever the operating system says is free, so several can run at once. |
| `--from` | A database to COPY before starting. `live` is the one the running daemon opened, read from its address file. |
| `--db` | Where this preview keeps its cards. Defaults to `.atrium/preview/preview.db` inside the worktree. |
| `--dir` | The worktree being previewed. Defaults to the current directory. |
| `--fresh` | Throw the previous preview's state away first. Without it, a preview keeps the cards it had, and says so. |

Ports are asked for before the daemon starts rather than left to the listener, because the address has to be
PRINTED and a port the operating system picked is not knowable until something has bound it. Both are reserved
together, since taking one and releasing it before taking the second is how both end up being the same number.

State lives inside the worktree it belongs to. Throwing the worktree away throws the preview away with it, and
two worktrees cannot end up sharing a database by accident.

## The two rules that make it safe

Everything else about a preview is convenience. These two are the difference between a window and a second
operator.

### Its own address file

Every daemon writes down where it is listening, and every hook in every claude session on the machine reads that
file to find one. `internal/daemon/whereami.go` is the whole mechanism, and it does not lock, heartbeat or
arbitrate. The last writer wins.

A second daemon that writes the machine's address file therefore takes every hook on the box. The symptom is not
an error. It is activity badges, permission prompts and session lifecycle events arriving at a board nobody is
looking at, while the board somebody IS looking at goes quiet. `writeLocation` logs a warning when it can see it
is taking the file off a live process, but that warning lands in the second daemon's own output, which is the
terminal of the person who did not need telling.

`Options.LocationFile` already existed for this, because a test that starts a daemon and stops it was deleting
the address of whatever daemon was really running at the time. `atrium preview` points it at
`.atrium/preview/daemon.json`, and that one line is what makes the subcommand harmless.

The cost is real and it is the point: a preview is not a second place to work. A claude session started under one
still files its activity, its permissions and its finish to the machine's real daemon. A preview is for LOOKING
at a board.

### Passive: opening a database is not consent to act on what is in it

`Options.Passive` in `internal/daemon/daemon.go` means serve the board and touch nothing outside the process.

Everything in a copied database is real. Real fixtures, real card shares, real cards. An ordinary start ACTS on
all of it: `startFixtures` spawns every terminal that comes up with the daemon, `RestoreCardShares` re-binds
every share that was lent out when the last daemon went down, and `SweepDeadCardShares` removes the reserved
overlay names whose cards have since gone. The first version of preview did all three. It spawned the operator's
runners and went after their zrok name, from a process that was supposed to be a window onto a copy.

Passive turns off exactly those three. What stays on is the store, the JSON API, the board, the SSE stream, the
reaper, the source loop, and the ability to attach to something you started HERE, because those are what is
being looked at.

Two things it is not:

- **Not a security boundary.** Somebody using a preview board can still launch a runner or shelve a card in the
  copy. The rule is about STARTUP.
- **Not a read-only mode.** The copy is an ordinary database and the preview writes to it freely. That is why it
  is a copy.

### Why `--from` copies rather than opens

Two daemons on one sqlite file is two writers, and the preview is the one that gets restarted and thrown away.

The copy takes `-wal` and `-shm` with it. Sqlite in WAL mode keeps recent writes in the first of those, so a
database copied on its own opens fine and is missing whatever happened most recently. That reads as a board that
is mysteriously out of date rather than as a bad copy, which is the worst way for it to fail. A cleanly closed
database has no sidecars at all, so their absence is not an error.

The source file is never opened. Only read.

## The question that produced it: can several atriums share one database?

It gets asked about once per person who reads the code, and the answer is not the one the question expects.

**Sqlite does not stop you.** `internal/store/store.go` sets `journal_mode = WAL` and `busy_timeout = 5000`, so
several processes can hold the same file, readers do not block behind the writer, and ordinary contention is
absorbed before it reaches any retry. If storage were the obstacle, the answer would be a connection string.

**What stops you is that both of them ACT.** Each of these is a machine-wide singleton the database knows
nothing about:

- **Fixtures.** Two daemons each read the fixture rows and each start every terminal on them. You get two of
  every runner in the same directory, and neither knows about the other's.
- **Card shares.** Two daemons each try to re-bind the same reserved zrok name on the same account. One wins,
  and the sweeper on each side sees names it cannot account for.
- **The address file.** Two daemons each write it, and every hook on the machine follows whichever wrote last.
  Which board your events land on becomes a question about restart order.
- **Migrations.** Both run them at startup against the same file. The runner tolerates a statement that is
  already there, so this is the mildest of the four, and it is still two processes racing to author a schema.
- **Supervision.** The daemon owns a pty per runner in the operator's own logon session. A second daemon can
  read the card and cannot attach to a terminal it did not open. Half a board that cannot be used is worse than
  no second board.

**Postgres changes none of that**, which is the part people assume it fixes. Postgres replaces the storage
engine. Every item above is a process reaching outside itself at a resource no database mediates: an operating
system process, an overlay account, a file in the user's runtime directory, a pty handle. Moving the rows to a
server leaves all five where they were. Postgres is worth doing for other reasons and this is not one of them.

### Does `Passive` generalise?

`Passive` is one writer and any number of readers, and it generalises exactly that far.

It generalises for READING. Any number of passive daemons can serve a board over one dataset, and that is a real
answer to "several people want to look at these cards", cheaper than federation because nothing has to be
routed. What it costs is that none of those boards can be worked in. It also does not make a shared dataset out
of a copy: `--from` snapshots, and a snapshot does not follow the original.

It does not generalise to WRITING, because a passive daemon is not a reduced-privilege daemon. It is an ordinary
daemon with its startup side effects switched off, and nothing enforces the reduction after startup. Growing it
into a genuine read-only mode means auditing every write path, which is different work with a different
justification.

### What would have to be true for two ACTIVE atriums to share state

Not a plan. A list of what the question costs, so nobody starts it by pointing two daemons at one file.

1. **Ownership on the rows that spawn things.** A fixture, a harness row and a card share each have to name the
   daemon responsible, and a daemon acts only on its own. That is a lease, with a lease's problems: what happens
   when the owner dies, who may take it over, and how long a terminal it started stays orphaned.
2. **A single binder for overlay names.** A reserved zrok address is one name on one account. Either exactly one
   daemon may bind it, or every daemon gets its own namespace and the address stops being stable, which is the
   reason it was reserved. See `docs/overlays.md`.
3. **Routing for the hooks.** One address file cannot name two daemons. Either a session records which daemon it
   belongs to, or something in front of both answers on one port.
4. **An answer for the pty.** There is no answer that keeps today's behaviour. A terminal belongs to the logon
   session of the process that opened it, so a daemon elsewhere can show you a card and never its terminal.

Read `docs/federation-design-v2.md` before starting any of it. One board over many machines was asked and
answered, and the answer was that the machines do NOT share a database: leaves dial out and the forum holds
nothing. If the requirement is "one board, several machines", that document is the design. If it is "look at the
board without disturbing the one I work in", it is this one, and it is already built.
