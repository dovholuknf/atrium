# A say leaves a record, and a reply asked for stays owed

Backlog item 32 (`docs/backlog-2.md`). Design first, as that item asks.

## The incident

A session sent an `atrium_say` to handle `atrium`, asking for a backlog card and a reply. It was told `queued`.
Nothing came back. Afterwards nobody could say whether `atrium` reached the right card, whether the text was ever
delivered, or whether a `/clear` erased it after delivery. The only trace was the receiving card's event log, which
`atrium_task` shows for the last twenty entries and which says `prompted` without saying which say.

The cause is not knowable from what exists. The gap is that a say leaves no record of its own.

## What exists today

Read from the code, because the design has to start from what actually happens.

**Where a say is recorded.** Nowhere, as a say. There are three outcomes and they leave different traces:

| Outcome | Trace |
| --- | --- |
| Typed into the terminal (`handleMessage`, `tellByTyping`) | a `prompted` event, `via: terminal`, `from_peer`. No `message` row. |
| Queued (`QueueFromPeer`, `QueueAfterTurn`) | a `message` row and a `prompted` event `queued: true`. |
| Delivered later (`takeMessages`, `pendinginject`) | `message.delivered_at` and `via`, and a `prompted` event `delivered: N, via` that names no message. |

So a typed say has no id at all, and a queued one has an id the sender is given (`id` in the `/message` answer) and
`atrium_say` throws away. The `message` table is not swept and has no channel for "end of turn" versus "hook".

**How a handle resolves.** `handleSay` calls `localTarget`, and `handleTell` calls `resolvePeer`. Both are EXACT:

1. the wire name, qualified with this machine's tenant (`Store.GetByWireName`). It matches a card in ANY status.
2. an alias (`Store.GetByAlias`), live cards only, newest first.
3. a card id (`localTarget` only).

There is no prefix, no case-insensitive, no fuzzy step, and there should not be. `atrium` therefore resolved either to
a card whose wire name is exactly `atrium`, or to a live card whose alias is `atrium`. It could not have been a guess.
What went wrong is that the answer never said which. The CLI has one more resolver of its own (`cli.resolvePeer`, used
when the room is older than `/v1/say`) with the same three steps.

**What a miss answers.** `handleSay` and `handleTell` answer `404` with `error: "no session called X"` and the whole
roster under `peers`. `atrium_say` reads only `error`, so the sender sees `no session called atrium` with no candidates.
`atrium tell` prints the roster. The MCP tool's own fallback (`cli.resolvePeer`) does list names, but only on the old
path.

**Cross-room.** `handleSay` and `handleTell` send `name@room` to `sayAcross`, which asks the hub through the `Relay`
interface. A hub that does not answer holds the row in `relay_outbox`. A hub that answers hands back `delivered`,
`card` and `to`. The row in `relay_outbox` is deleted once sent, so it records nothing afterwards.

## The rule

**Every say from a session to a session gets a row, written when it is sent and updated as it moves.** The row is the
answer to "what happened to that". Both ends can read it.

A say from the operator's own channel (no `from`) is not recorded. It is the board's message box, it is high volume, and
the operator can see it arrive. The lifecycle is about one session speaking to another.

## The record: table `say`, migration `0069_say`

