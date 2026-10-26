# Go review panel: shell-outs, DRY and not DRY, constants

Item `r-go-review-panel`. Reviewed at `claude/main` ca6a5c879 (115 commits past the panel snapshot 960577ccb, which
already landed the `git log` and `ssh-keyscan` storm fixes ec135bc4d, a4052156d, fdff1b269 and 8689f365a). The panel
spec named in the item, `C:/temp/review-panel/go-full-960577ccb/PANEL.md`, is not on this machine, so the three topics
of the item title are the scope. No Go code is changed here.

Severity: High is data loss or a hang a user meets. Medium is a wrong answer, a hole or a real maintenance trap. Low is
cost or tidiness.

## Summary

| # | Sev | Topic | Finding |
| --- | --- | --- | --- |
| 1 | High | shell-outs | the clone delete check reads a failed `git worktree list` as "no other worktrees" |
| 2 | High | shell-outs | 24 direct `exec.Command("git", ...)` sites bypass `gitsync.Runner` |
| 3 | Medium | shell-outs | `moveGit` has no timeout and runs inside a move request |
| 4 | Medium | shell-outs | the provider worktree endpoint passes an unchecked branch name to git |
| 5 | Medium | shell-outs | runner forks have no `WaitDelay`, a shim's child can outlive the timeout |
| 6 | Medium | not DRY | five protected-branch sets that disagree |
| 7 | Medium | not DRY | eight `git worktree list --porcelain` parsers with four path rules |
| 8 | Medium | not DRY | nine path-equality helpers with different case and symlink rules |
| 9 | Medium | constants | `Runner.Bound` says "tests only" and production sets it |
| 10 | Medium | constants | the same `git worktree list` is bounded at 2 s in one place and 60 s in another |
| 11 | Low | shell-outs | the autocompact probe holds its lock across a 15 s `--help` |
| 12 | Low | shell-outs | the host key scan's whole bound is shorter than two of its dials |
| 13 | Low | shell-outs | `CombinedOutput` wrappers parse stderr warnings as data |
| 14 | Low | not DRY | `git worktree add` is written out three times in `internal/api` |
| 15 | Low | not DRY | `commitExists` and `commitInDir` are one function twice |
| 16 | Low | not DRY | six PowerShell resolvers |
| 17 | Low | not DRY | `short`, `shortSHA`, `plural`, `firstLine`, `tail` copied per package |
| 18 | Low | constants | `"claude/main"` spelled as a literal in 16 places beside two named constants |
| 19 | Low | constants | 64 inline `context.WithTimeout(..., N*time.Second)` and default ports in flag help |
| 20 | Low | constants | request body caps spelled `1<<12`, `4<<10` and `4096` |

## Shell-outs

### 1. High: a failed worktree list lets a clone with other worktrees be deleted

`internal/api/closeclone.go:36` counts the other worktrees of a clone with `worktreesOf(dir)` before the close may
delete the clone. `internal/api/providers.go:603` answers `nil` on any error, and it runs on `context.Background()` with
`worktreeListTimeout` of 2 s (`internal/api/providers.go:582`), not the close's own `ctx`. A slow disk, an index lock or
a busy machine makes the list fail, `others` stays 0, and `cloneKeepReason` goes on to say the clone may go. Its later
checks look only at the clone's own tree, stash and branches, so another card's worktree that hangs off the clone loses
its git dir.

Why it matters: every other answer in `cloneKeepReason` fails toward keeping. This one fails toward deleting.

Fix: give `worktreesOf` an error (or add `worktreesOfErr(ctx, repo)` and keep the old name for the list views), pass the
close's `ctx`, and return a keep reason such as "git could not list its worktrees" when it fails.

### 2. High: 24 git shell-outs bypass the hardened runner

`gitsync.Runner` (`internal/gitsync/run.go:157` to `:256`) is the one place git is run safely: every `GIT_*` variable
stripped, `GIT_TERMINAL_PROMPT=0`, no credential helper or proxy, maintenance not detached, a bound, `WaitDelay`, a
whole-tree kill on cancel and a stop on shutdown. `internal/daemon/changes.go:28` says "GIT RUNS THROUGH
gitsync.Runner". These do not:

