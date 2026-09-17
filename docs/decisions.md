# Decisions

One question at a time. Each entry is the question as asked, the design agreed, and the outcome once it is
built. Nothing is implemented until its entry says the design is settled.

Written down because the alternative is deciding the same thing twice and differently. A conversation is not a
record.

------------

## 1. Where do room settings live?

**Asked 2026-09-17.** Settled.

### The question

`settings -> this machine` had grown into nine unrelated things in one list: the editor command, where pasted
files land, what is typed in front of a pasted path, which directories the picker may open, the worktree
command, how deep to look for repositories, the shared address file, the shell command, and scrollback size.

The name meant nothing. On a room, everything is about that machine. It grew by accretion: each setting was
added where there was space rather than where it belonged.

### The design

**A room is the unit, not a machine.** The word is `room` everywhere: these are per-room settings, not per-
machine ones, and the distinction is real because one machine can hold more than one room and a hub can be a
room itself.

**`rooms` enumerates every room.** That is the list, and it is what the tab is for.

**Every room in that list carries a settings cog.** Opening it gives that room's settings, and nothing else's.
Everything room-shaped belongs there, including the ones that are not in `this machine` today:

- what is in `this machine` now: editor command, pasted files, paste preamble, picker roots, worktree command,
  repository scan depth, shared address file, shell command, scrollback
- `run agents here too`, which is currently a panel above the room list
- anything else that is a fact about one room

**The gear keeps what belongs to the board.** One board, one answer: the skin, notifications, backup,
housekeeping.

### The outcome

**Built 2026-09-17.** Every room in the list has a cog, and behind it is that room's own pane: what the hub
knows about it, the nine settings that used to be `settings -> this machine`, and `run agents here too` on the
hub's own room. `this machine` is gone as a heading, and nothing asks which machine you meant any more, because
the room is in the title of the pane you opened.

Three things fell out of building it:

- **The hub's own room is on the list whether or not it is running.** The switch that turns it on lives behind
  its cog, so the row had to exist before the switch was ever thrown.
- **A board with no hub draws one row, `this machine`, with the same cog.** The daemon is the hub with its own
  room, and without that row the settings would have been in the page and unreachable.
- **An offline room's pane shows what the hub knows and no fields at all.** The only authoritative answer about
  a room comes from that room, and an empty box that saves nowhere is worse than no box.

------------

## 2. Do the sub-panes move behind the room cog too?

**Asked 2026-09-17.** Settled.

### The question

The `rooms` tab has a column down the left: runners, fixtures, sources, recognisers, actions, dispatch. Every
one of them is per-room data. If a room gets a settings cog, do these go behind it?

### The design

**They stay.** Most of what is in that column is worth seeing across every room at once, which is what a hub is
for: all the runners, all the fixtures, all the sources, grouped by the room they are on.

**The room-specific ones also appear behind that room's cog**, and being in both places is fine. The same fact
reached from two directions is not a contradiction: from the column you are asking "what runners exist
anywhere", and from the cog you are asking "what is this room set up to do".

### The outcome

**Half built, 2026-09-17.** The column stayed, which was the decision. The room-specific panes do not appear
behind the cog yet: what is there is the room's own settings and nothing else. Putting a room's runners and
fixtures there too is worth doing and is not what any of the first four items were for.

------------

## 3. What is a hub, and what is a daemon?

**Asked 2026-09-17.** Settled.

### The question

The board is one set of files serving two shapes: `atrium daemon`, which holds the database and runs the agents
itself, and `atrium2 hub`, which holds nothing and proxies to rooms. A per-room settings cog has nothing to
attach to on the first one, because it has no rooms.

### The design

**There is one thing. The daemon IS the hub.** Not two programs and not two words: `atrium` serves the board,
and rooms dial into it.

**A room is what holds agents.** Its own database, its own terminals, its own runners.