```
say(
  id            TEXT PRIMARY KEY,      -- ULID, returned to the sender
  from_task     TEXT NOT NULL DEFAULT '',   -- the sender's card, '' when it is not one here
  from_wire     TEXT NOT NULL,             -- as the sender named itself
  to_task       TEXT NOT NULL DEFAULT '',   -- '' when it never resolved, or lives on another room
  to_wire       TEXT NOT NULL DEFAULT '',   -- the handle it resolved to
  to_input      TEXT NOT NULL,             -- what the sender typed
  via           TEXT NOT NULL,             -- handle | alias | card | remote | none
  room          TEXT NOT NULL DEFAULT '',   -- the other room, when there is one
  door          TEXT NOT NULL,             -- say | tell | answer | ask
  preview       TEXT NOT NULL,             -- first 200 characters, and chars holds the full length
  chars         INTEGER NOT NULL,
  when_word     TEXT NOT NULL DEFAULT '',   -- immediate | done
  state         TEXT NOT NULL CHECK (...), -- see below
  channel       TEXT NOT NULL DEFAULT '',   -- terminal | hook | stop, once delivered
  message_id    TEXT NOT NULL DEFAULT '',   -- the queue row, when it was queued
  relay_id      TEXT NOT NULL DEFAULT '',   -- the relay_outbox row, when held for another room
  lapsed        INTEGER NOT NULL DEFAULT 0, -- a reply asked for that will never come
  note          TEXT NOT NULL DEFAULT '',   -- why a refusal, in a sentence
  reply_wanted  INTEGER NOT NULL DEFAULT 0,
  replied_at    TEXT NOT NULL DEFAULT '',
  reset_at      TEXT NOT NULL DEFAULT '',   -- the receiver cleared or compacted after delivery
  reset_kind    TEXT NOT NULL DEFAULT '',   -- clear | compact
  sent_at       TEXT NOT NULL,
  queued_at     TEXT NOT NULL DEFAULT '',
  delivered_at  TEXT NOT NULL DEFAULT ''
)
indexes on (to_task, sent_at), (from_task, sent_at), (message_id)
```

Timestamps are RFC3339 text, the state is `CHECK`ed, keys are text. It stays Postgres portable.

The FULL TEXT is not copied. It is already in `message.text` for a queued say, and copying it here would double what is
kept for no reader. The preview is what a person scanning a history needs, and 200 characters is enough to recognise
the say. A typed say's full text is in the `prompted` event, as it is today.

### States

A row is written at the moment it is placed, so the first state it can be seen in is `queued` or `delivered`. Sent
time is `sent_at`. `queued` -> `delivered`, or one of the ends that are not delivery:

The `sent` state is never written: the row is born in the state the say has already reached, and the CHECK still
allows `sent` for a later use. Only `say` and `tell` write rows. `ask` and `answer` write none: an `answer` only
settles the replies owed to the asker.

`lapsed` is NOT a state. It is a column (0 or 1) beside the state, set on a reply asked for that will never be answered
(the receiver ended, or seven days passed). A lapsed row is kept for history and no longer counted as owed. `relay_id`
is a column too, the `relay_outbox` row a cross-room say is held under.

| State | Meaning |
| --- | --- |
| `queued` | On the durable queue, waiting for the terminal to be free, the next tool call, or the end of the turn. |
| `delivered` | Reached the session. `channel` says how. |
| `held` | Cross-room only. This room could not reach the hub, and holds it in `relay_outbox` for up to 24 hours. |
| `handed` | Cross-room only. The hub took it. `note` carries what the hub said (`queued`, `terminal`). |
| `unconfirmed` | Cross-room only. The hub took it and did not say what became of it. Not held, not resent. |
| `refused` | Not sent, and `note` says why: the target has ended, the rate limit, too long. |
| `unresolved` | The handle matched nothing. `note` holds the candidates offered. Written so the sender can see its own miss. |

### Channels

`delivered` is a fact, and `channel` says by what:

- `terminal`: typed into the pseudo terminal, straight away or after waiting for the line to clear
  (`handleMessage`, `tellByTyping`, `pendinginject`).
- `hook`: carried by the permission hook on a tool call (`takeMessages` with `via: permission`).
- `stop`: carried by the Stop hook at the end of the turn (`takeMessages` with `via: stop`).

Nothing about delivery changes. A peer message is still QUEUED and never typed by a hook, and the permission chain is
untouched. This only writes down what already happens.

### Where it is written

Every write is in one of four places, and the store does the updating so a caller cannot forget it:

1. **Sent, resolved.** `handleMessage` (the one local path `handleSay` runs through) and `handleTell` create the row.
   `handleMessage` does not know what the sender typed or how it resolved, because `handleSay` resolves and calls it
   with a card id. So `handleSay` puts a `sayTrace{to, via}` on the inner request's CONTEXT. A context value, not a
   header: the inner request never crosses a wire, and a client cannot forge one.