| Site | Missing |
| --- | --- |
| `internal/api/close.go:433` `gitIn` | env strip, hardening, `WaitDelay`, tree kill |
| `internal/api/providers.go:609`, `internal/api/providerapi.go:391`, `:437` | env, prompt off, `WaitDelay` |
| `internal/api/openkinds.go:394`, `:412`, `internal/api/prworktree.go:235`, `:301`, `:349` | same |
| `internal/api/prbrief.go:58` | same |
| `internal/cardreview/cardreview.go:81` `git` | env, prompt off |
| `internal/daemon/cull.go:451` `isAncestor`, `:592` `gitIn` | env, prompt off, `WaitDelay` |
| `internal/daemon/finish.go:190`, `:203`, `internal/daemon/record.go:60` | same |
| `internal/daemon/moveroutes.go:163` `moveGit` | everything, including any timeout (see 3) |
| `internal/guard/env.go:40` | env strip (it adds `GIT_OPTIONAL_LOCKS=0`, which the runner lacks) |
| `internal/wtfree/wtfree.go:75` | env strip, `WaitDelay` |
| `internal/cli/roomsetup.go:255`, `internal/cli/merged.go:56`, `internal/cli/new.go:86` | CLI, no bound on two |
| `internal/base/land/duplicates.go:11`, `internal/base/land/identity.go:27` | no context, no bound |

Why it matters: an inherited `GIT_DIR`, `GIT_INDEX_FILE` or `GIT_WORK_TREE` points every one of these at the wrong
repository, and a credential prompt or a grandchild that keeps the pipe open turns a request into a hang. These are the
same causes the runner was written to remove, and the spawn count fixes landed for the runner only.