**The hub may run its own room**, on its own machine, which is how one atrium on one laptop still works. That is
the `--room` switch that exists today.

So the rooms list always describes everything that can run agents, including the hub's own, and the cog in
question 1 always has somewhere to live.

### The outcome

Not built yet. `atrium2` stays a separate binary until this is real, so the atrium in daily use is never the
one being rebuilt.

------------

## 4. Does the hub run its own room by default?

**Asked 2026-09-17.** Settled.

### The question

The hub's own room is off unless asked for. That was decided when the hub and the daemon were separate
programs, where a normal machine ran the daemon and a hub was a thing you chose. Now that they are one thing,
off by default means a fresh install serves a board with nothing behind it until somebody finds the switch.

Asked again with that changed. It was also worth asking whether a hub with nothing attached should run its own
room until a real one dials in.

### The design

**Off, still.** No automatic behaviour, no starting it because nothing else turned up.

A hub that holds a database is a hub whose restart is no longer free, and that freedom is the only reason the
two halves are separate. Turning it on has to be somebody saying so, once, and not something that happened
because the room they expected was slow to connect.

An empty hub says what it is and how to give it a room. That is a board that explains itself, which is better
than one that quietly became something else.

------------

## 5. Where do the hub's own settings live, and what are they?

**Asked 2026-09-17.** Settled in shape. The transports below still need their own entries.

### The question

Some settings are the hub's rather than the board's or a room's: where rooms dial in, over what, join tokens,
which binaries it can hand out. There is nowhere for them.

### The design

