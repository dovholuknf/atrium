# Sending work to another machine

Thirteen runners saturate one desktop. There are four other machines sitting idle: a laptop, an M1 mini, and two
cloud boxes. The operator wants to hand a backlog item to one of them and keep orchestrating from here.

`docs/federation-design-v2.md` settled the shape of one board over many machines and stage one of it shipped:
`atrium room` dials a hub every twenty seconds, says what is on that machine, and the hub holds the answer in
memory and nowhere else. Cards travel INWARD. This document is the other direction, and it is the smaller half:
there was no way to say "start this there".

Read `docs/overlays.md` and `docs/federation-design-v2.md` first. Neither is contradicted here.

## 1. The connection does not change

The hub does not push. A leaf dials out and asks whether there is work for it, which is the posture `atrium
room` already takes and the reason it works from behind NAT.

The check-in already happens and already gets a reply, so **a launch request rides the reply it is already
receiving**. No new connection, no inbound reachability, nothing to configure on the awkward network, and no
second thing to keep alive.

```
  hub (this desktop)                            room (cdaws)
  ┌───────────────────────────┐                 ┌──────────────────────────┐
  │ :7778 board               │                 │ its own daemon           │
  │ dispatch table (sqlite)   │ ◀── check-in ── │ atrium room              │
  │   queued for cdaws        │     every 20s   │   "here are my cards"    │
  │                           │ ─── reply ────▶ │   "and here is what      │
  │   claimed, token minted   │                 │    you may start"        │
  │                           │ ◀── result ──── │ launches on ITS daemon   │
  └───────────────────────────┘                 └──────────────────────────┘
      never dials anything                        dials out, always
```

The result comes back on its own POST rather than on the next heartbeat. That is still the leaf dialling out, so
it costs no reachability, and it means a refusal is on the board within a second of being decided rather than
twenty.

## 2. What a queued launch is

**A row on the hub with a room name on it.** Not an offer that rooms compete for.

An offer pool answers "somebody run this", and that is not the question being asked. A laptop, an M1 mini and two
cloud boxes are not interchangeable: they have different toolchains, different checkouts and different network
reach, and the operator handing an item over already knows which machine it belongs on. An offer pool would also
make the hub arbitrate a race it has no reason to have.

**The queue is durable, and the room list beside it is not.** Those look inconsistent and are not. A room's cards
belong to that room's database, so a copy here would be a second source of truth that is wrong the moment the
room is unreachable. A queued launch is this machine's own record of something it asked for, and a promise that
evaporates when the hub restarts is not a queue.

`internal/store/dispatch.go`, migration `0039_dispatch`.

### Two rooms cannot both take one item

The room name is not the guarantee. Two different rooms never see each other's items, which is easy. The race
that matters is **one room name and two processes**: a second `atrium room` started by hand, or a retry after the
reply to a check-in was lost on the wire.

So a claim is a conditional state change in a single statement:

```sql
UPDATE dispatch SET state = 'claimed', token = ?, attempts = attempts + 1, claimed_at = ?
WHERE id = ? AND state = 'queued'
```

`RowsAffected` of zero means somebody else got there, and the item is left out of that handout rather than being
handed to both. The winner is given a token, and a result is only accepted from whoever holds it, checked inside
the statement that settles the row.

### The states

| State | Means |
| --- | --- |
| `queued` | Waiting for its room to check in. The only state it can be withdrawn from. |
| `claimed` | Handed over. A token exists and the lease is running. |
| `running` | The room said it started something. Terminal HERE: from now on that room is the truth. |
| `failed` | The room refused, and `error` says why in that room's own words. |
| `cancelled` | Withdrawn before anybody took it. |

`running` being terminal is the point. Once a room reports a card, the work is visible the way every other remote
card is: in that room's check-in, on that room's board. The hub keeps a pointer, never a copy.

### When a room takes an item and says nothing

A room that claims an item and then loses power never reports anything, and without a lease the item is stuck in
`claimed` until a human notices. So a claim older than five minutes goes back to `queued`, and the next claim
mints a fresh token, which is what stops the previous holder from answering for it later.

That is at-least-once, and it is honest about it. Three guards keep it from turning into two runners in one
directory:

