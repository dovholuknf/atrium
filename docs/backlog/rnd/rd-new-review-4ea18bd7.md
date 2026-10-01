# Design review: room handoff, `atrium move` (4ea18bd7), and the rnd doc commits (m1mini, 2026-10-01): HOLD

Range claude/main 6bce194f..claude/rnd 4ea18bd7, 13 commits. One is the design, `docs/rnd/room-handoff-design.md`.
The other twelve are rnd queue and backlog docs: the pause and landing notes, the four 2026-10-01 filings, and the
r-new-move-card copy from sg4. Those twelve are fine as filed, nothing to hold. The room-to-room access spike will
put keys between machines, so its design comes to @review for a security read before anything is built. A design
review, so no trailers. Nothing gets built.

The design is good in shape. One hub-driven verb is right, check-then-move is the right split, the conversation is
carried with a handoff as fallback, and it is honest that the director move needs a landing route. It holds on two
gaps in section 3 that lose work in exactly the case it is for: a card that keeps working during a move.

## High 1: step 2 carries the branch before step 3 waits for idle

Section 3 carries `claude/<branch>` at step 2 and only then waits for the turn to end at step 3. Step 1's hold says
"the card keeps working". Any commit the card makes between steps 2 and 3 never reaches B. Any file it writes after
the check's clean-tree row (2) is neither committed nor refused. The successor resumes a conversation that
remembers that work, on a worktree that does not have it.

Fix: wait for idle first, then freeze (High 2), then re-run the clean-tree check, then carry the branch. The check
of section 2 is a forecast: every row that can change (idle, clean, cap, destination up) is re-checked at the moment
it is used.

## High 2: "hold" does not freeze the old card, so its last turns are lost or forked

Between step 5 (the transcript is copied) and step 7 (the old card exits), the old card is a live session. Only
"new says" are queued. Reports and stop or context notices come through `launcherOf` and `notifyLauncher`
(daemon/a2a.go:243-265), not the say queue, so they are typed in. So is anything a human types into its terminal on
the board. Any turn the old card takes in that window is missing from the successor's copy. At step 6 both
sessions are live and both may act on the same instruction: the fork the `ResumeBusy` guard exists to prevent
(daemon/launch.go:503-511), split across two machines.

Fix: from step 4 (capture) to the cut-over, the old card is frozen. Its terminal input is refused (the
new-context cycle already refuses input: terminal-links.js:1859 "input is refused during a new context"), and says,
reports, notices and nags all go to one queue. That queue is forwarded at the cut-over, or replayed on A if the move
fails. Also re-check step 3's idle after the freeze lands.

## Answers to the four questions

