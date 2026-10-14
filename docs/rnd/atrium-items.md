# Atrium items: a task whose only routing is "notify X when complete"

Design spike for backlog `r-items-no-chatter`. Written on `claude/items-spike` off `3ec37f46` on 2026-10-13. Design
only: nothing here is built. Every claim about today's code names the file and line it was read from.

## 1. The problem: cross chatter

Today a launched worker knows who launched it and has a channel to that session for its whole life.

- **It is told who its launcher is.** The opening prompt is queued with the launcher as its sender
  (`internal/store/a2a.go:234`), and a peer's text is typed as `@<from> says: "..."`
  (`internal/daemon/messages.go:52`).
- **It is told to talk to it.** Every launch ends with "Before you end your turn, tell your launcher either ... Report to
  your launcher" (`internal/daemon/launch.go:1747`, the same words for the hub's control in
  `internal/link/control_mcp.go:1270`). A resident card is told the same (`launch.go:1756`).
- **It has the tools to do so at any time.** The worker tool class includes `atrium_say`, `atrium_peers`,
  `atrium_report` and `atrium_task` (`internal/link/ctlclass.go:44-56`). `atrium_say` is "IMMEDIATE BY DEFAULT ... typed
  in ... EVEN MID-TURN" (`control_mcp.go:189-193`). The class is "NOT A BOUNDARY" and a failed lookup serves the full
  set (`ctlclass.go:14-17`, `ctlclass.go:125-130`).
- **Its launcher can talk back mid-flight**, and every launcher-to-worker say makes the worker owe a fresh report
  (`promptOwes`, `store/a2a.go:232-242`), which re-arms the silent-stop nudge and the owed item
  (`internal/daemon/owed.go:13-23`).

Atrium already spends real code damping the result: one message per end event (`internal/daemon/endonce.go:11-17`), a
say that repeats an end is dropped (`endonce.go:61-85`, called from `internal/daemon/peers.go:406`), a repeated done
sends no second notice (`endonce.go:87-103`), orchestrators hold their notices (`internal/store/ledger.go:896-911`), and
a report waits for the launcher's turn end (`ledger.go:912-914`). Each is a patch on a channel that should not exist.

**Example, shaped like this very spike's launch.** The orchestrator launches three reviewers on one PR. Each brief says
"Ask with atrium_say rather than guessing". Reviewer A says "starting, reading internal/link" (a `needs` say, typed
mid-turn into the orchestrator). The orchestrator answers "focus on ctl_end.go", which is an instruction that makes A owe
a report again. Reviewer B calls `atrium_peers`, sees A, and says to A "are you covering ctl_end.go?". A answers B. A
ends with `atrium_say "done 1a2b3c4"` and then `atrium_done 1a2b3c4`; endonce keeps that to one notice, but the
orchestrator has now taken four interruptions for one result, B and A have spent two turns on each other, and after the
orchestrator's `/clear` the `listOwed` line (`owed.go:336-366`) lists items it had already read. Nobody did anything
wrong: the tools and the ending lines invited all of it.

## 2. The item model

An **item** is a durable record of one task. The worker sees the brief and nothing about where the result goes.

### Fields

