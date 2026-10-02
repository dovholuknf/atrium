# Change lifecycle: the stages a change goes through, each with its gate and its evidence, shown on the card

Status: design by @rnd, 2026-10-02. Nothing built. Asked by clint through the orchestrator, for the software
factory. A session on ziti-sdk-c (the legacy-auth expired-token loop) finished with every test passing on the
CI-equivalent build (161 of 161) and a red/green proof for each new test. It then printed the `git add`, commit and
push commands and the PR body. clint answered that before anything is pushed, he also wants to be walked through the
changes.

Builds on:
- `docs/rnd/change-record-design.md`, one record per branch or PR, with facts pinned to a sha;
- `docs/rnd/review-on-atrium-design.md`, the standing reviewers;
- `docs/rnd/hub-forge-design.md` revision 2, where finished means pushed to the hub;
- `docs/rnd/operator-focus.md` 2.9, the one-step-at-a-time screen;
- `docs/rnd/pr-review-story.md` section 4, the walk format and `walk.txt`.

**Questions are held in section 7 until clint asks.**

## 0. The answer

- **A change goes through eight stages**, and atrium knows which one it is in:

  **work, tested, reviewed, walked through, signed, finished, PR open, merged.**

  Each stage has a **gate**, the evidence it needs, and that evidence is a fact on the change record. So "where are
  we" is a read of the record, and the board shows the stage on the card.
- **The stage is worked out from the evidence, never set by the session.** It is the furthest stage whose gate holds
  at the change's current head. A new commit can move it back. For example, a fix after the walkthrough reopens only
  the hunks it touched.
- **The walkthrough is a first-class stage.** The session walks clint through the diff hunk by hunk, on the same
  one-at-a-time screen as an interview.
  - For each hunk he approves, comments or asks for more.
  - A comment becomes either a fix, which sends that hunk back to work, or a lesson, which goes to the reviewers'
    knowledge or the repo's rules.
  - It is recorded in `walk.txt` lines, the format the PR walk already uses.
- **Nothing is pushed or proposed before the walkthrough passes. That is enforced, not asked for:**
  - the hub refuses a card's push of a change's branch until the record says walked and signed;
  - atrium's PR-draft tool refuses until the change is finished;
  - a session whose last message offers push or PR commands early is stopped once, by its Stop hook, and told what
    stage comes next.
- **clint signs, and atrium never signs.** Signing runs on whichever machine holds clint's key, by his own command
  (`atrium change sign`). That command then pushes the signed branch to the hub, which makes the change finished.
- **The PR to upstream is drafted, never posted, by atrium.** Its body comes from the record: tests, red/green,
  review, walkthrough. clint posts it, and atrium tracks it to merge.
- **The same ladder serves atrium's own work and outside repos.** An outside repo adds its contribution gates before
  work starts and at signing: license, CLA or DCO, AI policy, CI definition and PR template.

## 1. The stages

| # | stage | gate: what must hold at the change's head | evidence on the change record | who acts |
| --- | --- | --- | --- | --- |
| 0 | **start** (outside repos) | the contribution check passed: license, CLA or DCO, AI policy, toolchains and OSes (factory refactor section 3, held) | a `check` fact with the project's rules and clint's go-ahead | atrium checks, clint says go |
| 1 | **work** | none. This is where every change begins, and returns to after a fix | commits | the session |
| 2 | **tested** | the CI-equivalent build passes at head, and every new test has a red/green proof (1.1) | seen `test` facts: the command, N of N, the log, and a red and a green run per new test | atrium runs them |
| 3 | **reviewed** | the standing reviewers ran on the range, and every blocking or high finding is fixed (by a `Closed:` line) or rejected by clint | `review` and `finding` facts from `atrium_review` | the reviewer personas, then the session fixes |
| 4 | **walked through** | every hunk is approved by clint at head (section 2) | `walk` facts, one per hunk, by patch-id, from `walkthrough.txt` | clint, guided by the session |
| 5 | **signed** | every commit on the branch is signed by clint's key, and has a DCO sign-off where the project needs one | `sign` facts: the signed shas and the key's fingerprint | clint, on his machine |
| 6 | **finished** | the signed branch is on the hub at that head | a `push` fact from the hub's push log | clint's sign command, as operator |
| 7 | **PR open** | a PR from the hub branch (or its fork) to upstream exists, opened by clint | a `pr` fact with its URL, seen from the forge | atrium drafts the body, clint posts it |
| 8 | **merged** | the PR is merged, or closed with a reason | `ci` and `pr` facts, from `pr-ci-state` | upstream, tracked by atrium |

For atrium's own repo, "upstream" is the hub's `main`. Stage 7 is a change request to `main` (hub forge section 6),
and stage 8 is its landing.

### 1.1 The tested gate, concretely

