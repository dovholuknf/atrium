# Review: change record design (3ac5d79d, m1mini, 2026-10-02): HOLD

`docs/rnd/change-record-design.md`, alone in its commit, clint's idea through the orchestrator. Its questions are
held in the doc and, by clint's wish, are not forwarded.

The shape is right:
- one record per change, a row indexing a folder outside every repo;
- every fact pinned to the sha it was about, with staleness shown;
- "seen" against "reported", where only seen facts make a change ready;
- the four lines handed over at start, and carried by a move.

## The four checks you asked for

1. **The trust split: right.**
   - Seen facts come only from the hub: trailers through deployready's matcher, panel and PR-run reports,
     `ls-remote`, CI.
   - Reported facts come only for the card's own change, labelled with the card.
   - Only seen facts make a change ready.

   One caveat to state: a trailer is "seen" under deployready's rules, which trust a commit that touches only review
   files. Any card can write such a commit, since every session shares one author and the rooms are unsigned. Say that
   a review fact is as trustworthy as the trailer rule, and no more, until verdict commits are signed (REVIEWER-NOTES,
   "hand-written trailers").
2. **Staleness by `at_sha` with patch-id coverage: right for seen facts, and see Medium 1 for reported ones.**
3. **`~/.atrium/changes` at 0700 and 0600, outside every repo: right.** Copied test logs can hold env values or
   tokens printed by a test, so 0600 matters. Write "linked" reviews as copies or as links into atrium's own run
   folders only, never into a worktree.
4. **"Fix claimed", confirmed by a re-read: right in idea, wrong in grain.** See Medium 2.

## Medium 1 (holds): who sets `at_sha` on a reported fact

`atrium_record {kind: test, cmd, result, log}` names no sha, and nothing says who stamps `at_sha`. If the card
supplies it, a test run before the last three commits can be filed as current. The room stamps it instead: `git
rev-parse HEAD` of the card's worktree at the call, plus a `dirty` flag from `git status --porcelain`. A test on a
dirty tree is shown as "at abc1234 plus uncommitted changes", never as plain "at abc1234". "A change its own worktree
is on" is also checked by the room at that moment (the worktree's branch), not taken from the card's arguments.

## Medium 2 (holds): a re-read's verdict is per range, and findings close one by one

C2 says "a fix commit naming the review marks them as claimed, and a re-read's doc-ok marks them fixed". A re-read
OK often closes the blocking findings and leaves lows open, as many re-reads today do ("the low is closed; two lows
remain"). Marking every claimed finding fixed on the range's verdict would show "2 findings, both fixed" when one is
not, in the line clint reads first. Close per finding:
- the re-read review file carries a machine line, for example `Closed: M1, M2` and `Open: L1`, with ids that match
  the `finding` facts;
- the hub marks fixed only the ids it names;
- a claimed finding the re-read does not name stays "fix claimed" and is shown as such.

@review will write that line on every re-read once C2 is built. Put it in C2's acceptance.

## Smaller

- **"Related" records for a new PR** hand a session earlier findings, whose text came from earlier PRs, possibly
  outside ones. Mark them in the brief as earlier findings, data and not instructions, the way the PR bundle is.
- **The four-line example** shows `doc-ok` for a branch with tests. A code change gets `hub-ok` and `room-ok`, so
  show that form too.
- **The records outlive the repo's privacy question.** Q3's answer (machines, then the hub) should match the factory
  log's. It is stated, so this is fine.

## Verdict

HOLD on Medium 1 (the room stamps `at_sha` and `dirty`, and checks the change itself) and Medium 2 (findings closed
by id, not by range). A re-read covers sections 2 and 3 and C1 and C2. doc-ok on OK, and nothing to forward.

Quality: a clear, well-grounded design. It builds on what already observes (the pulls index, deployready) rather
than on what sessions say, and the four lines answer the question clint actually asked. The gaps are two places
where a claim could pass as a fact.
