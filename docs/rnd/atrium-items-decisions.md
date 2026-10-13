# Atrium items: decisions

Decisions from clint's interview on 2026-10-13 about `docs/rnd/atrium-items.md` (branch `claude/items-spike`, 79cdf1a7).
Section numbers point at that doc. "Try it" marks a decision clint accepted for now and expects to revisit once it is in
use.

## 1. Where items live: the hub owns them (Option B, not A)

- The **hub owns** items. The room-owned design (Option A) is rejected.
- Rooms keep a **mirror** and record locally while the hub is unreachable, then push back to the hub when they can.
- The store is **SQLite** in `hubstore`, beside backlog and flow. Git as the item store was considered and rejected.
  Git keeps holding only what is already git: result shas, artifacts and `LEARNINGS.md`.
- Exactly-once across the two stores needs a room-side **outbox** keyed `(item_id, revision, state)`, so that a replayed
  push is a no-op on the hub. Section 8's argument that one transaction is enough no longer holds. The hub's write is
  the one that counts, and the notice is queued from the hub's transaction.

## 2. Blocked is a state, not a finish

- Section 2's state machine changes to: `queued -> running -> blocked -> running ... -> finished`. `blocked` is not
  `finished`.
- The owner (the entity that started the item, which atrium records) gets one notice from **atrium**, not from an agent,
  for each `blocked` and for `finished`.
- The owner unblocks with a **separate atrium tool**, for example `atrium_item unblock it-42 "<answer>"`. Atrium types
  the answer to the worker with sender `atrium`. The worker never learns who the owner is, so it never tries to reach
  them.
- Atrium confirms the unblock arrived: the item goes back to `running` only when the worker's turn actually starts from
  it.

## 3. Stuck detection and missed messages

- **Owner never answers a `blocked`:** after 15 minutes, or immediately if the owner's session has ended, atrium alerts
  the human that the item is stuck on its owner.
- **Worker never resumes after an unblock:** after 2 minutes, atrium re-types the unblock once, then alerts the human.
- Each alert goes out once per stall, on the board and through the existing human notice path, and is not repeated.
- **Context clear:** if the owner (holding a `blocked` or `finished` notice) or the worker (holding an unblock) clears
  its context before acting on the message, atrium re-delivers it **once**, marked "in case you missed it", then alerts
  for human intervention.
- This replaces section 5.3's implicit and explicit ack design. What matters is whether the owner acted, or whether the
  worker resumed, not whether a notice was "read".

## 4. Item workers cannot say anything

- A running item worker loses `atrium_say`, `atrium_peers`, `atrium_task` and `atrium_report`. The say endpoint also
  refuses a say from a running item card (section 4.3), with no exception for the operator or owner.
- If the human types into the worker's terminal and asks for status, the worker answers in its terminal. A real stop
  goes through `atrium_blocked` or `atrium_done`.

## 5. Who can be the owner (try it)

- By default the owner is the creating session.
- Opt-in: a **single-holder flow role** (`orchestrator`, `merger`, `director`), resolved to its current holder at
  delivery. `tester` has several holders, so it is refused as an owner.
- If nothing resolves at delivery, the stuck alert from section 3 fires.

## 6. The human as owner

- `human` is allowed as an owner. A blocked or finished item shows on the board.
- There is **no board unblock action**, because the human has no `atrium_say`. The human goes to the worker's terminal
  and types. When the worker's turn starts from that typing, the item goes back to `running`.
- The 15 minute owner timer does not apply to a human owner, because the board item is itself the alert.

## 7. Pools (try it)

- Pools feed **one item at a time per resident card**. The next item's brief is typed only after the current one is
  `finished`. A card with a `blocked` item counts as busy.
- More throughput comes from more resident cards in the pool. The hub hands each new item to an idle card.

## 8. LEARNINGS.md (try it, likely to need changing)

- The reviewer only **appends**, through `learned` on `atrium_done`. Only the human edits or prunes the file.
- The file is capped at about 50 entries or 20 KB, and atrium alerts when the cap is reached. Old entries are never
  dropped silently.
- clint expects this to become a blocker. The fallback: when the cap is hit, atrium gives the pool one compaction item
  that rewrites the file, and the human reviews that commit.

## 9. Opt-in

- Items are **opt-in per launch**. Non-item launches keep today's launcher, say and report behavior unchanged.

## 10. Items do not drive the merge flow

- "Done means done." A finished item does not move any flow stage. An item with a `backlog_id` only follows backlog
  status, as `followItem` does today.
- clint called the `implemented` stage "dumb", because it reads as a synonym for done. Whether that stage should stay
  in `hubstore/flow.go` `Stages` is a separate follow-up and is not decided here.

## Consequences for the plan in section 8

Not asked directly. This is the interviewer's reading of the decisions above:

1. Stage 1 (Option C, room-side, no migration) still applies unchanged: ending lines without "launcher", `atrium` as
   the opening sender, the item tool class, and say refused for an item card.
2. Stage 2 moves to the hub: the `item` table in `hubstore`, the room mirror and outbox, the states `running`,
   `blocked` and `finished`, `atrium_item unblock|get|list|cancel`, the stuck timers and the one-time re-delivery
   after a clear.
3. Stage 3: pools and LEARNINGS.md as described above.
4. The old stage 4 (a read-only hub mirror) disappears, because the hub is now the owner.
