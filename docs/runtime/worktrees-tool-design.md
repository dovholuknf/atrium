# atrium_worktrees: audit and remove worktrees in one call

On 2026-10-16 the errand minion audited about twelve worktrees for landed work in four PowerShell calls. Each call
repeated `Set-Location`, printed fifty lines or more, and every later turn of that session read those lines again. A
tool that answers one line per worktree does the same audit in one call and costs its answer once.

Backlog item `f-worktrees-tool`. Status: designed, not built.

## What already exists, and what this reuses

| | where | what it decides |
|---|---|---|
| cull | `internal/daemon/cull.go`, `reclaim.go` | exit a merged worker, remove its worktree and branch |
| close | `internal/api/close.go`, `sweep.go` | free a card's inventory rows: worktree, branch, clone, dir |
| inventory | `internal/store/resources.go` | one row per thing the room made for a card |
| branch status | `internal/gitsync/branchstatus.go` | which hub branches landed: patch id, then subject AND files |
| land facts | `internal/gitsync/landfacts.go` | whether claude/main carries a commit, by ancestry or patch id |

The tool adds no new landed rule and no new delete path. Landed is branch status's rule, run on the room. Removing
is the cull's refusals and the close's freeing, applied to a worktree named by path instead of a card named by id.

## The tool

`atrium_worktrees`, on the hub's control MCP, deferred like the rest. Full set only: it is not in `workerTools`, so a
worker never sees it. The description stays under 120 words, because ToolSearch sends it every time it is loaded.

```
atrium_worktrees {
  action: "audit" | "remove",
  room?:  string,    // default: the caller's room
  repo?:  string,    // a path inside any checkout of the repo, or host/org/repo. default: the caller's cwd
  prefix?: string,   // audit: only worktrees whose path starts with this
  paths?: [string],  // audit: only these. remove: required, these and nothing else
}
```

`prefix` and `paths` together are refused. A path is compared after `filepath.Clean` and case folding on Windows,
the way `samePathFold` already does for the inventory. `host/org/repo` is resolved to the room's clone the way
`/v1/new` resolves it, and a repo the room has no clone of is refused with that sentence.

The hub forwards to the room as `POST /v1/worktrees/audit` and `POST /v1/worktrees/remove`, with `X-Atrium-Room`,
like every room tool. The room runs every git command. The hub runs none and no forge is asked.

## The audit line

One header line, then one line per worktree, then at most one trailer line. No command is echoed and no git output
is passed through, except the first line of a git error where a line says `unknown`.

```
claude/main 3a359af7 from hub, 4 worktrees
D:/worktrees/claude/atrium/w-ziti-cap   claude/w-ziti-cap  clean          landed:patch     removable
D:/worktrees/claude/atrium/w-stack      claude/w-stack     dirty 2 +BRIEF unlanded 1 9c1e2ab "Stack sort keeps the pin"  keep: dirty, unlanded
D:/worktrees/claude/atrium/rnd          claude/rnd         clean +BRIEF   landed:ancestry  keep: not atrium's
D:/worktrees/claude/atrium/w-old        (missing)          -              -                removable: prune
```

**The header** names the ref measured against, its short sha, and where it came from: `from hub` for
`refs/remotes/hub/claude/main`, the hub's own tip as the last sync fetched it, or `local` for `refs/heads/claude/main`
on a repo the hub does not sync. A repo with neither ref is refused and nothing is measured. Then the count.

**The columns**, separated by two spaces and not padded beyond the longest path in the answer:

1. The path, forward slashes.
2. The branch, or `detached@<sha7>`, or `(missing)` when git lists a directory that is gone.
3. `clean` or `dirty <n>`. `n` counts `git status --porcelain` lines other than an untracked `BRIEF.md` or
   `REPORT.md` at the worktree's root. Those two are shown as `+BRIEF` and `+REPORT` and never counted.
4. `landed:<how>` with the weakest evidence any commit needed (`ancestry`, `patch`, `subject`), or `unlanded <n>` with
   the newest unlanded commit's sha and subject, the subject cut to 40 characters, and `+<n-1>` when there are more.
5. The verdict: `removable`, `removable: prune` for a missing directory, or `keep: <reasons>`, joined with commas,
   from the refusal list below.

**The trailer** is there only when a line was left out: `<n> more, narrow with prefix or paths` past 100 worktrees,
or `<n> unknown: <first reason>` when git could not answer for some. No other trailer.

**The budget.** A line stays under 160 characters with a path of 50. Twelve worktrees are about 13 lines and under
600 tokens. The JSON the room answers has the same fields named, for a board view later. The MCP tool sends the text
only, since a second copy of the same facts doubles the tokens every later turn reads.

## Landed, by patch id then subject

The rule is branch status's (`docs/fabric/branch-status-design.md`), run in the room's clone against the header's ref.
A non-merge commit of the worktree's branch, not an ancestor of the ref, is carried when:

1. its `git patch-id --stable` is the patch id of a commit the ref has beyond the fork point, or
2. its subject is the subject of such a commit AND it touches the same set of files.

**Subject alone is not used**, though the backlog item says subject. Branch status decided against it: "Fix tests" is
on main a hundred times. A false landed here deletes work, and a false unlanded only keeps a worktree. So subject
counts only with the file set.

**The changelog rule is not used either.** Branch status counts a branch as landed when its changelog fragment is on
main, to catch a squash. A squash can drop a commit and still carry the fragment, and this tool deletes on the answer.
A worktree a squash landed reads `unlanded`, and is culled by hand after somebody reads it.

Merge commits are skipped, as both existing rules skip them. A commit with no patch id (no change, or past the size cap)
is carried only by rule 2.

