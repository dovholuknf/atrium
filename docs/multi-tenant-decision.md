# Multi-tenant atrium, and the decision not to build it yet

Backlog 3 asks a question that has been open since the daemon existed: could atrium hold more than one person.
It was parked because it contradicted every note saying one person, one machine, no auth. Two of those three
objections have since moved, so the parking is no longer justified by its original reasons and needs new ones or
none.

This document supplies the new ones. It is a decision rather than a design. Nothing here is built, and the
recommendation is that nothing here gets built in the shape the backlog entry assumes.

**The short version.** Fork rather than flag, and the standing note is right for a weaker reason than the one it
gives. The smallest tenancy boundary that is not a lie is an operating system user, which is to say a whole
atrium, which is to say the thing that already exists. What breaks first is not the supervisor: it is that
atrium has no concept of WHO, anywhere, and the supervisor is merely where that costs the most. Postgres buys
concurrent writers and buys nothing else that matters, because everything two active daemons fight over is
outside the database.

What to do instead is at the bottom, and it is cheaper than any of this.

---

## What actually moved

**Authentication exists, and it is not the kind that helps.** `internal/daemon/auth.go` puts an OIDC sign-in in
front of the published board, and it does it without atrium holding anybody's credential: identity is delegated,
the cookie proves a completed verification, and the allowlist defaults to nobody. That is a good design and it is
the right shape for a tenant boundary to sit on.

It is also, today, a turnstile and not a boundary. `internal/daemon/auth_flow.go:160` reads the cookie like this:

```go
if _, ok := readSession(key, c.Value); ok {
```

The subject is discarded. `signSession` goes to the trouble of putting a verified identity in a MAC-signed
cookie and the guard throws it away, because the only question anybody has ever needed to ask is "may this
request in". Nothing downstream of the guard knows who is asking, and nothing downstream has anywhere to put the
answer. Turning authentication into authorization is not a small edit to `authGuard`. It is a principal
threaded through every handler, every store call and every timer in the daemon, and the timers do not have a
request to thread it from.

**Federation exists, and it decided the opposite of multi-tenancy.** Rooms are the cheaper half, as the backlog
says, but the interesting part is what stage one refused. `internal/daemon/rooms.go` holds a room's cards IN
MEMORY and nowhere else, and says why at length: a room's cards are that room's state in that room's database,
and a second durable copy is a second source of truth that is wrong the moment the room is unreachable. It also
refuses to federate attach, because a pseudo terminal cannot leave the machine that made it.

So federation looked at "cards from more than one place" and answered with a cache and a link. Multi-tenancy
asks for the thing rooms deliberately did not build: one durable store holding several people's work. The
precedent runs against it, and the reasoning that produced the precedent has not weakened.

**The word `tenant` is already taken, and it means something else.** `internal/store/tenant.go` has
`SettingTenant`, `SetTenant`, `Qualify` and `LocalName`. A tenant there is THIS ATRIUM'S NAME FOR ITSELF, one per
database, immutable after it is set, and its entire job is to prefix wire names so two machines running the same
image in the same directory do not silently hand one session another's card. It is a collision-avoidance prefix.

Every use of it is a name being built or looked up. There is no `WHERE tenant = ?` in the tree. Reusing the word
for an isolation boundary would put two meanings on one identifier in the one package where getting it wrong
hands somebody another person's card, its history and its permission rules, which is the exact failure that file
was written to prevent.

---

## 1. Fork or flag

**Fork. Agreed with the standing note, and its stated reason is the weaker one.**

The note says bolting tenancy onto a tool designed around one person costs the tool its clarity. That is true and
it is an aesthetic argument, which means it loses to a sufficiently motivated afternoon. Here is the argument
that does not lose.

**A flag is a `WHERE` clause, and atrium's decisive code paths have nothing to put in it.** Three of them, each
load-bearing, each with no caller identity available at the moment it decides:

