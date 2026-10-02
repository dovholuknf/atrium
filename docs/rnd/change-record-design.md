# Change record: atrium keeps "where are we" for every branch and PR, so no session starts from scratch

Status: design by @rnd, 2026-10-02. Nothing built. Asked by clint through the orchestrator, 2026-10-02 afternoon.
He had asked a session whether a change was tested, ready to push and reviewed, and the session had to rebuild the
answer from its history. His idea:
- atrium holds and tracks each PR's result, with the files that go with it;
- when a new PR comes in, a session is handed that inventory, so it does not start from scratch.

**Questions are held in section 6 for when clint asks for them. They are not sent.**

Read for this design, 2026-10-02:
- `internal/store/prs.go` (the pulls index: a row indexes a run folder, and every field but `why` is observed);
- `internal/deployready/deployready.go` (`Atrium-Verdict` trailers, matched to commits by `git patch-id`, trusted
  only on commits that touch review files alone);
- the `work_item` and `work_log` tables (a card's reports and verdicts, per card, not per change);
- `docs/rnd/pr-ci-state-design.md`;
- `docs/rnd/pr-review-story.md`;
- `docs/rnd/review-on-atrium-design.md` and `docs/rnd/room-handoff-design.md`.

## 0. The answer

- **One record per change.** A change is a branch or a PR, ours or outside, and is keyed by forge, repo, and branch or
  PR number.
  - Like the pulls index, the record is a row that indexes a folder, `~/.atrium/changes/<id>/` (`0700`). The folder
    is outside every repo, because records quote people and the atrium repo is public.
  - The row holds the facts. The folder holds the files: review reports, test logs, repro notes and handoffs.
- **Every fact is pinned to the commit it was about.** "Tested" means tested at `abc1234`. When the branch has moved
  on, the record says *"tested at abc1234, 3 commits ago"* instead of a stale yes. This is the part a session gets
  wrong when it rebuilds the answer from memory.
- **Facts come from two kinds of source, and each is labelled:**
  - **seen by atrium:**
    - a verdict trailer, read the way `deployready` reads it;
    - a panel report from `atrium_review`;
    - a PR run from the pulls index;
    - a forge check state from `pr-ci-state`;
    - a push, from the remote ref atrium itself reads;
  - **reported by a card:** a test it ran, with the command, the room, the result and the log file, written through
    one tool, `atrium_record`.

  A reported fact names its card and is shown as "reported". Only seen facts make a change "ready".
- **"Where are we" is a read, not a re-derivation.** The answer is `atrium change <branch|PR>`, the `atrium_change`
  MCP read, or the card's drawer. Each gives the same four lines, which answer clint's question as asked:

  ```
  Tested    yes at 3f2a91c (go test ./..., m1mini, pass, log) . 0 commits since
  Reviewed  doc-ok by @review, covers 4 of 4 commits . 2 findings, both fixed
  Pushed    no . claude/fabric is 4 ahead of origin
  Open      1: the 378 replay (@runtime) . 1 question held for clint
  ```

  For a code change, the review line uses the deploy verdicts:

  ```
  Tested    yes at 9c41d07 (go test ./internal/..., sg3, pass, log) . 0 commits since
  Reviewed  hub-ok by @review, covers 3 of 3 commits . room-ok: none yet . 1 low open
  Pushed    yes . origin/claude/runtime at 9c41d07
  Open      0
  ```

- **The inventory is handed over at the start.** Two cases:
  - **A card launched on a branch or PR that has a record** gets the four lines and the folder's path at the top of
    its brief.
  - **A new PR** gets the records of the most related changes: the 3 records with the most files in common in the
    last 90 days. It gets their open items and their findings on those files, so a known issue is not found again
    from scratch. That part of the brief is marked as data from other changes, not instructions. Findings can
    quote an outside PR's text.
- **It travels with a move.** The record belongs to the change, not the card, so `atrium move` carries the folder,
  and the new card's brief reads it like any other.

## 1. What is there today

- **Per PR run:** the pulls index (`pr_review` rows) holds the run's state, cost, head reviewed and second opinion.
  The run folder holds the findings and the walk. That covers one review of an outside PR, not its tests, pushes or
  later rounds.
- **Per card:** `work_item` and `work_log` hold a card's reports and verdicts. When the card is culled or moved, a
  new card on the same branch starts with none of it.
- **Per landing:** `deployready` reads `Atrium-Verdict` trailers. A trailer is matched by patch-id, so a rebased
  commit keeps its verdict and an altered one loses it. It is trusted only on a commit that touches review files
  alone (`reviewFile`, `deployready.go:180`). Nothing shows that answer per branch.
- **Tests:** nothing records them. A session says "tests pass" in a report, in prose, at no commit.
- **Pushes:** nothing records them. Whether a branch is on its remote is a `git` question each session asks again.

## 2. The record

The row is `change`:
- id;
- forge, repo, and branch or PR number;
- the PR's URL, if there is one;
- head (the last tip seen);
- base (what it is measured against: `claude/main`, or the PR's base);
- state: open, landed, merged, closed or abandoned;
- the files touched at head, for the related search;
- created and updated times.

Each fact is one row in `change_fact`, append-only:

| kind | holds | source |
| --- | --- | --- |
| `test` | the command, the room, pass or fail, the duration and the log file in the folder | reported (`atrium_record`), or seen when atrium ran it (`prove`, CI) |
| `review` | the reviewer, the verdict, the range covered, and the report file | seen: an `Atrium-Verdict` trailer under deployready's rules, or an `atrium_review` report |
| `finding` | the severity, the file and line, the claim, and its state (open, fixed at `<sha>`, rejected because ...) | seen from a panel or PR-run report. A change of state is reported, and needs the fixing commit to exist |
| `push` | the remote, the ref and the sha | seen: atrium reads the remote ref with `git ls-remote` when it refreshes |
| `ci` | each check's state at a sha | seen: `pr-ci-state` |
| `open` | an open item: a stage, a question held for clint, or a blocker | reported. Closing one is reported too |
| `note` | a handoff, a repro or a decision, as a file | reported |

**Every fact carries `at_sha`, and atrium stamps it, never the card.** For a reported fact:
- the room runs `git rev-parse HEAD` in the card's own worktree when `atrium_record` is called;
- it adds a dirty flag from `git status --porcelain`, and shows that fact as "at abc1234 plus uncommitted changes";
- it checks that the worktree's branch is the change's branch, and refuses the call otherwise.

A card cannot pass an old test off as current, because it never names the sha. For a seen fact, the sha is the one
the source carries (the trailer's range, the run's head, the remote ref, the check's sha).

The read compares `at_sha` with head. A fact at an older sha is shown with the count
of commits since. For a review, it is shown with how many of the commits since are covered by patch-id.

**Who may write.**
- A card writes reported facts only for a change its own worktree or run is on.
- The hub writes seen facts.
- No card can write a seen fact.
- A trailer counts only under deployready's rules, and only from a branch atrium collects. Every session shares one
  git author, so a trailer written into an outside PR's commits never counts.
- **The caveat.** A "seen" trailer is only as good as deployready's rule that a verdict commit touches review files
  alone. Any card can write such a commit while rooms do not sign their commits. So "seen" means atrium read it from
  its own branches, not that @review is proven to have written it. Signed room commits would close that gap, and
  are not part of this design.

**The folder.** It is `~/.atrium/changes/<id>/`, with mode `0700` for folders and `0600` for files:
- `reviews/`: panel and PR-run reports, linked or copied;
- `tests/`: logs, at `0600`, because a test log can hold env values and tokens;
- `notes/`: handoffs, repros, decisions;
- `record.md`: the four lines, rewritten on each change, for a session or a person to open.

Each file is written through `internal/safepath`. A link in the folder points only into atrium's own run folders,
never into a worktree.

## 3. How it is filled, with no extra work for a session

- **Reviews.** When a review commit lands with a trailer, the hub adds a `review` fact to every change whose commits
  the range covers. `atrium_review` writes its report into the folder of the change it reviewed, with one
  `finding` fact per finding. A PR run writes into its PR's record.
- **Findings fixed, one by one.** When a fixing commit's message names a review (as "(review HOLD e40a3c27)" does
  today), the hub marks that review's findings as "fix claimed at `<sha>`".
  - A verdict covers a range, but findings close one by one. A re-read often closes the blockers and leaves a low
    open, so a doc-ok alone never marks a finding fixed.
  - Instead, the re-read's review file carries one machine line: `Closed: M1, M2` and `Open: L1`. These are matched
    to the findings' ids, and only the named ones become fixed. A finding the line does not name stays "fix
    claimed".
  - @review writes the line once C2 exists.
- **Tests.** A session calls `atrium_record {kind: test, cmd, result, log}` after a test run. Atrium copies the log
  into the folder, so the log survives the session. The PR runner's `prove`, and CI through `pr-ci-state`, write seen
  test facts directly.
- **Pushes.** The hub reads remote refs when it collects branches, and for a PR when it refreshes the pulls index.
- **Open items.** A director's queue rows and held questions can name a change, with `atrium_record {kind: open}`.
  Closing one is the same tool.

## 4. Stages

All held by the pause.

| stage | what | owner | size | acceptance |
| --- | --- | --- | --- | --- |
| C1 | `change` and `change_fact`, the folder, `atrium_record` with the room stamping `at_sha` and the dirty flag, the `atrium_change` read with the four lines (doc-ok and hub-ok/room-ok forms), staleness by `at_sha`, and review facts from trailers through deployready's matcher | @runtime | 2 days | on a branch with a landed doc-ok and one later commit, the read says "covers 1 of 2 commits". A test recorded before a later commit shows "1 commit ago", and one recorded with uncommitted changes shows "plus uncommitted changes". `atrium_record` from a worktree on another branch is refused. A card cannot write a `review` fact or name a sha |
| C2 | Seen facts from `atrium_review`, PR runs, pushes and `pr-ci-state`, plus "fix claimed" from commit messages | @runtime, @fabric (pushes, through the hub's collect) | 2 days | a panel's 3 findings appear on the change. A fix commit naming the review marks them as claimed, and a re-read whose file says `Closed: M1, M2` and `Open: L1` marks exactly M1 and M2 fixed and leaves L1 open. A re-read with no such line marks nothing fixed. A branch pushed by hand shows as pushed after the next collect |
| C3 | The inventory at start: the four lines and the folder path in the brief of a card launched on a recorded change, and the 3 related records for a new PR | @runtime | 1 day | a card launched on a recorded branch answers "where are we" from its brief, with no git or history reads, in one turn. A new PR touching a file with an open finding names that finding |
| C4 | The board: the four lines on the card face and in the drawer, and a per-repo list of changes | @ui | 2 days | clint reads "tested, reviewed, pushed, open" for a card's branch on the phone |
| C5 | `atrium move` carries the folder (a row in room-handoff section 4), and the hub keeps one record per change across rooms | @fabric, after M2 | 1 day | a card moved from sg3 to m1mini reads the same four lines after the move |

## 5. How it ties in

- **The PR review story.** A PR run is one review fact, and its walk outcome is the findings' states. A later round
  on the same PR starts from the record instead of a fresh run folder (story section 7).
- **Review on atrium.** Its `report.json` is the artifact, and each finding is a fact (C2).
- **Atrium-Verdict.** Read once, by deployready's matcher, and shown per change. No new trailer.
- **Atrium move.** The record belongs to the change and travels with the branch (C5).
- **Operator focus.** An `open` fact that is a question for clint is the same row as his decision list, linked by
  its number. The daily summary page can list changes whose four lines changed today.

## 6. Questions for clint, held until he asks

1. **What "ready to push" means.** A change counts as ready only when atrium itself saw the tests and the review at
   the current commit. A card's own "tests pass" is shown, but does not count. **Suggested: yes.**
2. **Outside PRs.** Keep a record for outside pull requests too, so a second look at the same PR, or a new PR in the
   same area, starts from what the first one found. **Suggested: yes, kept 90 days after the PR closes.**
3. **Where records live.** Records stay on the machines, outside every repo, because they quote people and the repo
   is public. Later they move to the hub, so every machine sees one copy. **Suggested: yes, the same answer as for the
   factory log.**
