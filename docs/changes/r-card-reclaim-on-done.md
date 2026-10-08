# r-card-reclaim-on-done

clint's answers of 2026-10-07: a worker's done closes its card (H3), the launcher saying merged and deployed reclaims
it and everything it made (H11), an ended card whose work is not done stays in Terminals for good (H4), and nothing
clint started is ever touched.

## What each path did before

- `atrium_report done` and `atrium finish --status done` both land in `finish()` in `internal/daemon/finish.go`. The
  card goes to `done`, the work item to `reported`, the launcher is sent a notice, and `exitsOnReport` asks the runner
  to leave after 5 seconds. It does not for a done with an ask, a director, the orchestrator, a card that parks when
  idle or holds notices, a `link:` card, a card with no launcher, or one typed into in the last 2 minutes.
- `atrium_say done <sha>` is what the launch text (`leanSystemPrompt`) and the silent-stop nudge tell a worker to
  send. It went through `peerSaid`, which marks the worker reported and logs the words, and by design never moved the
  card. So a worker that followed its launch text never closed its card and its runner stayed up. This is the
  contradiction r-agent-comms-one-protocol lists.
- The "ended without a report" notices (r-expected-end-notices) fire only for an end atrium did not cause and the
  worker did not report. They do not close anything.
- Reclaim was `atrium_cull`, and the git `post-merge` hook that marked a merged worker and culled it 30 minutes
  later. A merge is not a deploy, so that reclaimed work that was not deployed. Only a card whose directory is a git
  worktree could be culled. A worker in a scratch directory was refused, so its BRIEF.md and directory stayed.

## What each path does now

- All three doors close the card. `peerSaid` and `reportedAcross` hand a say that is exactly `done <sha>` and any
  recap to `doneBySay`, which calls the same `finish()`. Only a launched worker's say to its own launcher counts.
  Free text, `done` with no sha, and the launcher's words to the worker are not read as a verdict. The launcher is not
  sent a second notice, because it has the words.
- `atrium:keep-open` and `atrium:investigation` join the tags a done report does not exit.
- The signal for reclaim is the launcher's `atrium_cull`. It was already the acceptance, only the launcher knows the
  work is deployed, and a new verb or a mark on the card would be a second way to say the same thing. Its description
  now says merged and deployed. A merge alone reclaims nothing: `merged_cull_grace` is off until the operator sets a
  grace. With it set the old mark and sweep run, with the same checks as below.
- A reclaim removes the session, then for a git worktree its BRIEF.md, worktree and branch, as before, and for any
  other directory its BRIEF.md and the directory when that leaves it empty. What else the worker left there is its
  output and stays, and the answer says so. It then frees the card's inventory like a close does, keeping a worktree
  that holds work nowhere else has. The card and its history stay, archived, never filed as dead.
- Every path goes through `internal/safepath`. BRIEF.md that is a link, tracked by git, or not a plain file is left
  alone. A directory inside a git checkout, a drive root, the home folder and a relative path are never removed. The
  main checkout is refused by `inspectCull` as before.
- H4 and the scope rule, in `reclaimBlock`, read by the explicit cull and the sweep: refused for a card without
  `origin:agent`, a card tagged keep-open or investigation, a card not in `done`, and a card whose last report was not
  done.

## Limits

A worker that makes its own git worktree outside its card's directory is reclaimed only through its inventory, so
that worktree and branch stay unless the worker owns them. A directory that is not a git worktree has no branch of
its own to prove merged, so the launcher's call is the claim there.

## Test plan

## @LETTER@. A worker's done closes its card, and the launcher's cull reclaims it

### @LETTER@1. Done by say

1. Launch a worker from the orchestrator. Have it `atrium_say` its launcher `done <sha> tests pass`.

**Expected:** its card goes to done with the recap, its runner is asked to leave, and the launcher got the say and
no second notice.

### @LETTER@2. Free text does not close

1. Have a worker say `done with part one, carrying on` to its launcher.

**Expected:** the card stays where it was.

### @LETTER@3. Reclaim

1. After the work is merged and deployed, the launcher calls `atrium_cull card=<worker>`.

**Expected:** BRIEF.md and the worker's empty scratch directory are gone, the card is off the board with its history,
and a merge alone did nothing.

### @LETTER@4. What stays

1. Call `atrium_cull` on a card clint started, on a card tagged `atrium:keep-open`, on one that ended without a done
   report, and on one whose directory holds other files.

**Expected:** the first three are refused with a sentence and nothing is removed. The last loses only BRIEF.md.