- **The permission chain** starts at `onPermRequest` in `internal/daemon/daemon.go:461`, from a hook POST on the
  agent listener. The only identity in that request is `req.Agent`, a name the session chose for itself, and
  `CLAUDE.md` states the posture plainly: the hub trusts it, no auth. `store.Register` then AUTO-CREATES a card
  for a name it has never seen (`internal/store/tasks.go:143`). The listener that answers the most consequential
  question in the product will mint a new identity on request.
- **The reaper** runs on a timer (`internal/daemon/reaper.go:45`). It lists every card and asks the local
  operating system whether a pid exists. There is no request, no caller and no session. A tenancy predicate here
  would have to come from the row, and the row would have to be trusted, and the row was written by the listener
  above.
- **The supervisor** holds `runner` values in a map in memory (`internal/daemon/supervisor.go`), keyed by task
  id, in a process whose operating system user IS the person. There is nothing to scope, because the process is
  the scope.

A flag would therefore not be one flag. It would be a principal invented at the agent listener, where there is
nothing to derive it from, carried into a store that has no column for it, and consulted by timers that have no
request. That is not a feature behind a switch. It is a different program with the same board on top, which is
the same conclusion `docs/packaging.md` reached about running the daemon as a Windows service: the literal
reading installs, reports `Running`, and supervises nothing anybody can use.

**But fork-or-flag is a false pair, and the third option is the recommendation.** See "What to do instead".

---

## 2. The smallest tenancy boundary that is not a lie

**It is an operating system user.** Which is to say: a logon session, a home directory, a database, an address
file and a daemon process. Which is to say a whole atrium, unforked.

That is not a rhetorical answer. It is what the code already enforces, and the five things the question asks
about each land on it for their own reason.

### Cards

Not isolated, and cheap to isolate badly. `task` has no owner column, `Store.List()` returns everything, and
every consumer takes the whole list: the board, the reaper, `peers`, the sweeper. A scope column plus a
predicate at every call site is a day's work and produces a board that LOOKS isolated.

It would be a lie, because the isolation would end at the row. Two things reach past it immediately:

- **The peer bus.** `internal/daemon/peers.go:115` builds the addressable list from `d.st.List()` with no filter
  beyond status, and `handleTell` takes `from` off the request body and qualifies it. Nothing checks that the
  caller is who it says it is. Under two tenants, one person's session can enumerate the other's sessions and
  queue a message into one, wearing any name it likes. `docs/charon.md` makes discovery mandatory before
  sending, which is correct for one operator and is a directory service for everybody else's work under two.
- **Permission rules.** `MatchRule` (`internal/store/rules.go:329`) scopes on `scope = '' OR scope = ?`, where
  scope is a WORKTREE PATH. An empty scope is a wildcard over the entire database. The only existing scoping key
  in the permission chain is a filesystem path, and `internal/safepath` exists precisely because atrium had no
  correct answer to "is this path inside that directory" until file transfer forced one. Building a tenant
  boundary on path prefixes means building it on the primitive that already documents three ways it gets broken,
  one of which fails OPEN on Windows.

### Terminals

Not isolatable at all without changing what a terminal is. A supervised runner is a `pty.Pty` and a ring buffer
held in one process's memory. Attach is a websocket into that process. `docs/architecture-v2.md` records that
closing one terminates the attached process and that ConPTY offers no reattach, so there is no orphan-survival
path to build. `rooms.go` draws the conclusion from it: a pseudo terminal cannot leave the machine that made it.

So the boundary here is the process, and it is a hard one rather than a soft one. Two tenants sharing a daemon
share a supervisor. Two tenants not sharing a daemon do not have a shared board, which is the entire feature.
This is the contradiction the backlog entry names, and it is real.

### Permission rules and settings

The `setting` table is `key TEXT PRIMARY KEY` (`internal/store/schema.go:370`), one flat global namespace, and
seventeen keys live in it. They split three ways under two tenants and the split is not clean:

- **Genuinely deployment-wide:** `sweep_dead_after`, `prune_after`, `scrollback_lines`, `scrollback_mb`,
  `shared_location`.