- **CI-equivalent** is the repo's own CI recipe, run on the room. The recipe is in the repo's change recipe:
  - the commands its CI workflow runs;
  - the toolchain;
  - the OS it applies to.

  It is read once from the CI config by a person or a reviewed drafting step, and kept in the store. It is never
  re-derived each time. A run records `passed 161 of 161`, the log file and the sha.
- **Red/green per new test.** The session names its new tests through `atrium_record {kind: new_test}`. A new test
  file is also found mechanically from the diff (added files matching the repo's test patterns). For each one, atrium
  runs two things:
  - **red:** the base commit with only the test hunks applied. It must fail;
  - **green:** the head. It must pass.

  Both runs, with their logs, are seen facts. A test that passes on red proves nothing, and the gate says so by name.
- A docs-only change (no file outside `docs/` or `*.md`) passes this gate as `not applicable: docs`.

## 2. The walkthrough

**The screen** is the interview screen (operator-focus 2.9), one step at a time. Each step is one hunk, or a small
group of hunks that only make sense together. It shows:
- **what this changes, in plain words**, written by the session in the PR walk's style (story 4.6: plain wording,
  certain only when proven);
- **the hunk itself**, with its file and line as a deep link to the room's copy (hub forge, pass-through);
- **what covers it**: the new tests that hit it, with their red and green results, and any review finding on these
  lines and its state;
- **progress**, as "4 of 11", with a back control.

**The order** is the story's ordering (story section 3): the riskiest hunk first, then behaviour changes, then tests,
then mechanical changes (renames, formatting) as one skippable group.

**What clint can do on each step:**
- **approve**: moves on;
- **comment**, in his words. The session asks one question back, as a button pair:
  - **fix it**: the hunk goes back to work, with his comment as the session's todo;
  - **lesson**: the comment goes to the right knowledge file (a persona's notes, the repo's reviewer file, or a
    proposed line for the repo's `CLAUDE.md`), through review-on-atrium 7.4's route, read by @review before use;
- **deeper**: a worked example, traced or run (story 4.3's `deeper`);
- **skip for now**: the walkthrough does not pass while anything is skipped;
- **back**.

**The record** is `walkthrough.txt` in the change's folder (`~/.atrium/changes/<id>/`), in `render.WalkLine`'s format:

```
03-auth.c-L212.hunk approved 2026-10-02T15:04Z
04-auth.c-L240.hunk fix 2026-10-02T15:06Z clint: the retry should stop at the token's expiry, not after 3 tries
05-auth_test.c-L88.hunk lesson 2026-10-02T15:07Z clint: name tests for the behaviour, not the bug number
```

Each line is also a `walk` fact pinned to the hunk's patch-id.

- **After a fix,** only the hunks whose patch-id changed reopen. The walkthrough resumes there, after the tested and
  reviewed gates hold again at the new head.
- **A walkthrough parked halfway** resumes at the first open hunk, on any day, by any session, as a PR walk does.

**Who walks.** The session that made the change is the guide, because it knows why each hunk is there. If that
session is gone, a fresh one is launched with the change record and the diff, and the screen says "guided by a new
session".

## 3. Enforcing the order

**The stage is computed.** `atrium_change_next {change}` answers the current stage, what its gate is missing, and
the next action, for example "tested: the red run of `auth_expiry_test.c` passed, so the test does not prove the
fix". Every card brief on a change says: "call `atrium_change_next` before you report done. Never print push, commit
or PR commands for clint."

**Three walls against pushing early:**
1. **The hub.** The hub forge's pre-receive refuses a card's push of a branch that has a change record whose stage is
   below "signed". The refusal says `walkthrough not passed`, or `not signed`. The operator's push is not refused,
   because clint can always push his own way. It is only logged as "pushed before the walkthrough".
2. **The PR tool.** `atrium_pr_draft` refuses below "finished".
3. **The Stop hook.** A session's final message that contains `git push`, `gh pr create` or a PR body while its
   change is below "walked through" is blocked once, with "the walkthrough is next: call `atrium_change_next`". The
   block uses the Stop hook (other-runners: claude and codex both support a blocking Stop). A second Stop is let
   through, so a session is never trapped. This wall is soft, because it reads text. Walls 1 and 2 are the hard ones.

**The board** shows the stage on the card face as a chip ("walkthrough 4 of 11", "tested: red run missing") and the
whole ladder in the drawer. Each stage links its evidence (the CI log, the red and green logs, the review report,
`walkthrough.txt`, the signed shas, the PR).

## 4. Signing, and the push that finishes a change

- **`atrium change sign <change>`** is a command clint runs in his own terminal, on whichever machine holds his
  signing key. Atrium never holds the key and never signs. The command:
  1. checks the record is at "walked through";
  2. fetches the branch (pass-through from the room, hub forge 3.3);
  3. re-signs each commit with clint's configured key (`git rebase --exec 'git commit --amend --no-edit -S'`, adding
     `Signed-off-by` where the project needs DCO);
  4. shows the result;
  5. on his yes, pushes it to the hub as the operator.
- **That is the branch's first push to the hub.** A card never pushes a change's branch itself (wall 1), so the
  signed history never has to replace an unsigned one, which the hub's no-force rule would refuse.
- The push fact makes the change **finished**, which matches the hub forge rule that only a branch on the hub is
  finished.

## 5. Outside repos

The same eight stages, plus:
- **Stage 0, the contribution check**, before the clone (factory refactor section 3). A project whose AI policy
  forbids AI-written contributions stops here, and nothing is cloned.
- **The CI recipe** is the project's own CI, not atrium's.
- **Signing** adds what the project's rules require: DCO sign-off, or a CLA that clint has signed (a check, never
  signed by atrium).