**Section 2, does the check refuse before anything moves?** For a check failure, yes: the whole list is checked
first. But two holes:
- **A list moves one card at a time** (section 3). A failure at step 6 for card 3 leaves cards 1 and 2 cut over.
  "All five move, or nothing moved" (M3's acceptance) is only true for check failures. Either run steps 1 to 6 for
  every card before any cut-over, then cut all over, or say that a runtime failure stops the list and reports which
  cards moved. The first is better and fits the hub driving it.
- **The check is not re-checked** where it is used (High 1). Smaller: row 11 keys "lands work" on a tag. A director
  without `role:director` or `lands` skips the landing check and arrives on B unable to land, which is the
  2026-10-01 halfway state. Key it on the card's lineage (launched by the orchestrator) or require the tag on every
  director.

**Section 3 step 7, is anything delivered twice or lost?** With High 2 fixed, the hold queue is the one place
messages wait, which is right. Four gaps remain:
- **The alias gap.** A releases the alias, then B sets it. A say by alias in between answers "no session called".
  It is refused, not silently lost, but the sender has to retry by hand. Let B take the alias first through the hub
  (the hub can order it, and `resolvePeer` sees one live holder because A's card is frozen and about to be done), or
  have alias lookup follow `alias_note`'s moved_to.
- **Exactly once needs an id and an ack.** "Forwards them exactly once, and drops them from its own queue" has no
  mechanism. A hub restart mid-forward either loses the queue (dropped before B has it) or doubles it (resent). Keep
  each say's id, have B dedup by it, and have A drop a say only after B acks.
- **No move record.** The hub drives a multi-step operation across two rooms with nothing persisted. A hub restart
  between steps leaves a frozen card on A, a successor on B, or both. The hub keeps a move record per card (step
  reached, ids), resumes or undoes it at start, and makes every cut-over step idempotent.
- **Cut-over order.** Set `moved_to` and freeze-forwarding on A first, then B's alias and pins, then the child
  re-point, then A exits. Every step is retried from the record. Say what happens if the successor dies after
  answering but before the cut-over finishes: the record undoes it and A is unfrozen.

**Section 5, the moved_to forwarding.** Right as a single rule. Three points:
- `launcherOf` resolves `report_to` first (`currentLauncher`, a2a.go:250), then `spawned_by_id`. `SetLauncher` writes
  only `spawned_by` and `spawned_by_id` (store/a2a.go:79). So the step 7 re-point fixes the fallback. A child whose
  `report_to` names the old card by id or handle still lands on it. Make the follow-`moved_to` rule apply to
  `currentLauncher`'s answer as well as the fallback. The design's wording ("when the launcher it finds") covers it
  if both paths go through one lookup. Say so, and test a child with `report_to` set to the old card's handle.
- Follow the chain with a hop limit and a cycle check (A to B and back to A is a normal case, and a bug there should
  not spin).
- A handle on A can be reused by a later card once the old one is done. A `report_to` by handle could then resolve
  to the new stranger and not follow. Resolve by id where one is stored, and keep the old card's handle reserved
  while `moved_to` is set.

**Section 6, is the interim landing route safe as a rule?** Safe only with four rules the improvised route keeps by
habit today. Write them into the hub op:
1. **One writer per room.** Every m1mini worktree shares one clone and one `claude/landing`. Two directors landing
   at once race on it. Either one landing role per room (today @review) or a lock in the op. Per-director landing
   branches would also do.
2. **Fast-forward only, and reset after.** The hub moves `claude/main` only by fast-forward to the landing tip. After
   each landing, `claude/landing` is reset to the new `claude/main`, so a stale or refused commit does not ride
   along into the next landing.
3. **A mechanical verdict check, not an eye.** Before the fast-forward, every commit in `claude/main..landing` must
   be covered by an `Atrium-Verdict` OK range (the trailer shape the deploy reader takes,
   docs/rnd/factory-shape.md rule 2), or be a verdict commit itself, or be a doc commit under a stated
   exception. "@review has cleared it", checked by the orchestrator reading, is how today works, and it does not
   scale to a rule.
4. **Two rooms' landing branches.** Both cut from `claude/main`. The first to land makes the second non-ff. Its
   commits must be rebased, which changes the SHAs the verdicts name. Say that a verdict re-stamp is needed after a
   rebase, or land one room at a time.

## Lower

- The project memory folder (`~/.claude/projects/<encoded cwd>/memory/`) is not in the carry list. A director's
  memories stay behind on A. Carry it with the transcript (section 3 step 5, section 4).
- The files B writes from the stream (`BRIEF.md`, `HANDOFF.*.md`, the jsonl) go through `internal/safepath` and an
  allowlist of names, since the hub writes into B's worktree and home.
- Row 7's "B uses its own values": launch-time env values (not the room's) are not reproduced. Say that a card
  launched with a per-card value gets B's room value or none, and that row 7 checks the room env only.

## Verdict

HOLD on the design: High 1 (step order) and High 2 (the freeze), plus the list's all-or-nothing and the move record,
before M1 is sent to @runtime. The rest are clarifications. The doc commits in the range are fine. A re-read is the
section 2, 3, 5 and 6 changes.

Quality: strong. It is grounded in file:line facts, takes over f-004 section 3 cleanly, and answers the 2026-10-01
failure directly. The gap is concurrency: the old card is treated as still while the move runs, and the hub as
never restarting.

## Re-read at 3195e7c0 (2026-10-01, m1mini): OK

One commit over 4ea18bd7, the design only. Every point of the HOLD is answered.

- **High 1, closed.** 3.2 waits for idle (1), freezes (2), checks idle and clean again (3), and captures (4) before
  the branch carry (5). The tree is final when it is carried.
- **High 2, closed.** The freeze runs from 3.2 step 2 to the cut-over. It refuses input as a new context does, and
  says, reports, notices, nags and wakes all go to one queue with ids, so nothing is typed in. Parking the old runner
  (step 7) before the successor resumes (step 8) means one live session per conversation at every moment. That is
  stronger than I asked for.
- **The list, closed.** Prepare all, then cut all over, and a failure in phase 2 undoes every card newest first. The
  cost is a whole list frozen together, now Q6 for clint.
- **Re-checks, closed.** A per-row "checked again" column. Row 11 fails closed for any worktree card unless
  `--no-land`, so it no longer depends on a tag.
- **Step 7, closed.** B takes the alias first, and `resolvePeer` prefers the card a `moved_to` names, so there is no
  gap. Forwarding is by id, with dedup and an ack before A drops. The move record has idempotent steps (the
  successor's `task_id` comes from the move id), resume or undo at start, a lease so A can undo itself, and a rule
  each side of `moved_to` for a successor that dies.
- **Section 5, closed.** One lookup for `sayGate`, `currentLauncher` and the fallback, 8 hops, a cycle check, and the
  handle reserved.
- **Lower, closed.** The memory folder is carried, B writes through safepath and an allowlist, and per-card env is
  stated as not reproduced.

### Conditions on M3 (the landing op), not a hold on the design

1. **Design and doc commits have no verdict trailer.** Section 6 rule 3 refuses any commit not covered by an
   `Atrium-Verdict` OK range, except QUEUE.md. @review's rule is that design and doc reviews carry no trailers, so
   this design, the backlog filings in this range and every later doc commit could never land through the op. Add a
   trailer kind for them, for example `Atrium-Verdict: doc-ok <base>..<tip>`, and @review starts writing it. A wider
   exemption would also do it, but it is the weaker fix.
2. **"Every non-merge commit" lets a merge carry unreviewed content.** A merge whose tree differs from the clean
   merge of its parents (a hand conflict resolution, or an amended merge) adds changes no verdict covers. The check
   accepts a merge only if its tree equals `git merge-tree` of its parents. Otherwise its own diff needs coverage.
3. **State the trust.** Trailers are text, and m1mini and sg3 commits are unsigned, so any director can write one.
   The check catches mistakes, not a hostile director. Count a verdict only from a commit that touches nothing but
   `docs/backlog/*/*-new-review-*.md` (or `review-new-*.md`), and say that signing @review's commits is what would
   make it a guarantee.

### Lows

- The lease against `moved_to`: A's self-undo when the lease lapses, and the hub's step 1, race. A refuses to set
  `moved_to` once its lease has lapsed, and undoes only while `moved_to` is unset, both under one lock on A.
- In step 8 the successor is a live session with tools, told not to act until the cut-over note. Launch it
  with the gate on and nothing auto-approved until the cut-over, so being "told" is not the only guard.
- Between 3.3 step 3 and step 5 the old card is not done yet. Say that anything arriving then still enters the
  queue and is forwarded. The text says "from now on its freeze queue forwards", which covers it if the queue stays
  open until step 5.

Verdict: OK. The design can go to clint with Q1 to Q6. M3's landing op must take conditions 1 to 3. The doc commits
in claude/main..claude/rnd are OK as filed.