| Field | Holds | Written by |
| --- | --- | --- |
| `item_id` | `it-<n>` or a given id, never reused. Its own key, NOT the card id (see 4.1) | creator, once |
| `brief` | The task text, bounded like `MaxWorkBrief` (`ledger.go:72`) | creator, once |
| `notify` | One target: a card handle, `name@room`, a role (`flow` role, `hubstore/flow.go:77`), or `human` | creator; operator may change |
| `created_by` | Who made it. Provenance, like `launcher_id` today (`ledger.go:86-90`) | atrium |
| `assignee` | The card running it, or empty while queued | atrium |
| `pool` | Optional: a resident card or pool name items are fed to in order (section 6) | creator |
| `state`, `state_at`, `revision` | See below. `revision` as today's `work_item.revision` (`ledger.go:97-101`) | atrium only |
| `result` | `{kind: sha|artifact|text|blocked, sha, branch, paths, text, line}`, the shape of `WorkOutputs` (`ledger.go:133-142`) plus text | worker, once per revision |
| `learned` | Optional one paragraph the worker leaves for its pool (section 6) | worker, with the result |
| `delivered_at`, `delivery_msg` | When the one notice was typed or held, and its message row | atrium |
| `acked_at`, `acked_by` | When the notify target took it | notify target, or atrium on its behalf (5.3) |
| `backlog_id` | Optional link to a backlog item, so `followItem` (`internal/link/backlog_mcp.go:296`) moves it | creator |

### States

```
queued --assign--> running --done--> finished --deliver--> delivered --ack--> acked
                      |      --blocked-> finished(blocked) ...same...
                      +--session ends with no result--> finished(ended)  ...same...
delivered/acked --reopen(answer)--> running (revision+1, same assignee)
any open state --cancel--> cancelled (delivered once, as "cancelled", if it was running)
```

`finished` is reached once per revision, and that is the only state that produces a notice. Nothing else the worker or
atrium does while `running` produces anything for the notify target: no progress, no silent-stop, no long-turn, no owed
push. `finished(ended)` replaces today's `ended-without-report` notice (`ledger.go:37`, design step 4 in
`docs/runtime/work-ledger-design.md`) for item cards: it rides the same single delivery.

### Who can read and write what

| Party | Read | Write |
| --- | --- | --- |
| Worker | its own `brief` (as BRIEF.md and the opening prompt), its own `revision`. Never `notify`, `created_by` or another item | `result` and `learned`, once per revision, through `atrium_done` / `atrium_blocked` |
| Notify target | the whole item | `ack`, `reopen` (with an answer), `cancel` |
| Creator | the whole item | create, `cancel` |
| Operator (board) | everything | everything, including changing `notify` |
| Other workers | nothing | nothing |

The worker's opening prompt is sent with sender `atrium`, not the creator (`store/reserved.go:9-15` already reserves that
handle), so `messages.go:52` never prints a launcher name.

## 3. Mapping onto what exists

### Replaces

- **The launcher as an addressable party for an item card.** `launcherOf` (`internal/daemon/a2a.go:253-272`) resolves
  `notify` instead of `spawned_by` / `report_to` for an item card. `report_to` (`internal/daemon/reportto.go:11-37`) is
  the nearest existing thing and is room-local only; `notify` is its generalisation.
- **The ending lines** (`launch.go:1747-1758`, `control_mcp.go:1270-1281`) for item cards become: "End with
  atrium_done <sha or artifact> [line] or atrium_blocked <reason>. Atrium records the result. There is nobody to
  message." No "launcher" word at all.
- **The `ended` / silent-stop / owed notices for item cards** (`owed.go:46-79`, `a2a.go:274+`): suppressed while running,
  folded into the item's one delivery when it finishes.
- **`endonce.go` for item cards**: unnecessary, because an item card has no say to echo.

### Reuses as is

- **`atrium_done` / `atrium_blocked`** (`internal/link/ctl_end.go:126-180`) unchanged on the worker side. They already
  carry exactly the result shape: sha or artifact plus a 20 word line, or a 50 word reason. The room's checks stay:
  a commit has to exist in the worktree or on the hub (`internal/daemon/finish.go:243-267`), an artifact path has to
  exist (`finish.go:283-294`).
- **`finish` and `RecordReport`** (`finish.go:313-520`, `ledger.go:1055-1133`): the report, the item move and the notice
  already commit in ONE transaction. An item adds its own row's update to that transaction.