- **Per person, harmless:** `board_skin`, `editor_command`, `shell_command`, `paste_preamble`, `paste_keep`.
- **Per person, and a security decision if it is not:** `global_auto`, `browse_roots`, `auth_oidc`,
  `auth_cookie_key`, `overlay_zrok`, `overlay_ziti`, `tenant`.

`global_auto` is the one to look at. It is read on every permission request from every card
(`internal/daemon/daemon.go:593`) and is recorded under its own name because "I turned this session loose" and "I
turned the whole board loose" are different answers to give six hours later. Under two tenants it acquires a
third meaning nobody chose: I turned YOUR sessions loose. `browse_roots` is the same shape pointed at the
filesystem, and `auth_oidc` holds the allowlist deciding who may in at all.

Scoping the settings table means changing its primary key, which SQLite cannot do in place. That is a table
rebuild, the `0010` and `0014` pattern, appended to the end of the slice and never edited. Doable. Not the
expensive part.

### Overlays

One `overlay_zrok` row, one `overlay_ziti` row, one reserved board name, one `--shutdown-token`, one OIDC
allowlist, one cookie key. A share also changes a global rule: while any share is running the shutdown endpoint
stops trusting a loopback address, because the overlay terminates connections locally and every remote request
presents as `127.0.0.1` (`docs/overlays.md`). That is one machine-wide safety property, toggled by whichever
tenant last started a share.

There is exactly one place in the tree where a per-thing boundary was built deliberately, and it is worth
copying rather than extending: `internal/daemon/overlay_guest.go`. Lending one session serves a SEPARATE
RESTRICTED HANDLER on its own listener, with an allowlist, not the board with a filter over it. The file says why
it has to stay an allowlist: a denylist over an API this size is a list of the paths somebody remembered, and the
endpoint added next week is not on it. It then documents the hazard that proves the point, where the allowlist
alone was not enough because an already-allowed attach route grew a `?kind=shell` parameter and a guest could
have got a general purpose command line by appending six characters.

That is the honest shape of a tenant boundary: a separate listener, a separate handler, default deny, enumerated.
Note what it is not. It is not a column, and it is not a filter. And note what the guest handler is allowed to
do, which is read one card and watch one terminal. Scaling that shape up to a tenant who may launch runners,
write permission rules, browse the filesystem and answer permission prompts produces, again, a separate process.

### Credentials

This is where the boundary stops being an engineering question.

`internal/daemon/launch.go:401` starts every runner's environment from `os.Environ()`, the daemon's own, and
`spawnPTYResume` (`internal/daemon/supervisor.go:392`) resolves the command on the daemon's PATH and starts it
with no impersonation of any kind. Every runner runs as the daemon's operating system user, with that user's
environment. `internal/claudeconf/hooks.go:164` writes hooks into `os.UserHomeDir()`, one `settings.json` per
operating system user, and the header notes that breaking it breaks every claude session on the machine.

`docs/packaging.md` already worked out what happens when the daemon is not in the user's own session, in the
course of deciding against a Windows service. Session 0 means the user profile is not loaded, so DPAPI, the
credential manager, the ssh agent and the per-session PATH are absent or different, and every claude session it
spawned would inherit that. That analysis concluded the daemon must run as the user, in the user's session, and
the logon task is what ships, with the cost stated: it stops when you log out.

That conclusion and multi-tenancy are the same fact seen from two sides. The runner needs the operator's session
because that is where the credentials live. Ten developers means ten sets of credentials, which means ten
sessions, which means ten daemons, which means the boundary is the operating system user and always was.

**So: the smallest tenancy boundary that is not a lie is one daemon, one database, one logon session, one home
directory, one address file. A board per person is not a tenant. A process per person is.**

---

## 3. What breaks first

The question assumes the supervisor is the answer, and it is the answer to a different question. Ordered by how
few tenants it takes to be wrong:

**First, and at N of 2 on day one: the permission chain, because there is no principal.** Not the ordering, which
is sound, but the identity underneath it. `req.Agent` is self-declared and mints a card on first contact.
`handleTell` takes `from` off the wire. `authGuard` verifies an OIDC identity and discards the subject. Two
people on one deployment means every decision the chain makes is made on behalf of a name that anybody who can
reach the listener could have supplied. The chain does not fail loudly here. It works exactly as designed, on an
identity that means nothing.

