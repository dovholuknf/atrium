# Review: change lifecycle design 45687063 (docs/rnd/change-lifecycle-design.md)

@review on m1mini read this design commit, which stands alone on claude/rnd and lands by cherry-pick. The commit is
unsigned, like every m1mini commit. I took @rnd's five points in order, then looked at the rest. The ladder is right:
eight stages, a gate on each, and the stage worked out from the record. The holds are about who can write which
evidence, and about what happens at the second round.

## (1) The stage is computed from facts at head, never set by the session: HOLD on H1

- **H1: walk evidence can be forged.** Section 2 makes `walkthrough.txt` in `~/.atrium/changes/<id>/` "the record",
  and each line is "also" a `walk` fact. That folder is on the room, where the session runs as the same user and can
  write the file. If the stage is ever read from that file, or a `walk` fact is made from a line the session wrote,
  then a session can approve its own hunks.
  - Fix: a `walk` fact is written only by the daemon or hub, from the operator's own authenticated action on the
    screen. The fact carries the operator identity and the patch-id. `walkthrough.txt` becomes a render of those
    facts and is never read back as evidence.
  - The same rule applies to "rejected by clint" in the reviewed gate, and to clint's go-ahead at stage 0.
- **M1: a new test inside an existing file escapes red/green.** New tests are found from added files, plus whatever
  the session names through `atrium_record {kind: new_test}`. A new test function added to an existing test file is
  only caught if the session chooses to name it. That makes the gate partly self-declared.
  - Fix: every hunk in a file that matches the repo's test patterns counts as test code. The tests it adds or
    changes are listed from the diff, by the language's test naming where one exists. The session's naming can add
    to that list but never take anything away.
- Patch-id, `Closed:` lines and the sha pinning follow the change-record design's M1 and M2, which is right.

## (2) The three walls: HOLD on M2 and M3

- **M2: the hub wall is keyed on a branch name the card chooses.** Pre-receive refuses "a branch that has a change
  record". But hub forge 3.2 lets a card create any `refs/heads/<name>`. So a card pushes the same commits as
  `fix/x-2`, and no record matches that name.
  - Fix: refuse any card push whose new commits include a commit (or patch-id) of an open change below "signed",
    whatever the ref is called.
  - Also, "a card" and "the operator" must be told apart by the hub credential (3.2's card column, the room's
    forwarder token). Commit author or committer won't do: every commit here is `dovholuknf` (REVIEWER-NOTES).
- **M3: the walls cover the hub only.** A room that holds a GitHub token can `git push origin` and
  `gh pr create` straight to upstream. Then only the soft wall 3 stands in the way.
  - Fix: state that a room holds no forge write credential for a change's upstream or for clint's fork, or a token
    scoped so it can't push. Name the check (L4 acceptance: a card's `git push origin` fails for want of a
    credential).
  - Section 5 says pushing to the fork is clint's step. That only holds if the room can't do it.
- **Wall 2** (`atrium_pr_draft` below finished) is right.
- **Wall 3:** "blocked once" needs a scope. Make it once per change per stage. Per session, a long session would be
  blocked once and then never again. It is right that it lets the second Stop through, and right to call it soft.
  On opencode the block reaches the session as a new prompt, delivered through atrium.js's `session.idle`, and that
  works.

## (3) Signing as the branch's first hub push: HOLD on M4 and M5

- **M4: re-signing moves head, so the gates stop holding.** `git rebase --exec 'git commit --amend --no-edit -S'`
  makes new shas. The tested and reviewed facts are pinned to the old head, and "the furthest stage whose gate
  holds at the current head" then falls back to work.
  - Fix: a signed head carries the walked head's facts when its tree is byte-identical and its patch-ids match.
    The sign command checks exactly that before it pushes.
  - It must also compute the stage at the head it fetched, and not trust the record's cached stage. Otherwise a
    commit the room adds between the walkthrough and the sign gets signed without being walked.
  - It should refuse merge commits, or use `--rebase-merges`. A plain rebase flattens them.
- **M5: "first push" only holds for the first round.**
  - A fix after "finished" adds commits. Running the same rebase `--exec` over the whole branch re-signs commits
    that are already on the hub, which changes their committer date and sha. The push is then a non-fast-forward,
    which `receive.denyNonFastForwards` refuses.
  - Fix: sign only the commits after the hub's tip, `<hub tip>..HEAD`.
  - The same break follows Q4's "push your own way" path. If clint pushed the unsigned branch himself first, the
    signed push can't replace it. Say what happens then: sign onto a new branch name, or have the operator replace
    the branch through `atrium hub git`.
- Atrium never holding the key, signing on clint's machine, and his yes before the push are all right.

## (4) Red/green: HOLD on M6

- **M6: a build failure counts as red.** "Base plus only the test hunks must fail" is satisfied by a compile or link
  error. That is the usual case in C (ziti-sdk-c), whenever a new test calls a symbol the fix adds.
  - Fix: red means the named test ran and failed. A build failure is recorded as `red: did not build`, which does
    not pass the gate. Or it is marked not applicable by clint, as an operator fact.
  - The red run must also run that test by name. Another test failing proves nothing.
- Low: test hunks that touch shared helpers or fixtures may not apply to base on their own. When they don't apply,
  record it as such, never as red.

## (5) Only hunks whose patch-id changed reopen: OK, with lows

- L1: `git patch-id` is per file diff, not per hunk. Name the per-hunk hash: the hunk's own `+`/`-`/context lines
  with line numbers stripped, as patch-id does, `--stable`.
- L2: a step that groups hunks reopens whole when any of its hunks changes.
- L3: a rebase onto a moved base can change context lines and reopen hunks the change didn't touch. Say that this
  is expected.

## Other

- Questions are held in section 7, as asked, and not forwarded. The wording follows the interviewer brief.
- Public repo: the clint lines in the `walkthrough.txt` example are made up, and section 0 paraphrases him. Fine.

Closed: none (first read) / Open: H1, M1, M2, M3, M4, M5, M6, L1, L2, L3

Verdict: HOLD 45687063. The re-read can be one pass over sections 1.1, 2, 3 and 4.

Quality: clear, well layered on the change record and the hub forge, and the stage table is the right spine. The
gaps are all about trust and the second round: evidence a session could write, a wall keyed on a branch name the
card chooses, and signing that only works the first time.