- **The PR body** follows the project's PR template, filled from the record. It is drafted into the change folder as
  `pr.md` and never posted.
- **The fork:** the PR goes from clint's fork on the forge. Pushing to that fork is clint's step, like posting the PR.
  Atrium prints the one command for him only at stage 7, and never runs it.

## 6. Stages of the build

All held by the pause. They need the change record's C1 and C2 first.

| stage | what | owner | size | acceptance |
| --- | --- | --- | --- | --- |
| L1 | The stage computed from facts at head, `atrium_change_next`, the brief line, and the stage chip and ladder on the board | @runtime, @ui | 2 days | a change with tests passed and a review with one open high shows "reviewed: 1 high open". After the fix and a `Closed:` line it shows "walkthrough 0 of N". A new commit moves it back to the stage its gates hold at |
| L2 | The tested gate: the CI recipe in the store, the CI-equivalent run, new tests from the diff and `atrium_record`, the red and green runs, and the docs-only pass | @runtime | 2 days | on the ziti-sdk-c change: 161 of 161 recorded as seen, each new test with a red that fails and a green that passes. A test that passes on red blocks the gate and is named |
| L3 | The walkthrough on the interview screen: hunks ordered by risk, approve, comment then fix or lesson, deeper, skip, back, `walkthrough.txt` and `walk` facts, and reopening by patch-id after a fix | @ui, @runtime | 3 days | clint walks 11 hunks on the phone, asks for one fix and one lesson. After the fix commit only that hunk reopens. The lesson reaches the knowledge branch for @review |
| L4 | The walls: the hub's pre-receive reading the stage, `atrium_pr_draft` refusing below finished, and the Stop hook's one block | @fabric (hub), @runtime | 1.5 days | a card's `git push hub` of the change branch before signing is refused with `walkthrough not passed`. A final message with `git push` before the walkthrough is blocked once and let through the second time |
| L5 | `atrium change sign` on clint's machine: fetch, re-sign with his key, DCO where needed, show, push to the hub as the operator | @runtime | 1.5 days | on clint's laptop, the command signs a three-commit branch, every commit verifies with `git log --show-signature`, and the hub's push log records the operator push. Atrium's own processes hold no key |
| L6 | PR draft from the record with the project's template, `pr.md`, and tracking to merge through `pr-ci-state` | @runtime | 1.5 days | `pr.md` lists the 161 tests, the red/green proofs, the review summary and the walkthrough's fixes. When clint posts it, the record moves to "PR open", and then to "merged" when upstream merges |
| L7 | Outside gates: stage 0 from the contribution check, the DCO and CLA checks at signing, the project's template | @runtime, after the contribution check is built | 1 day | a project with a DCO requirement gets `Signed-off-by` on every commit at signing. A project whose policy forbids AI contributions stops at stage 0, with nothing cloned |

## 7. Questions for clint, held until he asks

Phrased as `docs/rnd/interviewer-brief.md` section 3 asks.

1. **The walkthrough on every code change.** A session finishes a fix to ziti-sdk-c and all its tests pass. Before
   anything is pushed, it walks you through each changed piece on the phone or the board, one at a time, and you
   approve each or say what to change. Should every code change wait for that? **Suggested: yes, for code. Not for
   design docs, which @review reads.**
2. **Where signing happens.** The walkthrough is done. You run one command on your machine, it signs every commit
   with your key and puts the branch on the hub, and that makes it finished. Should finished always mean signed by
   you? **Suggested: yes.**
3. **A test that proves nothing.** A new test passes even on the old code, before the fix. Should that stop the
   change at "tested" until the test is fixed? **Suggested: yes.**
4. **Pushing your own way.** You push a branch yourself before its walkthrough. Atrium lets it through and only notes
   it on the change. Is that right, or should it ask you first? **Suggested: let it through and note it.**