This is also the cheapest thing on the list to notice and the most expensive to fix, because fixing it is the
flag that is really a fork.

**Second, at N of 2 the first time anybody publishes: the overlays.** They fail in a way you would see. One zrok
name, one auth allowlist, one cookie key, and a share that changes the shutdown rule for the whole machine. Two
tenants both publishing produce a name collision and a settings row that one of them overwrote. Loud, annoying,
and genuinely fixable by scoping the settings table.

**Third, and worst: the supervisor.** Worst, not first, and the distinction matters. It does not fall over. It
runs, correctly, and the failure is silent: tenant B's runner gets tenant A's environment, tenant A's ssh agent,
tenant A's PATH and tenant A's `~/.claude/settings.json`. There is no error, no log line and nothing on the
board. You find out when somebody's agent pushes with somebody else's key.

The container answer that the backlog names is correct and is a different program, and it is worth being precise
about how different. Moving runners into containers means four things, each of which is a rewrite:

- The pty is created inside the container and proxied out, so `attach.go`, `supervisor.go` and the ring buffer
  are written against a stream rather than a `pty.Pty`.
- Hooks post from inside the container to a daemon outside it, so `whereami.go`'s address file stops being the
  answer and becomes network configuration.
- `claudeconf` writes into a container's home rather than the operator's.
- `internal/safepath` stops being the containment answer, because the container is.

That is four of the packages in the layout table, replaced rather than modified.

**The three break for one reason.** Atrium has no concept of who, so the permission chain decides on behalf of a
name, the overlays let people in without recording which one, and the supervisor runs everything as the same
person. A fix to any one of them without the other two produces a boundary that is drawn on the board and not
enforced anywhere, which is the worst outcome available: a multi-tenant atrium people believe.

---

## 4. What Postgres buys, and what it does not

**It buys three things.** Concurrent writers without a file lock and without the halt being a shared fate. A
database reachable from more than one machine. Real types and real constraints, where the schema currently uses
text ULID-ish keys, RFC3339 text timestamps, `CHECK` instead of enums and TEXT instead of JSONB to stay portable
to it.

**It buys nothing about tenancy, and nothing about two active daemons, because everything they fight over is
outside the database.** `docs/dispatch-queue.md` group D says two atriums on one database is not blocked by
sqlite, it is blocked by both of them ACTING. The specific list, with what each one is:

- **Fixtures.** Both daemons start every fixture on boot (`internal/daemon/fixtures.go`). Two runners in one
  directory writing the same files, neither knowing about the other. The `if_running` guard in `Launch` answers
  this within one daemon and has nothing to say across two.
- **Overlay names.** Both re-bind the same reserved zrok name. One wins and the other reports an error that
  classifies as "name already taken", which is true and unhelpful.
- **The address file.** `whereami.go` is last-writer-wins, and every hook on the machine reads it. The file's own
  header notes that staleness is deliberately unguarded because a refused connection is the fail-open path every
  hook handles. That reasoning holds for a dead daemon and not for a live wrong one: hooks would post their
  permission requests to whichever daemon wrote the file most recently.
- **The reaper.** This is the sharpest one, and it is worth stating on its own because it is the case people
  assume a shared database solves. `reapOnce` lists every running card and calls `processAlive(t.PID)` against
  the LOCAL operating system. Given a shared database, daemon A evaluates daemon B's cards by asking its own
  kernel about a pid from another machine. It is wrong in both directions: absent means it marks a healthy
  session dead, and pid reuse means it marks a dead session alive with confidence. Postgres makes this worse, not
  better, because it is what lets the two daemons see each other's rows in the first place.
- **The supervisor and the activity badge.** Both in memory by design (`supervisor.go`, `activity.go`, and
  `docs/activity-design.md` on why activity is never written down). A card's terminal is attachable only from the
  daemon that spawned it. This is the same conclusion rooms reached, from the other end.