2. **Queued.** The row takes `message_id` when the message is queued.
3. **Delivered.** `Store.MarkDelivered` already receives the message ids and the channel, and is the single door all
   three delivery paths go through. It updates the matching `say` rows in the same transaction. No daemon call site has
   to change, and none can be missed.
4. **Typed.** `handleMessage` and `tellByTyping` mark the row delivered with channel `terminal` at the point they write
   the `prompted` event.

Cross-room and the hold: `sayAcross` writes the row on THIS room's side, `state` `held`, `handed` or `unconfirmed`. A
held row records the outbox row's id in `say.relay_id` (`relay_outbox` is not altered), and `drainOnce` moves it to
`handed` when the hub takes it and to `refused` when it gives up. The receiving room writes its own row when the say
lands, from the same code as any local say, with `from_wire` `name@room`. Nothing in `internal/link` or `internal/hub`
changes.

## A miss answers with candidates

A handle that does not match exactly is never resolved to a near miss. That stays true and is now stated. What changes is
the answer, which `atrium_say` could not show: the `404` gains `candidates`, and its `error` sentence carries them.

A candidate is a live card whose wire name or alias CONTAINS the typed name (case-insensitive), or whose wire name begins
with it followed by `-`, which is the shape of `atrium` against `atrium-87300`. Ranked prefix first, then substring,
then by title. Capped at eight. When there are none, the sentence says so and points at `atrium_peers`. The sentence is:

```
no session called atrium. did you mean: atrium-87300 (@sa32), atrium-docs? none of these was messaged.
```

One helper, `candidatesFor(name, exclude)`, used by both `handleSay` and `handleTell`, so the two doors cannot drift. A
miss also writes a row with state `unresolved`, so the sender's own history shows the failed attempt.

A resolution that is exact but INDIRECT is reported, not blocked: `via` is `alias` or `card`, and the answer carries the
handle it went to. A sender who said `atrium` and read back `to: sg4/research-2, via: alias` can see it landed
somewhere unexpected. Blocking an alias would defeat item 35, whose whole point is that an alias is a handle.

Two facts worth stating and not changing here. `GetByWireName` matches a card that has ended, so an exact handle that
belongs to a dead card is refused as ended even when a live card holds the same word as an alias. That is the right
refusal, because the exact handle is what was asked for, and the row now records it as `refused`.

## Looking a say up

**The sender.** `atrium_say` returns `id` (the say id), `via` and `state`, next to what it returns now. That is the
handle to ask about later.

**Either end, afterwards.** `atrium_task` gains `says` (bool). With it, the output carries `says`: the last twenty says
this card sent or received, newest first, each with id, direction (`sent`, `received`), the other end, `via`, `state`,
`channel`, the three timestamps, `preview`, `reply_wanted`, `replied_at`, and `reset_kind` when a clear or compact
followed delivery. It is a field on an existing tool rather than a new tool, since the model already asks `atrium_task`
what a card is doing. The store call is `SaysFor(taskID, limit)` and the human API gets `GET /v1/tasks/{id}/says`.

Anybody who can call `atrium_task` on a card can read its says. That is the same trust the peer bus already has: no
auth, loopback, one operator. The preview is 200 characters, and this is stated rather than gated.

## A clear or a compact no longer drops a say silently

The item asks for an owed mark. Before that, a smaller thing closes half the incident: the daemon is told when a session
clears or compacts (`SessionEvent` `end` with reason `clear`, and `compact`). On either, every `delivered` say to that
card whose `reply_wanted` is unanswered, AND every delivered say from the last ten minutes, gets `reset_at` and
`reset_kind`. Then the record can say `delivered 09:22:10 via hook, receiver cleared 09:31:00`, which is the sentence the
operator needed. It costs two calls in `session.go`.

## A reply asked for stays owed

### What "asks for a reply"