**One pass per call.** Every audited branch's commits beyond the ref come from one `git rev-list --no-merges <b1>
<b2> ... --not <ref>`. The ref's side is the commits since the oldest fork point among them, hashed once with
branch status's batching and its cap (`hashBatch`, `hashCap`). Nothing is cached between calls: the room has no pass
to keep warm, and an audit is a few calls a day.

**Bounds.** The whole call is bounded at 60 seconds and each git command at 20, as the cull's are. A worktree whose
git does not answer in time is `unknown` and is never removable.

## Remove, and what it refuses

`remove` takes `paths` and nothing else. It never trusts an earlier audit: it audits those paths again, in the same
call, and removes the ones whose fresh line is `removable`. The answer is the same line format, with the verdict
column saying what happened: `removed`, `removed, branch kept: <why>`, or `kept: <reasons>`.

**A worktree is removed only when every one of these holds.** Each failure is a reason on the line.

| check | reason on the line |
|---|---|
| it is atrium's, see the ledger rule below | `not atrium's` |
| it is not the repo's main checkout or the room's clone | `main checkout` |
| its branch is not `main`, `master`, `claude/main`, `hub-main` or the repo's default branch | `protected branch` |
| nothing dirty beyond an untracked root `BRIEF.md` or `REPORT.md` | `dirty` |
| no unlanded commit | `unlanded` |
| git lists it and it is not locked (`git worktree lock`) | `not a worktree` or `locked` |
| no card in it has a live runner or a new context in progress | `in use: <card title>`, cull it instead |
| a card's worktree passes `reclaimBlock` (see below) | the `reclaimBlock` sentence |

`reclaimBlock` passes when an agent launched the card, it is not tagged to stay open, it is done, and its last report
says done.

**It never exits a session.** A runner still alive in the worktree is `atrium_cull`'s case, which asks it to leave
first. This tool cleans up after sessions that already left.

**The order inside one removal** is the cull's: `git worktree remove` without `--force`, so git refuses a worktree
these checks passed by mistake. The long path fallback is the cull's too. Then `git branch -D`, after the carried
check above, since `-d` measures against whatever HEAD is. A branch another worktree has checked out is kept and
the line says so. A missing directory is cleared with `git worktree prune`, which only drops git's records of
directories already gone.

**After a removal** the card's inventory rows for that worktree and branch are freed, and a card the worktree belonged
to gets the event the cull writes and is archived as a culled card is. Its row, transcript and history stay.

**Not touched:** the branch's copy on the hub (branch status reports it, and the card's close frees it), any clone,
and any file outside the worktree.

## The ledger rule: only worktrees atrium created

A path is atrium's when either holds:

1. **An inventory row names it.** A live `worktree` row in `card_resources`, written by the open verb, `/v1/new` or
   a link's open when atrium made the worktree. Its `branch` row decides whether the branch is atrium's to delete
   too. A worktree that was already there when the card opened has no row, so it is not atrium's by this rule.
2. **It is the worktree of a card tagged `origin:agent` and `atrium:subagent`.** That is the cull's rule today: a
   launcher made the worktree for the worker it launched, and the card says so. The branch is the worktree's own.

A worktree with neither is `not atrium's`, whoever asks and whatever the audit says of it. That covers the operator's
own worktrees, an orchestrator's, a director's like this one, and a worker worktree whose card was deleted by the
sweep. The last case is the one that costs: the audit lists it as `landed` and `keep: not atrium's`, and somebody
removes it by hand. Deleting by path because it looks finished is the thing `docs/rnd/close-workflow.md` rules out.

## Phases

Each lands on `claude/main` on its own, with its changelog fragment and `docs/changes` page.

**P1, the landed rule as a function.** Move branch status's commit facts and the carried decision (`readFacts`,
`readMeta`, `hashBatch`, `carriedBy`) off the hub's `Store` onto plain functions over a `Runner` and a directory, in
`internal/gitsync`. Branch status calls them as before and its tests do not change. No behavior change, hub only by
code, nothing to deploy.

**P2, audit.** The room endpoint, the hub tool with `audit`, the line format, the bounds. Room and hub restart.

**P3, remove.** The refusals, the ledger rule, the freeing and the archive, sharing the cull's removal code rather
than copying it. Room restart.

## Tests

Integration tests with real git in a temp repo, under the `integration && git` tags the gitsync tests already use.

- Landed: a fast-forward reads `ancestry`. A cherry-pick reads `patch`. A cherry-pick reworded by hand with the
  same files reads `subject`. A same-subject commit touching other files reads `unlanded`. A merge commit on the
  branch is ignored. A branch carried only by its changelog fragment reads `unlanded`.
- Dirty: an untracked root `BRIEF.md` and `REPORT.md` count as clean and show as flags. The same names in a
  subdirectory count as dirty. A staged change, a modified file and an untracked file each count one.
- Header: `from hub` with `refs/remotes/hub/claude/main`, `local` without it, and a refusal with neither.
- Each refusal in the table on its own, with the line's reason. The main checkout, each protected branch, a locked
  worktree, a live runner, a card `reclaimBlock` refuses, a path with no row and no tagged card.
- Remove re-audits: a worktree made dirty between an audit and a remove is kept.
- Remove frees the inventory rows and archives the card. A branch checked out in a second worktree is kept.
- A missing directory reads `(missing)` and is pruned by remove.
- Budget: 150 worktrees answer 100 lines and the trailer. No line is over 160 characters with a 50-character path.
- The tool is absent from the worker class (`workerTools`).
- P1: branch status's existing tests pass unchanged.
