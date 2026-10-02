# Review: review on atrium, amended for standing reviewers (2e9cddd0, m1mini, 2026-10-02): HOLD

The latest on claude/rnd, `docs/rnd/review-on-atrium-design.md` only: section 7, stages P1 to P4, and held
questions 4 to 7. It is clint's through the orchestrator. Nothing to forward.

The direction is right:
- standing handles a session can find (`atrium_peers` personas);
- a job queue in atrium, not a model;
- a fresh session per job in the run folder, so section 3's isolation holds;
- follow-ups resume the job;
- answers go to the job's caller;
- the panel rebuilt on the personas;
- the hub serving code so nobody asks for a paste.

It holds on 7.4's write path, and on the queue having no bound.

## Your question: is 7.4 an acceptable change to "the director is the only writer"?

**Yes, with two changes. Without them, no.** Decision 2's rule exists because memory that a later review reads is an
attack surface. As written, a note takes effect at once: the hub applies it to `claude/knowledge`, and every later job
reads it, before @review's batch read ever happens. And the most dangerous kind, false positives, is written
automatically from "verifier refutations", which are a model's output over PR content. So:
- A PR author shapes a change so the verifier refutes a true finding.
- An automatic `false-positives` entry says "this is not a bug".
- Every later review of that repo then stays quiet about that class of bug.

The "instruction-shaped line" refusal does not catch it, because a well-formed false-positive entry is the attack.

1. **Reads pin to the reviewed tip, not the newest.** Jobs read `claude/knowledge` at the last commit @review has
   cleared (a `knowledge-ok <sha>` marker, or a reviewed branch the hub fast-forwards after the batch read). New
   notes wait in the queue until then. The hub still applies them with the mechanical checks, so @review's batch is
   a diff read, not a turn per job. The writer is the hub, the gate is @review, and nothing unread is ever read.
2. **False positives come only from a human or @review**: a walk rejection by clint, or a re-read's "not a bug".
   Never from a verifier refutation, which is model-on-attacker-text. Each entry is pinned to repo, file, finding
   kind and commit, never a general "X is fine". It expires after a line cap or N commits, so one entry cannot mute a
   class forever.

With those two, the change is acceptable and cheaper than a turn per note. Author notes (Q5) go through the same
pin, for the same reason.

## Medium 2 (holds): "queued, never refused" has no bound

Any card on any room can open a job, and each job is a full review session that costs money. A looping or
prompt-injected worker can fill a persona's queue, and clint's jobs jump it but still wait behind the running one.
Add:
- a per-caller cap (for example 3 open jobs per caller per persona);
- a queue length cap per persona, answering "queue full, N ahead" when it is full, which is still not a refusal of
  the persona;
- the peer rate limit the message path already has (`peerLimit`).

P1's acceptance gets one case: a fourth job from one caller is answered "queue full".

## Your other checks

- **7.1, the reserved `atrium:` prefix: right**, with two notes.
  - Reserve it on every name a card can answer to, not only the alias. That covers the wire handle derived from a
    title, and a migration that renames any existing alias starting with it.
  - `atrium:<name>` is also the namespace `lean_agents` uses for agents inside a lean card (the Agent tool). Say that
    the persona handle is a say address and the lean name is an Agent-tool name, so a reader does not take one for
    the other.
- **7.2, personas as hub rows from any room: right, with one gap.** Two rooms can carry agent files with the same
  name and different contents, so one handle would behave differently depending on the room it was routed to. Record
  each room's file hash in the row, route only to rooms whose hash matches the one the hub pinned, and show a
  mismatch on the persona tile.
- **7.3, a fresh session per job with cwd the run folder: right.** It keeps section 3's isolation for outside
  targets, follow-ups within 2 hours resume the job's own session, answers go to the job's caller, and a culled
  job restarts from its notes.
- **7.5, `max_instances` (default 1, at most 3) under keep-one-free: right.**
- **7.7, the hub serves the code: right as the default**, never a paste, and dirty worktrees refused. Two notes:
  - Outside PR heads live under `refs/pull/<n>/head` in the mirror, never in `claude/*`, and the landing op and the
    collect never take a ref from the pull namespace. Say so, so a PR cannot become a landable branch by its name.
  - The hub's forge login fetches only, through the typed read-only op of the room-to-room spike.

## Verdict

HOLD on 7.4 (reads pinned to the @review-cleared tip, and false positives only from clint or @review, pinned and
expiring) and on the queue bound. The 7.1 and 7.2 notes and the 7.7 namespace line are to fold in. A re-read covers
7.1, 7.2, 7.4, 7.7 and P1 and P2's acceptance.

Closed: none (first read)
Open: M1 (7.4), M2 (queue bound), S1 (prefix on every name), S2 (persona file hash), S3 (pull namespace)

Quality: a strong amendment that answers the review-b failure directly. The miss is that a knowledge base read by
every later review is the highest-value target in the design, and its writes were treated as low-risk.

## Re-read at ceca58d8 (2026-10-02): OK, doc-ok

One commit, only the design doc.
- **M1, closed.** Jobs read `knowledge/cleared`, pinned at the job's start, which the hub fast-forwards only after
  @review's batch commit carrying `Atrium-Verdict: knowledge-ok <sha>`. The hub still writes `claude/knowledge` with
  the mechanical checks. False positives come only from clint's walk rejection or @review's re-read "not a bug",
  never from a verifier. They are pinned to repo, file, kind and commit, and expire (90 days, or a substantial
  change to the file).
- **M2, closed.** 3 open jobs per caller per persona and 10 in all, a queue cap of 20 ("queue full"), and
  `peerLimit`. clint is outside the per-caller cap and inside the queue cap.
- **S1 to S3, closed.** `atrium:` is reserved on derived wire handles too, with a renaming migration, and is
  separate from the `lean_agents` namespace. Agent-file hashes route only to matching rooms. PR heads stay in
  `refs/pull/*`, which landing and collect never take.

Two lows, to fold in when convenient:
- **L1:** P2's "what" column still reads "false positives from walks, verifiers and re-reads", against its own
  acceptance ("a verifier's refutation adds no false-positive entry"). Drop "verifiers".
- **L2:** a `knowledge-ok` trailer is text like any other. The hub should accept it only on a review-files-only commit
  it collected from `claude/review`, using its own collection record, so another card's commit cannot move
  `knowledge/cleared`.

Closed: M1, M2, S1, S2, S3
Open: L1, L2

Verdict: OK, doc-ok 2e9cddd0^..ceca58d8. It lands alone by cherry-pick of the two.

## The lows at c17e9c20 (2026-10-02): OK, doc-ok

- **L1, closed.** P2's "what" column now names only clint's walk rejections and @review's re-reads as sources of
  false positives.
- **L2, closed.** The hub accepts `knowledge-ok` only on a review-files-only commit it collected from @review's own
  `claude/review` branch.

Closed: L1, L2
Open: none

Verdict: OK, doc-ok c17e9c20^..c17e9c20.