The smallest way to know is to be told. `atrium_say` gains `reply: true` ("I need an answer to this, not just a
delivery"). `atrium tell` gets `--reply`. No inference from the text: guessing that a message is a question is exactly
the guess `promptWasPeer` already makes for a different purpose, and it was wrong often enough to be a warning.

### What "answered" means

The row is answered when the RECEIVER sends a say back to the SENDER, by any door (`say`, `tell`, `answer`), after the
original was sent. All open owed rows from that sender to that receiver are settled together, and each is stamped
`replied_at`. It is the same shape as `AnswerAsksFrom`, which settles only the questions a given peer was asked, and for
the same reason: a reply to one session says nothing about a question put to another.

It is deliberately not tied to the reply's content. A model cannot mark which say it is answering without a way to name
one, and asking it to pass a say id back is a second thing to remember at the moment it is already at risk of forgetting.
If two are open from one sender, one reply settles both, which is the honest reading of "they answered me".

### What "owed" looks like

`Task.RepliesOwed` (computed, not a column, like `AsksOpen`) is the count of unanswered, un-lapsed rows on the receiving
card, counted for the whole list in one query. It rides the task JSON as `replies_owed`, and `atrium_task says` lists the
rows with `reply_wanted` and no `replied_at`. A row is open until it is answered, until its receiver ends (`done` or
`dead`, then `lapsed`), or until seven days pass (`lapsed`, by the sweep below).

This is a record on the card. It is NOT a stuck badge, not a column move, and not a notice. `owed_at` and
`docs/owed-report-design.md` are about a report owed to a LAUNCHER and drive the STUCK escalation. A reply owed to any
sender is a different debt with a different creditor, so it has its own table, its own count and no path into
`OwesReport`, `stuckNow` or `PromptKey`. `owed_at` is not read or written by anything here.

### What it does not do, and the Open Question

It does not put the owed reply back in front of the model after a `/clear`. The mark exists and the operator or the
sender can see it, but the session that lost the text does not know it lost it. Closing that would mean a reminder
delivered on the next hook, and that is a new message the receiver did not ask for, sent by atrium on a schedule.
Whether it is worth the interruption is clint's call, so it is written down and not built:

> **Open Question for clint.** When a session clears or compacts while a reply is owed, should atrium tell it? The
> option is one queued message at the next `SessionStart`: "you were asked for a reply by X at T and have not sent one,
> here is the preview". It would repair the incident at the cost of atrium speaking to a session unprompted. Left off
> until asked for.

The board does not draw a chip either. The count is in the JSON, and drawing it is a separate change to a file this one
does not need to touch.

## Bounded

Three limits, one sweep:

- `preview` is 200 characters and `note` 300.
- A row older than 30 days is deleted, unless it is an open owed reply younger than 7 days.
- No more than 2000 rows are kept. On the sweep, the oldest beyond that go, open owed replies excepted.
- An owed reply older than 7 days becomes `lapsed` (kept for the 30 days, no longer counted).

The sweep is `Store.SweepSays`, called from the tick that already sweeps the dispatch queue (`reaper.go`). It is not a new
timer. Inserting a row never trims, so the hot path does no extra work. A `guard`ed store call, so it halts like every
other.

## What does not change

- The delivery of a peer message. Queued, never typed by a hook. `deliverPeerWhen`, `takeMessages` and
  `pendinginject` keep their behaviour; only `MarkDelivered` gains an UPDATE.
- The permission chain and its order.
- `owed_at`, `OwesReport`, the STUCK escalation.
- `internal/link`, `internal/hub`, and `nosession.go`.
- The operator's channel. No `from`, no row.

## A row is best effort, and a say is not

A failure to write the row is logged and the say proceeds. A message not sent because its own bookkeeping failed is the
worse outcome, and the store halting is already the answer to a database that cannot be written. The exception is the
store being halted: then the agent listener is closed anyway.

## Known limits

- The record says a say was delivered to the session, not that the model read it or kept it. That is unknowable from
  outside, and `reset_kind` is the nearest honest signal.
- A say the receiver answers by typing to the sender's terminal, or by a channel that is not a say, does not settle the
  row. It lapses after seven days.
- A sender on another room has no row on ITS side that moves from `handed` to `delivered`. The hub returns one word at
  handoff. See "What the hub would need" in the report: a delivery receipt on the relay, and a say id on the request, so
  the receiving room's row and the sender's are the same row.
- `via: none` and `unresolved` rows have no `to_task`. They show in the SENDER's history only.