- **The notice queue and its dedupe**: `queueNotice` (`ledger.go:881-946`) with `claimNotice` on
  `(worker_id, source, key)` (`ledger.go:948-956`). Held for an orchestrator (`ledger.go:898`), typed at turn end for a
  report (`ledger.go:914`), held in the freeze queue (`ledger.go:917`), relayed to another room in the same transaction
  (`finish.go:435-439`, `ledger.go:958-978`).
- **Duplicate reports**: `reportClientID` makes a retried report one row per generation (`ledger.go:1196-1205`).
- **Delivery records**: `MarkDelivered` stamps `delivered_at` and the turn it started (`store/messages.go:175-200`,
  `store/delivery.go:9-30`).
- **Resident cards** (`internal/daemon/exitonreport.go:56-58`, resident tool words `ctl_end.go:43-48`): the shape for a
  reviewer that takes item after item.

### Stays as it is

- **`atrium_report`** (`control_mcp.go:224-240`): stays for non-item launches. An item card's class does not get it.
- **`atrium_say` between sessions that are not items**: directors, orchestrators, humans. Items do not remove say from
  atrium, only from item workers.
- **The work ledger** (`ledger.go:16-30`) for ordinary launches.
- **Backlog** (`internal/hubstore/backlog.go:88-101`): a backlog item is a *want*, an atrium item is *one assignment of
  work*. An item may name `backlog_id`; finishing it calls the existing `followItem` (`backlog_mcp.go:296-305`,
  already called on every recorded end, `control_mcp.go:1049-1051`).
- **The merge flow** (`hubstore/flow.go:44-45`, stages `idea ... pushed-by-clint`): a flow stage is where a *backlog
  item* is; an atrium item finishing `done` with a sha may report `implemented` for its `backlog_id`, as an orchestrator
  does by hand today. Flow roles (`flow.go:77-130`) are a good `notify` target: "notify whoever holds `merger`".

## 4. What has to change in the code

### 4.1 One item per card is not enough

`work_item` is keyed on `task_id`: `INSERT ... ON CONFLICT (task_id) DO NOTHING` (`ledger.go:298-305`), and
`ledgerReport` finds the item by card (`ledger.go:1138`). A resident reviewer that runs items 1..N would fold all N into
one row. Items need their own key and a card may hold many, at most one `running`. This is the largest single change.

### 4.2 The report notice key is the moment, not the item

`finish` keys the launcher's notice on `time.Now()` (`finish.go:432`). Exactly-once today rests on `reportClientID`
catching a retry (`ledger.go:1114`: no notice on a duplicate). For an item the key becomes `(item_id, revision,
"result")`, so a second `atrium_done` for the same revision, a crash and retry, or a replayed hook cannot queue a second
notice even if its words differ.

### 4.3 The worker class must lose its address book

`workerTools` (`ctlclass.go:44-71`) gives every worker `atrium_say`, `atrium_peers`, `atrium_task`, `atrium_report`.
An item class keeps `atrium_done`, `atrium_blocked`, the git, files, CI and resources tools, and drops those four.
Because the class is not a boundary (`ctlclass.go:14-21`), the say endpoint must ALSO refuse a say whose sender is a
running item card (beside `endSayDropped`, `peers.go:406`): "you are on an item: end with atrium_done or atrium_blocked".

### 4.4 Nothing about the target in the worker's world

- opening prompt sender `atrium` (2, last paragraph);
- BRIEF.md gets the item id and the brief, nothing else;
- the card's `spawned_by` stays for provenance on the board but the worker's own `atrium_task` is gone (4.3).

## 5. Delivery and replay

### 5.1 Exactly once, by construction

1. `atrium_done`/`atrium_blocked` reaches `finish` (`finish.go:313`). In ONE transaction (`RecordReport`,
   `ledger.go:1055`): the result is written, the item moves `running -> finished`, and a notice is claimed on
   `(item_id, revision, "result")` and queued (or held, or relayed). A refused end (bad sha, missing artifact) records
   nothing, as today (`ctl_end.go:31-34`).