- **A batch fits inside the lease.** A room runs its handout one item at a time and bounds each launch at a
  minute, and a handout is at most three items, so a full batch is three minutes against a five minute lease. A
  room part way through one reports `busy` and is handed nothing more, so two batches never run end to end.
  Raising the handout size without raising the lease breaks this and the constants say so.

- **The room remembers what it has already done**, keyed by item id, and answers a repeat handout with the stored
  result instead of launching again. In memory, so it dies with the room process, which is the honest limit.
- **An item is handed out at most twice.** Beyond that the item is not unlucky, it is wrong: a runner that does
  not exist on that machine, or a directory that is never going to be there. It fails with a reason rather than
  starting a broken launch every time that machine comes back.

### Withdrawing

Only while it is `queued`. Once a room has claimed one there is nothing here that can stop it, because the hub
never dials a room. Marking it `cancelled` while a session starts on another machine would be worse than saying
no, so it says no and names the machine to stop it on.

## 3. The part atrium must not solve and must not ignore

**A remote machine does not have the worktree.** Every launch here names a directory that already exists because
a tool outside atrium made it. cdaws has no `D:/worktrees/...` and never will.

Three answers were on the table:

- **The launch carries a prepare command the room runs first.** The harness table already has `Prepare`, so this
  is nearly free. **Rejected.** `Prepare` exists to capture an ENVIRONMENT, and overloading it into "the thing
  that makes the workspace" is where atrium starts holding git commands somebody wrote on another machine. That
  it is cheap is exactly what makes it a slope. The rule that atrium does not learn git is worth more than this
  feature.
- **The room is given a workspace root and clones into it.** Same objection, one layer down.
- **Preparing the directory was somebody else's job.** **Taken.**

### What that means concretely

The room is the authority on its own filesystem, and the resolution order is:

1. **The directory the dispatch named**, if it named one.
2. **The `cwd` on that room's own harness row.**
3. **Refuse.**

There is no fourth step. The local launcher falls back to `os.Getwd()` here, and doing that for a remote
instruction would start a session in whatever directory the room process happens to be sitting in: a plausible
directory, the wrong one, on a machine nobody is watching.

**The ordinary dispatch names no directory at all.** A cloud box with one checkout points its `claude` runner at
it once, and every item queued for that box lands there. The hub never learns a path on another machine, which
is the property that makes this work at all.

**A dispatch may only name a directory on a room that has said where.** `atrium room --workspace <dir>` is the
consent. Without it, a hub naming an absolute path would have arbitrary reach into that filesystem, and the room
granted nothing of the sort by agreeing to report its cards. With it, the path resolves through
`internal/safepath`, which follows symlinks on both sides, so a junction inside the workspace pointing at the
rest of the disk does not get through.

The harness's own `cwd` is not checked against the workspace, and that is not an oversight. The workspace bounds
what the HUB may name. What the room's own operator configured on the room's own harness row is that machine's
business, and checking it would refuse the ordinary setup, where the one checkout is nowhere near whatever
directory was nominated for hub-supplied paths.

### When the directory is not there

**It is refused before anything starts, and the reason travels back to the hub and onto the row.**

```
D:/worktrees/atrium/remote-launch is not a directory on this machine. atrium does not
create worktrees, so whatever makes them has to have run here first
```

That is the whole design goal of this half. A card that fails immediately with a readable reason beats a card
that silently starts in the wrong place, and on a machine nobody is looking at, the second one is not discovered
for hours.

No card is made on the hub for a refusal. The queue row IS the record: it carries the state, the room, the
runner, the directory that was asked for and the reason. Fabricating a local card for work that never started
somewhere else would be a copy of a card that does not exist.

### A room may refuse work outright

`atrium room --no-launch` reports cards and takes nothing. It is said on every check-in rather than configured on
the hub, because the machine granting the hub the right to start processes on it is the one that should be able
to withdraw it by restarting with a flag.

A room that says no is handed nothing. Its items stay `queued` and the board says why beside the room that is
refusing them, which is a state somebody can act on. Handing them over to be bounced would spend the item's two
attempts on a machine that was never going to run it.