**HOW A ROOM JOINS A HUB IS THE HUB'S DECISION**, and it gets a pane of its own that reads like `expose the
board`: one panel per way in, each saying what it costs and what it needs, because they are not
interchangeable and the differences are the whole choice.

The bar for all of them is the experience that exists today. `atrium join atr1_...` is one paste and it works.
Nothing here may be harder than that.

#### direct mTLS

What ships. A one-time token, a key made on the room, a certificate signed by the hub, pinned by fingerprint.

The honest assessment: complex and brittle. Atrium has to hide all of making a keypair, registering it and
handing it back, and it does. It needs a listening port, which means network reach, which makes it the least
secure of the four. And a certificate expires, so **rotation is something this has to cover** and does not.

#### zrok private

Atrium embeds the zrok SDK, so a private share should be close to free. What it needs spelled out: where the
zrok account lives, that the hub is `zrok enable`d, and that each room is `zrok enable`d with its own enable
token.

Atrium has to make all of that easy rather than describe it.

#### zrok public

The hub published on a public share. Basic auth is the floor; mTLS over the top would be better if it can work
at all here.

**Least convinced of the four.** Needs fleshing out before it is offered.

#### OpenZiti

The hub needs an identity and a service to bind. Enrolling from a JWT has to be easy. A room needs its own JWT
and the service to dial, or the intercept to use.

Assume the overlay already exists. Under those terms this is the most straightforward of the four.

### What exists today

Direct mTLS, working, with the `atr1_` token. `ziti.go` binds and dials a service. `zrok.go` uses the SDK in
tunnel backend mode, which is the private case. All of it is CLI flags: there is no pane, and no rotation for
any credential.

### The outcome

Not built yet.

------------

## 6. Can a hub accept rooms over several transports at once?

**Asked 2026-09-17.** Settled.

### The design

**Yes. Any transport the hub has configured.** Direct mTLS on the LAN and OpenZiti from elsewhere is one hub,
not two.

Structurally this already holds: the hub serves a listener, and it can serve several. It is how the hub's own
in-process room attaches today, beside the real one.

------------

## 7. Who decides a room's name, and what proves it?

**Asked 2026-09-17.** Settled.

### The question

Over zrok or OpenZiti the network has already decided who may connect before a byte reaches atrium. Is the
transport's answer enough to say WHICH room this is, or does atrium keep an identity of its own?

The case with no answer is zrok private: the hub knows a connection arrived through its own share and not who
sent it. Two rooms on one share, and either can call itself the other.

### The design

**THE HUB NAMES THE ROOM, AND THE NAME TRAVELS IN THE JOIN TOKEN.**

You add a room on the hub and give it a name. That name is minted into the token. The room joins with the token
and is called what the token says. It does not choose, and it does not ask the transport.

The transport is not the identity. Changing an OpenZiti identity, or moving a room to a different zrok account,
must not rename a room or orphan its work.

### Why this is better than what exists

Today the token carries the transport, the address, a fingerprint and a secret, and NOT a name. The room names
itself at enrolment and the hub signs whatever it asked for, which is why the hub carries a hand-written check
refusing a second room that claims a name already attached.

With the name in the token that hole closes by construction: a secret authorises one name, so there is nothing
to claim. `--name` on the room stops being a flag.

### And what the room calls itself

A room still says what it is, and the two are shown side by side.

**This is `docs/architecture-v2.md`'s observed-versus-overrides rule, not a new one.** What a machine reports
never overwrites what a human typed. The hub's name is the override and is the name; the room's own hostname is
observed and sits beside it.

So there is no preference to configure. A setting for which name wins would be a second source of truth for one
fact, which is the shape of the bug that made a shell both a runner and a machine property.

### The outcome

**Built over direct mTLS, 2026-09-17.** The name is minted into the token, the secret is bound to that room in
the hub's store, and spending it is both the proof and the answer in one step. The certificate carries the name
the hub chose, the room reads its own name back out of that certificate rather than out of the string it was
given, and `--name` on the room is gone. The fallback that let `sign` take a name from the signing request is
gone with it, which is the line the hole actually lived on.

**Not proven over ziti or zrok**, where there is no secret to spend and no certificate to sign. See 18.

------------

## 8. Is a room a durable thing on the hub, or only a live connection?

**Asked 2026-09-17.** Settled.

### The design

**Durable.** `add a room` writes a row. The room appears in the list from that moment, before it has ever
connected, and stays there when it is offline.

The reason is inventory: the question "have I already made that room" has to be answerable, and a list that
only shows what is currently dialled in cannot answer it.

**The join secret is shown once.** It is copy-once, the way a secret should be. If it is lost, the hub
regenerates the token rather than revealing the old one.

### The outcome

**Built 2026-09-17.** `atrium2 hub room add <name>` writes the row and prints the string, once. `ls` shows every
room whether or not it has ever connected, with what it calls itself beside what it is called. `token <name>`
mints a fresh one and retires the old, because the hub holds a hash and cannot show what it printed before.
`mark` and `rm` are there too, with `rm` refusing a room that is not marked, a room whose cards the hub last saw
on it, and any room heard from in the last twenty seconds.

A room the hub has no record of cannot attach, even holding a certificate this hub signed, and that is checked
on every heartbeat rather than only at attach: the store is a file, and forcing a room out is another process
writing to it.

The board's version of this list is item 3 and is still to come.

------------

## 9. What happens when a room is deleted?

**Asked 2026-09-17.** Settled.

### The design

**A HUB CANNOT DELETE A ROOM THAT HAS ACTIVE CARDS.** Not refuse-and-warn: refuse. The cards are work, on a
machine the hub does not own, and a list that forgets them does not stop them.

The order is: clear the cards, the ROOM CONFIRMS they are done and cleaned up, and only then is the row
removed. The room stays visible throughout, because a room mid-cleanup is a thing that still exists.

**`marked for deletion` is the state in between.** A room marked for deletion starts no new cards. Everything
already running continues and is worked out normally.

**Marking is reversible.** The hub can cancel it, new cards can be started again, and it is business as usual.
Nothing about marking destroys anything, which is what makes it safe to press.

### The machine that never comes back

A room marked for deletion can go offline and stay offline: a dead laptop, a wiped machine. Its confirmation is
never coming, and a row that can only be removed by a room that no longer exists is a row nobody can remove.

**A CONNECTED room cannot be deleted. A DISCONNECTED one can be forced out, behind a large warning.**

The warning is the point. Forcing removes the hub's record and nothing else: if that machine ever comes back it
is still holding cards, directories and sessions the hub has now forgotten about.

### Why the room has to confirm

The hub holds nothing. It cannot see whether a directory was cleaned up, whether a throwaway was deleted, or
whether a session really ended, and it must not decide those from the outside. The room is the only thing that
knows, so the room says so.

### The outcome

Not built yet.

------------

## 11. Does the hub get a database of its own?

**Asked 2026-09-17.** Settled.

### The question

A durable room list, the names, the join secrets and the deletion state all have to be written down. The hub's
founding rule is that it holds nothing, which is exactly what makes restarting it free.

### The design

**Yes, a local database. SQLite, written to port to Postgres if that is ever needed**, which is the rule the
room's schema already follows: text keys, RFC3339 text timestamps, `CHECK` instead of enums, TEXT instead of
JSONB, `?` placeholders.

**It is not all or nothing.** The store holds two kinds of thing, and the difference between them is the whole
rule:

**Its own configuration, which is the hub's truth.** Which rooms exist, what they are called, how they may
connect, their join secrets, which are marked for deletion. Nobody else knows these and nothing else can
answer them.

**A cache of what each room last said.** Its cards, its last activity, enough to draw a board. Written down so
a hub with a room offline can still show what was there rather than nothing.

**THE CACHE IS NEVER AUTHORITATIVE.** The only authoritative answer about a room comes from that room, and
while it is connected it is the one asked. The cache is what was last heard, shown as such, and a connected
room overrides it without argument. Nothing read from the cache is ever written back as fact.

**The founding rule is restated rather than broken.** The hub still holds no WORK: no sessions, no pseudo
terminals, no agent processes, and no authority over any of them. Stopping it costs nobody a session, which is
the only property that matters.

### When that store fails

**It halts. It does not start with a corrupt database and it does not carry on with one.**

The same rule the daemon already follows, and for the same reason: running without durable state is worse than
not running. A hub that keeps serving while it cannot remember which rooms exist is a hub that will mint a
second room under a name it has forgotten, or fail to refuse a deletion it has no record of.

That the work is safe on the rooms is not a reason to stay up. It is a reason the halt costs little.

### And it backs itself up while it runs

A halt on a corrupt database is only tolerable if there is something to go back to.

**The running process takes its own periodic snapshots**, kept in tiers so the history is useful rather than
merely long: roughly every 10 minutes, hourly, daily, weekly. **Restoring a previous one has to be easy**,
because a backup nobody can restore under pressure is a backup that does not exist.

This is new. What `back it up` offers today is a manual export of configuration as one file a repository can
hold, which is a different thing: it is for rebuilding a machine from a checkout, not for going back twenty
minutes after something went wrong.

### The outcome

**The store is built, 2026-09-17**, as `internal/hubstore`: rooms, names, transports, secrets, deletion state,
the cache tables and the audit log. Postgres-portable in the way the room's schema already is. It halts rather
than degrading, and it refuses to start on a database it cannot read, which is asked at open rather than found
at the first query.

**The backups are not built.** Tiered snapshots and an easy restore are item 8 and are still to come, and the
halt is only as tolerable as they make it.

------------

## 12. A cached card whose room is offline

**Asked 2026-09-17.** Settled.

### The design

**Nothing opens.** The card carries a no-entry mark, and its tooltip says:

> room `$xyz` is offline. cannot restore terminal

Not a read-only view of the cache, and not an error after the click. The cache exists so the board can still
show what was there; it cannot show a terminal, because a terminal is a live thing on a machine that is not
answering. Offering to open one would be offering something the hub cannot produce.

### And every other operation on that card

**All of them are refused until the room is back**, with a message saying so. Renaming, tagging, shelving,
queueing a message, answering a permission: refused.

**NO MUTATION QUEUEING.** Not "we will apply it when the machine returns". A queue of intentions against a
machine nobody has heard from is a second source of truth that has to be reconciled, and the reconciliation is
the part that goes wrong: the card changed on the room while the change was waiting, and now two answers exist
and one has to lose.

An offline room is a room you cannot act on. That is a simpler thing to explain and a simpler thing to be
right about.

### The rule, in one sentence

**The source of truth is always the online room, and only the online room can change it.** The hub caches the
last known state and nothing more, until that room is back.

------------

## 13. What happens to the cache when a room comes back?

**Asked 2026-09-17.** Settled.

### The design

**The room announces its state, and that announcement is taken whole.**

It is the only source of truth, so what it sends IS the state. **Anything the hub was holding that is not in
the new announcement is discarded entirely**, because it is no longer there. No merging, no row-by-row
reconciliation, no keeping something the room did not mention on the chance it still exists.

### And it is written down

The discard is not silent. Coming back produces an audit entry along the lines of:

> room `sparta` came back online. 3 cards were discarded as they are no longer there. 2 cards are new. 4 cards
> changed.

Wholesale replacement is the right rule and it is also the one that can quietly lose something a person
remembers seeing. The log is what makes that answerable afterwards instead of being a thing nobody can explain.

### The outcome

**Built 2026-09-17.** Announcements are taken whole and the discard is written down, with one refinement: a
line is written when something was DISCARDED, and not for every announcement. A card that is new or changed has
not been lost, it is on the board. A line every two seconds saying nothing was lost is a log nobody can read,
and the point of this one is that somebody can.

`atrium2 hub room log` prints it, for one room or all of them, and it answers for a room that has been forced
out, since that is the case it is mostly for.

------------

## 14. How the cache stays current while a room is connected

**Asked 2026-09-17.** Settled.

### The design

**The room sends updates and the hub writes them down.** Pushed, not polled: the room is the only thing that
knows something changed.

**Written on change, coalesced under load.** A timer was the first instinct and it is the wrong trigger: most
of the time nothing is happening, so a fixed tick means a change waits for no reason, and the moment things get
busy it is the wrong number anyway. Writing as changes arrive, with a small ceiling of a second or two when
they come faster than that, keeps quiet periods fresh at no cost and stops a busy room thrashing the database.

### What is cached, without a list to maintain

**THE HUB CACHES EXACTLY WHAT THE ROOM ITSELF PERSISTS, AND NEVER WHAT THE ROOM DECLINES TO PERSIST.**

That line comes from a rule atrium already has. `docs/activity-design.md` says what a runner is doing right now
is never written down, because "it would be a lie the moment the daemon restarted". A hub writing it down would
break that rule at one remove.

So: a card's title, status, tags, worktree and runner live in the room's database and are cached. Activity,
subagent chatter and typing state are in memory on the room, are streamed straight through, and are never
cached. Nothing here needs a field list, and the two halves cannot drift apart.

### What it costs

An unclean disconnect loses the last window of changes, so a card that finished moments before a crash can
show as running. Decision 13 corrects that wholesale when the room returns, which is why the window being
small matters more than it being exact.

### The outcome

**Built 2026-09-17.** The room watches its own event stream and announces on change, with a two second ceiling,
over a connection it dials for the purpose. An announcement identical to the last one is dropped, which is what
stops a working agent rewriting the hub's cache every two seconds for as long as it works.

**"Exactly what the room persists" is a new endpoint rather than a field list.** `/v1/state` answers with the
stored rows; `/v1/tasks` answers with a view that carries what is true only this second. Stripping the live
fields from the view would have been a list to maintain, and the day somebody adds a field to that view is the
day the hub starts remembering it.

Two failures found in review and worth writing down, because both turn a room's bad day into lost history:

- A room whose store has halted answers with an error rather than a card list. Read carelessly that is an empty
  announcement, and an empty announcement means "everything you were holding for me is gone". A room in trouble
  must not be able to tell the hub its work no longer exists.
- A room holding nothing must still be able to say so. Go marshals an empty list as `null`, which is exactly
  what "I could not tell you" looks like on the wire, so the endpoint sends a list either way.

------------

## 15. When is the cache read?

**Asked 2026-09-17.** Settled.

### The design

**Only when the room is offline. While a room is connected the cache is written and never read.**

Not a read-through cache and not a performance layer. A connected room is asked, every time, and its answer is
the answer. The hub does not serve a remembered card next to a live one and hope they agree.

This is what makes decision 12 simple to reason about: the board is either showing a live room, or it is showing
what a room last said and marked as such. There is no third state where some rows are fresh and some are
remembered and nothing says which.

### The outcome

**Built 2026-09-17, on the reading side.** The hub's inventory only fetches a room's cached card count when
that room is not attached, and `atrium2 hub room ls` does the same. **Serving cached CARDS to the board is not
built**: that is item 6, along with everything else about how an offline room draws.

------------

## 16. Do offline rooms' cards appear on the board and the stack?

**Asked 2026-09-17.** Settled.

### The design

**Yes, and they are kept apart.**

**Live rooms first, always.** What is running is what the board is for, and it is never pushed down the page by
what is not.

**Then a divider, and one big group holding every offline room's cards, collapsed by default.** One group, not
one per room: it is the same kind of thing, which is work you cannot touch right now, and four collapsed
headings for four dead laptops is four times the furniture for one fact.

**A room that has never connected does not appear on the board or the stack at all.** It cannot have cards, so
there is nothing to draw. It exists in the `rooms` tab, which is the inventory, and that is the only place it
belongs.

### Why collapsed

An offline room's cards can be looked at and cannot be acted on: decision 12 refuses everything until the room
is back. Open by default, they are a screen of things that do not respond to being clicked. Collapsed, they are
one line saying the work still exists, which is the fact worth carrying.

### And they are not counted

**The header numbers are live only.** The terminals badge, the permissions count, anything saying how much
wants attention: every one of them counts connected rooms and nothing else.

A number you cannot act on is a number that makes you look. Three agents waiting for permission on a laptop
that is shut is not three things to do, and a badge saying so is a badge that is wrong in the way that costs
most.

**The room counter is what reports the other half.** `1/2 rooms` says a room is missing, in the one place whose
job is to say so.

### The outcome

**The counter is built, 2026-09-17.** It reads live over ever-connected, so a room that is offline is counted as
missing and a room that has never connected is not counted at all: nothing is missing because of it.

**The board and the stack are not done.** Offline rooms' cards, the divider, the collapsed group and the
live-only badges are item 6 and are still to come.

------------

## 17. Does a hub with a database still restart freely?

**Asked 2026-09-17.** Settled.

### The design

**Yes. Freely and aggressively, and often.** Nothing about the hub gaining a database makes a restart something
to schedule.

**The hub is the UI.** It is the half being changed, all day, and the whole reason the split exists is that
changing it must cost nothing. The data comes from the rooms; the hub draws it.

### What that constrains

This is a rule about everything above it, not a note at the end of them. Every design here has to survive the
hub going away mid-sentence and coming back seconds later:

- the cache is written, never read while a room is connected, so a restart cannot serve a stale card
- a room's reconnect announces its whole state, so a restart converges rather than reconciles
- no mutation queue exists to be lost
- the room holds the sessions, so a restart is a dropped socket and a redial

A hub that needed to be stopped carefully would be a hub that had quietly become a room.

------------

## The design is settled. What is not covered here

Decisions 1 to 17 are enough to build from, agreed 2026-09-17.

**The zrok and OpenZiti experience gets its own round of questions.** Decision 5 says what the four transports
are and what each one costs, and stops there. How somebody actually gets a zrok private share or an OpenZiti
identity set up without reading two other products' documentation is a separate conversation, and guessing at
it here would be inventing answers nobody agreed to.

### Order of work

Everything hangs off the first two.

1. **The hub's store.** SQLite, Postgres-portable, migrations. Rooms, names, secrets, deletion state, the
   cache tables, the audit log. Halts on a corrupt database (16).
2. **`add a room`.** The hub mints the name into the token, the room takes the name it is given, `--name` on
   the room goes away (7, 8).
3. **The rooms tab.** The durable list, live before offline before never-connected, a cog on every row, a
   transport badge (1, 8, 10).
4. **Room settings behind that cog.** `this machine` moves there, along with `run agents here too` (1).
5. **The cache.** The room pushes, the hub coalesces, written while connected and read only when not
   (14, 15).
6. **Offline behaviour.** No-entry cards, every operation refused, the collapsed group on the board, live-only
   counters (12, 16).
7. **Deletion.** Marked for deletion, the room's confirmation, force-remove behind a warning (9).
8. **Backups of the hub's store**, tiered and restorable (16).

------------

## 10. How a room is connected, on the row

**Asked 2026-09-17.** Settled.

### The design

The room list says `sparta attached 1h30m` and not how it got here. It gets a small mark for the transport:
zrok, OpenZiti, or mTLS.

**A badge and nothing more**, which is what `docs/hub-room-requirements.md` already says: transport is
ancillary noise and must never become a concept in the UI. Worth seeing at a glance, never worth a column.

### The outcome

**Built 2026-09-17.** A small chip beside the room's name: `mTLS`, `ziti`, `zrok`, or `here` for the hub's own
room, which reaches it over a pipe inside one process. The tooltip says what each one means, which is the only
place the word "transport" appears at all.

------------

## 18. What proves a room's name over an overlay?

**Raised 2026-09-17 while building 2. NOT SETTLED. This one is a question, not a decision.**

### How it came up

Decision 7 says the hub names the room and the name travels in the join token. Building that made the direct
mTLS path hold it exactly: the secret is bound to one room in the hub's store, spending it answers with that
room's name, and the name goes into a certificate the hub signs. Editing the token changes nothing, because the
token is not what the hub reads afterwards.

A review of that work pointed out that the same guarantee does not hold for the other two transports, and it is
right.

### The gap, stated plainly

A ziti or zrok join string is base64 JSON with nothing signed in it. There is no secret to spend and no
certificate to issue, so the name is a field the room sends. On attach the hub checks that a row by that name
exists and nothing more.

**So anybody the overlay already lets through can edit the name and attach as any room on the hub.** Over zrok
private that is exactly the case decision 7 named as having no answer: the hub knows a connection arrived
through its own share and not who sent it.

### What it is not

It is not a regression. Before this work a room named itself outright on every transport, and the hub signed
whatever it asked for. The overlay path is no weaker than it was. The direct path got much stronger, and the
difference between them is now visible.

### Why it is written down rather than fixed

Fixing it means deciding what binds a room to an overlay identity, and that is the first of the questions the
zrok and OpenZiti round is for. The obvious answers each carry a decision nobody has made:

- a store-minted secret spent on first attach, which needs something durable to remember afterwards, which is a
  new kind of credential
- the OpenZiti identity itself, recorded on first attach and refused if it changes, which works for ziti and has
  nothing to offer zrok private
- a shared secret sent on every hello, which is a bearer token by another name

Guessing here would invent a credential nobody agreed to, in the one area explicitly held back for its own
round of questions.

### What is done in the meantime

The hub says so at startup, in the log, whenever it is listening on a transport that does not prove which room
is calling. `certs.go` says the same where those tokens are minted. Nothing claims a guarantee it is not
keeping.

------------