2. A second end for the same revision: the item is already `finished`, the claim is taken, nothing is queued. The worker
   is told "already recorded" so it stops.
3. Delivery is the queue's existing job. When the row is typed or carried by a hook, `MarkDelivered` sets
   `delivered_at` (`messages.go:176-199`); the item copies it and moves to `delivered`.
4. An item that ends with no result (process gone) moves to `finished(ended)` in the transaction that records the exit,
   as the ledger does today for `ended-without-report`, and is delivered by the same step 3.

The notice text is the item, not a chat line: `item it-42 done 1a2b3c4d on claude/x: <line>. atrium_item get it-42`.
The worker's handle is in it for the human, never the other way round.

### 5.2 Nothing mid-flight

While an item is `running`, every source that today writes to a launcher checks for an item first and writes nothing:
`notifyLauncher` (`a2a.go:274+`), the owed push (`owed.go:21-23`), the `listOwed` line (`owed.go:336-366`), the
long-turn and context notices. They still show on the board; the notify target is not a launcher of a running item.

### 5.3 Ack

`delivered` is "it was put in front of the target". `acked` is "the target took it". Two ways in:

- explicit: `atrium_item ack <id>` (or `reopen`, `cancel`, which ack too);
- implicit: the turn that the delivery started ends (the delivery-to-turn join already exists, `delivery.go:9-12`).
  A turn that started from the notice and ended has read it.

Recommended default: implicit, with explicit available. An orchestrator that holds notices only reads them via
`atrium_task notices` (`control_mcp.go:265-271`); reading an item notice there acks it.

### 5.4 `/clear` and new contexts

Today a new conversation gets one line listing every open owed item (`session.go:450-452` calls `listOwed`,
`owed.go:336-366`), and `READING NEVER CLOSES IT` (`owed.go:19-20`), so a launcher hears the same items after each clear.
For items:

- `delivered` but not `acked`: listed once in the new context, as references (`it-42 done, it-43 blocked`), not re-typed
  in full.
- `acked`: never listed, never re-typed. That is the rule the brief asks for.
- `running` / `queued`: not listed. The target asks with `atrium_item list` if it wants them.

### 5.5 Restart

The queue, the claim and the item rows are SQLite (`ledger.go:926-929`), committed before any typing, so a daemon or
machine restart delivers an undelivered row once when the target is up (restart-wake, `internal/daemon/restartwake.go`)
and never re-delivers a delivered one. A target on another room is the relay outbox's job, held up to a day
(`control_mcp.go:208-210`). A target card that is gone resolves again at delivery (`a2a.go:257-262`); if nothing
resolves, the item is delivered to the orchestrator held, as `orphanReport` does today (`finish.go:467-471`).

## 6. Review agents that learn across items

Two pieces, both cheap given the above.

1. **A pool is a resident card fed in order.** Items created with `pool: reviewer-go` queue on the hub. Atrium types item
   N+1's brief into the resident card only after item N is `finished`, as the next prompt from `atrium`. The card keeps
   its conversation, so item N+1 sees items 1..N in context for as long as the context lasts. Residents already stay
   open after a done (`exitonreport.go:45-48`, `ctl_end.go:43-48`). Serial, one `running` per card (4.1), so there is
   never a mid-item interruption.
2. **Learnings survive a new context.** Each `atrium_done` on a pool item may carry `learned` (one paragraph). Atrium
   appends it to `LEARNINGS.md` in the pool's worktree and commits nothing; on SessionStart after a `/clear` or an
   automatic new context (the same hook point as `listOwed`, `session.go:450-452`), the resident is typed: "You review
   for pool reviewer-go. Read LEARNINGS.md (N entries) before the next item." A pool may also be relaunched fresh and keep
   learning, because the file, not the conversation, is the memory. The operator can edit or prune it.

The reviewer never learns who its items are for. Its learning is about the code and its own misses, which is the
point.