**`Options.Passive` is not the generalisation people want it to be.** It is one writer and any number of readers,
and it works: `atrium preview` opens a copy and serves the board without starting fixtures, restoring shares or
sweeping. But its own comment refuses the reading (`internal/daemon/daemon.go:65`): not a security boundary and
not a read-only mode. Somebody on a preview board can still launch a runner or shelve a card. The rule is
narrower and it is about STARTUP, that opening a database is not consent to act on what is in it.

So passive generalises to MANY VIEWS and not to many tenants. It is abstention chosen by the operator who started
the process, not a constraint imposed on somebody else. Turning it into a constraint means enumerating what a
passive daemon may not do, at every handler, default deny. Which is the guest handler's shape again, at the scale
of the whole API.

**Where Postgres actually belongs:** in the control plane of the fork, if the fork ever happens. It is a
consequence of that decision and not an input to it. Doing it first buys a portability exercise for a schema
nothing is asking to move.

---

## What is refused, and why

- **A tenancy flag on the daemon.** Refused because the identity it would filter on does not exist at the three
  places that decide, and inventing one at the agent listener means trusting a name off the wire, which is the
  posture `CLAUDE.md` states and the posture a tenant boundary cannot have.
- **Reusing the word `tenant`.** Refused. It means "this atrium's name for itself" in
  `internal/store/tenant.go`, it is immutable, and it is a collision-avoidance prefix. A second meaning goes in
  the file where getting it wrong hands somebody another person's cards. A hosted fork picks a different word.
- **A scope column on the tables, alone.** Refused as a deliverable. It is a day's work, it makes the board look
  isolated, and it isolates nothing while runners share an environment and the peer bus takes its sender off the
  wire. A boundary that is drawn and not enforced is worse than none, because people act on it.
- **Attach across a tenant boundary.** Refused for the same reason rooms refuses it across a machine boundary. A
  pty belongs to the process that made it. The federated answer is a link to the board that owns the terminal,
  and the tenant answer is the same link.
- **A shared database between two active daemons.** Refused regardless of engine, per group D and the reaper
  above. One writer, any number of readers, and the readers abstain at startup.
- **Doing Postgres first.** Refused as sequencing. Nothing needs it until the fork does.

## What would change this answer

Written down so a later session can check rather than re-derive.

- **A supervisor that does not need the operator's logon session.** Not a wrapper around one. A runner in a
  container with its own credentials, its own home and a proxied terminal, with `attach.go` written against a
  stream. If that exists for one tenant, most of this document is obsolete.
- **A principal that survives from the edge to the store.** An OIDC subject that `authGuard` keeps rather than
  discards, an agent listener that will not mint a card for an unattested name, and a `from` on the peer bus that
  is verified rather than declared. All three, or none of them is worth having.
- **Somebody who wants to pay for other people's credentials.** Ten developers on one deployment means ten sets
  of keys somebody is responsible for. That is an operational commitment, not an engineering one, and it is the
  thing that makes this a product rather than a feature. Nobody has asked for it.

## What to do instead

The pair in the backlog entry is fork or flag. The answer is neither, yet, and the reason is that the cheap
eighty percent already shipped under a different name.

**Many atriums, one view.** One daemon per person on their own machine, in their own logon session, with their
own credentials, which is what the packaging work already installs and what the supervisor already requires.
Rooms aggregate them onto one board. The tenant boundary is the operating system user, enforced by the operating
system, which is the only enforcement in this document that does not have to be written.

What that leaves undone is genuinely a subset of multi-tenancy and it is what backlog 1 already lists: permission
requests from a room, so a blocked card on somebody else's machine is visible on yours, and attach by redirect,
so clicking a remote terminal opens that room's board with the right card open. Both are designed in
`docs/federation-design-v2.md`. Both are days rather than weeks. Neither requires a principal, a schema change or
a container.

The one thing rooms will not give you is somebody running their agent on YOUR hardware. That is hosting, and
hosting is the fork. It is worth having decided that deliberately, which was the ask, rather than drifting into a
scope column and a board that lies.