Fix: route the daemon, api and guard sites through `gitsync.Default` (or the daemon's own runner) with
`GitEnv(..., "GIT_OPTIONAL_LOCKS=0")` where the guard needs it. Keep each local wrapper's name and signature so callers
do not change, and let the body call the runner. Exit code 1 as an answer (`merge-base --is-ancestor`, `diff
--no-index`) needs the runner's `*Error` to expose the exit code, which is one small addition to `gitsync.Error`.
`internal/base` cannot import `gitsync`, so the two `land` sites take a context and a bound instead.

### 3. Medium: `moveGit` can hang a move request

`internal/daemon/moveroutes.go:162` runs `exec.Command` with no context, from the move check and move handlers that
hold a freeze lease. A git that waits on a lock or a prompt holds the HTTP handler until the client gives up, and the
lease runs out under it.

Fix: take a context bounded like `cullGitTimeout`, or call `gitsync.Default.Git` (finding 2).

### 4. Medium: the provider worktree endpoint does not check the branch name

`internal/api/providerapi.go:328` trims `req.Branch` from the request body and passes it as a positional argument to
`git worktree add <dest> <branch>` and `git rev-parse --verify refs/heads/<branch>` (`:387`, `:437`). A name that starts
with `-` becomes an option. `internal/api/openkinds.go:405` already has `checkBranchName`, which refuses a leading `-`,
`..` and anything `git check-ref-format --branch` refuses, and this path does not call it.

Fix: call `checkBranchName(ctx, branch)` after the empty check, and add `--` before the path and branch in the
`worktree add` argv.

### 5. Medium: runner forks have no `WaitDelay`

`internal/daemon/autocompact.go:44` sets `cmd.WaitDelay` because "A .cmd shim's child keeps the output pipe after the
shim is killed". `runForkProcess` at `internal/daemon/keepalive.go:499` runs the same runner through the same
`viaShellIfScript`, with `bytes.Buffer` stdout and stderr, and sets no `WaitDelay`. On Windows a keep-alive or a
context-limit fork that times out kills the shim, and `cmd.Run` then waits for the real `claude` child to exit.
Only 9 files under `internal` set `WaitDelay` at all.

Fix: set `cmd.WaitDelay` in `runForkProcess`, and in `internal/daemon/recognise.go:144`,
`internal/link/deployready.go:161` and the `sshwizard` `Run` (`internal/sshwizard/machine.go:116`), which read a pipe
from a tool that may be a shim.

### 11. Low: the autocompact probe serialises launches

`internal/daemon/autocompact.go:54` holds `p.mu` while `p.help(exe)` runs `--help` with a 15 s bound. Every launch
calls `takes`, so one slow runner holds every other launch for up to 15 s. A failed probe is not cached, so while the
runner is broken each launch probes again.

Fix: release the lock around the probe and keep one probe in flight per exe (a `singleflight` or a per-exe channel).

### 12. Low: the host key scan's bound cuts its own dials

`ProbeAddr` bounds a scan at `scanTimeout` 4 s (`internal/sshwizard/whichsshd.go:143`). `Keyscan` dials once per
algorithm, five in turn, each with `dialTimeout` 3 s (`:322`). A server that takes over 0.8 s per handshake is cut
before it offers its RSA key, and the probe reports fewer keys than it has. `Which` (`:236`) also probes every candidate
address of the same sshd in parallel, so the "never holds more than one of sshd's unauthenticated slots" promise holds
per address, not per sshd.

Fix: make the scan bound `len(hostKeyAlgos) * dialTimeout` or stop after the first key that matches a local key, and
probe loopback first, the rest only when loopback is not this machine's.

### 13. Low: combined output parsed as data

`internal/api/close.go:433` and `internal/daemon/cull.go:592` return `CombinedOutput`, and callers parse it:
`rev-parse --show-toplevel`, `status --porcelain`, `rev-list --count`. A git warning on stderr (a safe.directory note, a
`core.fsmonitor` notice) lands in the value. Most callers then fail toward keeping, but the answer given is wrong
("it has changed files").

Fix: read stdout as the value and keep stderr for the error, as `cardreview.go:87` does. Finding 2 gets this for free.

## DRY and not DRY

### 6. Medium: five protected-branch sets disagree

| Site | main | master | claude/main | hub-main | HEAD | `into` |
| --- | --- | --- | --- | --- | --- | --- |
| `internal/daemon/cull.go:438` | yes | yes | yes | no | no | yes |
| `internal/link/work_record.go:39` | yes | yes | yes | no | yes | no |
| `internal/worktrees/audit.go:52` | yes | yes | yes | yes | no | no |
| `internal/gitsync/served.go:56` | yes | yes | yes | yes | no | no |
| `internal/api/closeadopt.go:62` | yes | yes | yes | yes | no | no |

`hub-main` is in `gitsync.RoomOwned` (`internal/gitsync/locks.go:13`) and is a branch every room keeps, yet the cull
would delete a `hub-main` worktree branch that looked landed.

Fix: one exported set in `gitsync` (beside `IntegrationBranch`) and a `Protected(branch string) bool`, each site adding
only what is its own (`into`, `HEAD`).

### 7. Medium: eight worktree list parsers, four path rules

`git worktree list --porcelain` is run and parsed at `internal/api/providers.go:624`, `internal/wtfree/wtfree.go:86`,
`internal/daemon/hubworktree.go:82`, `internal/gitsync/hub.go:395`, `internal/gitsync/mergerun.go:550`,
`internal/shepherd/steps.go:130`, `internal/worktrees/audit.go:157` and `internal/daemon/hubworktree.go:41`. The path is
compared by `wtfree.EqPath` plus real path, by `sameRealDir`, by `filepath.Clean ==` (case sensitive on Windows,
`internal/daemon/hubworktree.go:104`) and by raw `==` (`internal/shepherd/steps.go:130`). The shepherd compares
`path.Join(e.Cfg.Worktrees, ...)` to git's spelling, so a config with `g:/` where git prints `G:/` misses an existing
worktree and the next `worktree add` fails.

Fix: one `gitsync.ParseWorktrees(out string) []Worktree` returning path, head, branch, detached, locked and prunable,
and every caller compares with `wtfree.EqPath` plus real path.

### 8. Medium: nine path-equality helpers

`eqPath` (`internal/api/browseroots.go:195`), `sameRealDir` (`internal/api/closeclone.go:76`), `sameDir`
(`internal/api/filelist.go:184`, lowercases on every OS), `samePath` (`internal/cli/close.go:166`, folds case on every
OS), `sameDir` (`internal/cli/remoteaccess_dir.go:26`), `sameDir` (`internal/daemon/hubworktree.go:104`, never folds),
`samePath` (`internal/daemon/reclaim.go:194`, folds on every OS while its comment says "the way the platform does"),
`wtfree.EqPath` (`internal/wtfree/wtfree.go:64`). On Linux two of them call `/a/X` and `/a/x` the same directory, and on
Windows one calls `G:\a` and `g:\a` different ones.

Fix: `safepath` (or `wtfree`) owns `EqPath` and `SameRealDir`, and the local helpers keep their names as one-line calls
to them so no call site changes.

### 14. Low: `git worktree add` three times

`internal/api/openkinds.go:394`, `internal/api/providerapi.go:391` and `internal/api/prworktree.go:235` each build the
argv, hide the window, run `CombinedOutput`, say "git could not make that worktree: " with `tailLines`, and call
`s.scopeHub`. Only `openkinds` checks the branch name (finding 4).

Fix: one `s.addWorktree(ctx, repo, dest, branch, from string, create bool) error` that checks the name, runs through the
runner and scopes the hub.

### 15. Low: `commitExists` and `commitInDir`

`internal/daemon/finish.go:184` runs `cat-file -e <sha>^{commit}` and `:197` runs `rev-parse --verify --quiet
<sha>^{commit}`, both with a literal 5 s bound and the same empty-dir guard. They answer the same question.

Fix: keep both names as seams and point them at one function.

### 16. Low: six PowerShell resolvers

`internal/daemon/script_windows.go:56` and `internal/api/shell_windows.go:12` prefer `pwsh.exe`.
`internal/api/newwork.go:328` and `internal/link/deployready.go:532` prefer `pwsh` by `LookPath`.
`internal/base/detach/outsidejob_windows.go:19` uses the System32 path. `internal/sshwizard/steps.go:246`, `:453`,
`internal/sshwizard/whichsshd.go:434` and `internal/cli/clipboard.go:22` run bare `powershell`. A machine with only
`pwsh` fails the last four.

Fix: one `shellpick.PowerShell()` (the package already lists both) used by all of them.

### 17. Low: small helpers copied per package

`short` or `shortSHA` in 8 files (`internal/cli/merge.go:87`, `internal/deployready/deployready.go:360`,
`internal/gitsync/hub.go:422`, `internal/hubstore/merge.go:219`, `internal/link/deps.go:232`,
`internal/link/lifecycle_git.go:167`, `internal/store/merged.go:55`, `internal/worktrees/audit.go:491`), `plural` in 4
with 3 signatures, `firstLine` in 6 with 3 argument types, and `tailLines` (`internal/api/providerapi.go:446`) beside
`tail` (`internal/shepherd/steps.go:31`). Each is a few lines, so the cost is drift, not size.

Fix: leave them unless a change touches one. Then move that one to `internal/base` and point the copies at it.

## Constants

### 9. Medium: `Runner.Bound` is "tests only" and production sets it

`internal/gitsync/run.go:30` documents `Bound` as "Tests only". `internal/daemon/landed.go:177` sets it to
`cullGitTimeout` on a fresh runner per call, and `internal/worktrees/audit.go:130` makes a fresh runner and sets
`CommandBound`. These runners are not `gitsync.Default`, so the daemon's shutdown `Stop` does not cancel them.

Fix: drop "Tests only" from the comment, since a per-call bound is a real need, and have `landedInto` use
`gitsync.Default` with a context deadline instead of a private runner.

### 10. Medium: one command, two bounds

`git worktree list --porcelain` is bounded at 2 s in `internal/api/providers.go:582` and at `gitWait` 60 s in
`internal/wtfree/wtfree.go:20`, and they fail opposite ways: `worktreesOf` reads failure as "none", `wtfree.Listed` as
"listed". Finding 1 is what the 2 s bound costs.

Fix: one bound for a local metadata read (10 s is enough for a cold disk) in `gitsync`, and a failure that says it
failed, as in finding 1.

### 18. Low: `"claude/main"` as a literal

`gitsync.IntegrationBranch` (`internal/gitsync/sync.go:25`) and `daemon.DefaultCullInto` (`internal/daemon/cull.go:64`)
both name it, and 14 more sites spell it out, among them `internal/daemon/changes.go:184`,
`internal/daemon/closehub.go:23`, `internal/gitsync/hubpush.go:180`, `internal/gitsync/branchstatus.go:183`,
`internal/gitsync/repos.go:39`, `internal/gitsync/store.go:724`, `internal/gitsync/sync.go:223`,
`internal/link/review.go:52` and `internal/shepherd/steps.go:15`.

Fix: `DefaultCullInto = gitsync.IntegrationBranch` and the literals replaced as each file is next touched. The sets in
finding 6 go with that fix.

### 19. Low: inline timeouts and ports

64 non-test calls pass a literal to `context.WithTimeout` (14 at 10 s, 12 at 30 s, 10 at 5 s, 10 at 20 s), most in
`internal/link`. The ones inside a request with no name say nothing about why that number. Default ports have named
functions in `internal/cli/atrium_defaults.go:19` to `:22`, but `internal/cli/cli.go:177`, `:179` and
`internal/cli/control.go:328` spell `7777` and `7778` again, as do 9 flag help strings.

Fix: name a timeout where it is a contract (a request bound, a git read) and leave one-off waits. Build flag defaults
and help text from the `default*Addr` functions.

### 20. Low: one cap, three spellings

Request bodies are capped with `io.LimitReader(r.Body, N)` at 25 distinct sizes. `1<<12` (12 sites), `4<<10` (9) and
`4096` (1) are the same 4 KiB. A capped body that is cut reads as a JSON syntax error, not as too large, except at the
few sites that read `max+1` and check.

Fix: a `readJSON(r, max, &v)` helper that returns 413 when the body is over `max`, used as handlers are touched.

## Not findings

- `ssh-keyscan` is gone. The scan is in Go (`internal/sshwizard/whichsshd.go:331`), one connection at a time per
  address.
- The `git log` storm fix (`ec135bc4d`, one log per ref tip cached until the tip moves) and the cull's one read of refs
  (`8689f365a`) hold at this tip.
- `ListenerOwner` (`internal/sshwizard/whichsshd.go:427`) puts `addr` into a PowerShell script, but only after
  `net.ParseIP` accepts it, so it cannot carry a quote.
- `exec.LookPath` on Go 1.19 and later does not resolve from the current directory, so the bare `git` and `powershell`
  names are not a hijack from a card's folder.