## 7. Options

| | What | Cost | Gets |
| --- | --- | --- | --- |
| **A. Items on the room's ledger** | New `item` table in the room store beside `work_item` (many per card), item key on notices (4.2), an item tool class and say refusal (4.3), suppression of mid-flight notices (5.2), ack and clear rules (5.3, 5.4), `atrium_item` for targets, pools on resident cards (6) | ~1 migration, ~1.5-2k lines with tests, 4 stages. Reuses `RecordReport`'s transaction and the relay. Cross-room items work through the relay, but a pool lives on one room | Everything in the brief. Exactly-once is one transaction, as reports are today |
| **B. Items on the hub** | `item` table in `hubstore` beside backlog and flow; rooms post results to the hub; the hub delivers by say/relay | Same tool and class work as A, plus a second write path (room records, hub delivers), so a crash between the two needs an outbox and retry. ~2.5k lines | One place for items across rooms, pools spanning rooms, board view for free with backlog. Exactly-once is harder: two stores |
| **C. Convention only** | Launch with an item flag that swaps the ending lines, sends the opening prompt as `atrium`, drops say/peers from the class, and keys the report notice on the card's generation | ~300 lines, no migration | Most of the chatter gone. No ack, no `/clear` rule, no pools, `work_item` still one per card, the class is still not a boundary |

## 8. Recommendation

**Option A, staged, with C as its first stage.**

1. **Stage 1 (C, room-side):** item launches: ending lines without "launcher", `atrium` as opening sender, item tool
   class, say refused for a running item card, notice key `(card, generation, "result")`. Ships the chatter fix in a
   week and needs no migration.
2. **Stage 2 (room-side):** the `item` table, many per card, `finished/delivered/acked`, mid-flight suppression, the
   `/clear` rule, `atrium_item get|list|ack|reopen|cancel` for targets.
3. **Stage 3:** pools on resident cards, serial feeding, `learned` and LEARNINGS.md.
4. **Stage 4 (hub-side, read only):** the hub mirrors item rows for the board and for `backlog_id` follow, as it mirrors
   cards today. The room stays the writer, so exactly-once stays one transaction.

Rejected B as the base because the room already holds the one transaction that makes "record and notify" atomic
(`ledger.go:1053-1133`), and moving the record to the hub splits it. The brief says "a durable task record on the hub";
the hub gets the record as a mirror in stage 4, which is enough to see items from any room. That is a deviation from the
brief's wording, not from its intent, and question 1 asks clint to confirm it.

## 9. Open questions for clint

1. Does the item's source of truth live on the room (with a hub mirror) rather than the hub? **Default: room, mirrored
   to the hub.**
2. Is a blocked item finished (delivered once, target reopens with an answer), or does it stay running and wait for an
   answer? **Default: finished; reopen with the answer bumps the revision.**
3. Implicit ack (the turn the notice started ended) or explicit `atrium_item ack` only? **Default: implicit, explicit
   available.**
4. Should a running item card be refused `atrium_say` outright, or allowed to say the operator only? **Default: refused
   outright; the human types into its terminal if needed.**
5. Can `notify` be a flow role (`merger`, `reviewer`) resolved at delivery time, not a card? **Default: yes, card or
   role.**
6. Is `notify` allowed to be the human (board toast, no session)? **Default: yes, `human`, acked when the operator opens
   it.**
7. Do pools feed strictly one item at a time per resident card? **Default: yes, serial.**
8. Who may prune LEARNINGS.md: only the operator, or the reviewer too? **Default: operator only; the reviewer appends.**
9. Do non-item launches keep today's launcher chatter unchanged? **Default: yes; items are opt-in per launch.**
10. Should an item finishing `done` with a sha move its `backlog_id`'s flow stage to `implemented` automatically?
    **Default: no, the flow stays reported by its role holders; the item only follows the backlog status as today.**