## 4. What this looks like

On the hub:

```
atrium dispatch to cdaws --runner claude --prompt "backlog item 12: the board throws away where you were"
atrium dispatch list
atrium dispatch cancel <item>
```

Or the board: **runners → rooms**, `send work to a machine`, and the queue is drawn under **queued for other
machines**.

On the machine doing the work:

```
atrium room --hub http://desktop:7778 --name cdaws --board http://cdaws:7778
atrium room --hub http://desktop:7778 --name cdaws --workspace /home/clint/work
atrium room --hub http://desktop:7778 --name cdaws --no-launch
```

## 5. What this does NOT do

- **Permission forwarding.** Another session built that. A gate question raised on cdaws is answered on cdaws.
- **Attach by redirect.** A pseudo terminal cannot leave the machine that made it. The queue row carries a link
  to the card on that room's own board, which is where its terminal is and always will be.
- **Creating the directory.** Said three times above and once more here, because it is the rule this whole
  document is arranged around.
- **Telling a room to stop something.** The hub does not dial a room, so it cannot reach in.

## 6. Open: how work gets BACK

**Nothing has answered this, and it has to be decided before the feature is usable.** Sending work is half a
loop. Right now a session on cdaws finishes and the result is: a card on cdaws's board, in a directory on cdaws,
with a recap in cdaws's database. The hub sees the card go `done` in a check-in summary, and that is all.

The four candidate answers, and none of them is built:

- **The room pushes its recap.** `atrium finish` already records what a session says it did, bounded, on the
  card. A room could carry the recap of a finished card in its check-in the way it carries the status, and the
  hub could draw it on the queue row that started it. Cheapest, and answers "what happened" without answering
  "where is the code".
- **The work was on a branch and the branch is the answer.** The room pushes, and what comes back is a URL. This
  is almost certainly right and it is exactly where atrium must not go: pushing is git, and `docs/scm-design.md`
  is where the outbound half of that question already lives. The honest version is that the ROOM's own harness
  or an action on that card runs the push, and what atrium carries is the URL somebody else produced.
- **The hub pulls the diff.** Rejected on sight for the same reason a forum with a store is rejected: the hub
  would be holding a copy of something that lives somewhere else.
- **Nothing comes back, and the room's board is the answer.** Defensible and unsatisfying. It means orchestrating
  from here still requires opening four boards to find out what happened, which is the complaint that produced
  federation in the first place.

The recommendation is the first plus the third form of the second: **a finished card's recap rides the check-in,
and any URL the work produced is a field the room fills in from an action the operator wrote.** Atrium carries
strings it did not derive, which is the same posture `LaunchRequest.Repo` and friends already take.

## 7. What the board draws

- The **rooms** pane gains a line per room saying whether it takes work and what a directory would be checked
  against, so queueing for a machine that has decided to run nothing is a mistake you cannot make silently.
- A **queued for other machines** pane: one row per item, with the state, and the refusal in the room's own words
  where there is one. A `withdraw` button, only while the item is still `queued`.
- A dialog for sending work. Deliberately NOT the launch dialog with a room dropdown on it: that dialog browses
  this daemon's filesystem, picks from this machine's runners and offers to resume a conversation this machine
  recorded, and every one of those is a fact about here rather than there.

## 8. Rules this design does not break

- **Storage failure halts, it does not degrade.** The dispatch table is on the hub's own store and goes through
  `guard` like everything else. A refusal is NOT returned to `guard`: anything it gets back that is not
  contention halts the store, so a full queue or a stale token is reported outside the closure. Getting that
  wrong means a script queueing one item too many takes the daemon down.
- **A hook must never fail a session.** Hooks do not touch any of this.
- **A room is not a runner.** The check-in and the result both sit on the HUMAN listener, which is where the room
  endpoint already was. The agent listener closes when the store halts so runners park, and a hub that has halted
  should still be able to say so to the rooms attached to it.
- **Nothing observed is stored.** A room's cards are still in memory only. What is durable is what this machine
  asked for.
- **Atrium drives an overlay, it does not become one.** The dispatch rides the same connection `atrium room`
  already makes, including over `--service`. Nothing new is dialled and no identity is held.
