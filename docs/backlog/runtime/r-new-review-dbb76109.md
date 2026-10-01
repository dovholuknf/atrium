# Review of r-pr-run dbb76109 (pulls view P1 part 3, the runner): HOLD

Range d33bd3b5..dbb76109 (the merge-base with claude/main is d33bd3b5, not 59d21773: the branch also carries the
live412-changes merge, which is already on claude/main). New code: 2f587290 and dbb76109, `internal/daemon/prrunner.go`,
its tests, `forkSpec` and `forkReceipt` in keepalive.go, the wiring in daemon.go.

## HIGH (proven): a PR's own `.claude/settings.json` runs its hooks on the reviewer's machine

`call()` runs the prime and every fork with `Dir: <run>/src`, which is the PR head checked out from the author's
branch, and `forkCommon` takes `leanArgs`, which sets `--setting-sources project,local`. So the PR's checked-in
`.claude/settings.json` (and a committed `.claude/settings.local.json`) is the project source. `claude -p` shows no
trust dialog, so its hooks run. `--tools Read,Grep,Glob` does not limit a hook: a hook is a shell command. Anyone who
opens a PR against a repo with a recipe gets code run as the daemon's user, with its `gh` token, the moment a review
starts (the prime is enough). The PR's CLAUDE.md, agents and skills load too, which is prompt injection into every
reviewer.

Proof: D:/worktrees/claude/reviews/github-dovholuknf-atrium/proof-dbb76109.ps1. A `src/.claude/settings.json` with a
SessionStart hook `touch ../PWNED-sessionstart`, then the fork's shape (`-p`, cwd src/, `--setting-sources
project,local`, `--tools Read,Grep,Glob`, ATRIUM_PERM_GATE=off). Printed `PROVEN: the PR hook ran`.

Fix: never start claude with the untrusted checkout as its project. Run the prime and forks with cwd in a folder
atrium wrote (the run folder or `steps/`), give `src/` by `--add-dir`, and pass a setting source that cannot come from
the PR. Keep the prompt saying where the source is. Test: a case whose fake checkout writes `src/.claude/settings.json`,
asserting every forkSpec's Dir is not under `src/` and its args carry no `project` setting source. Also re-run the
proof against the real binary.

## MEDIUM (read): a retry after the merge step finished renders an empty review as ready

`call()` writes each fork's receipt to `steps/<dir>/out.json`, and the merge fork's dir is `merge`, so
`steps/merge/out.json` is the claude receipt. The merged list goes to `steps/merge/findings.json`. But merge's replay
branch reads `merge/out.json` into `prFindings`. A receipt has no `findings` key, so the decode succeeds with nil and
`pr.final` is empty. Any retry where merge is Done reaches `write` with no findings: a budget stop at `write` (the
budget check runs in `step("write")` after merge spent), or a `write` error. `prrender.Render` then writes a walk with
nothing in it and the row goes `ready`, which reads as a clean PR. Fix: replay from `merge/findings.json` and
treat a missing file as not done. Test: run to a budget stop at `write`, ResetPR, run again, expect the 3 fixture
findings.

## Low

- `rename` moves the folder before the row names it. An abort read between `os.Rename` and `SetPRFetched` deletes the
  old, already moved path and leaves `pr-<n>-<head7>` on disk.
- A retry with the panel step not done appends the panel's names to `review.Panel` again.
- Known and accepted by @runtime: rows in `fetching` or `running` after a daemon restart are never recovered, the
  consumers critic cannot read outside `src/`, prompts untested against the real claude, live 378 replay not run.

## Tests

On Windows (sg4), detached worktree at dbb76109: `go vet ./internal/daemon/` clean, `go test ./internal/daemon/ -run
PRRunner -count=3` ok. The macOS socket-path failures @runtime named are not this change's. No case covers the high or
the medium, which is why both pass.

Quality: after the Sonnet switch, the shape of this work is sound: bounded commands, state-guarded moves, abort
handled before writes. The high is a trust boundary the design did not name, which is where a review should catch it.

## Re-read at 8c9b17d7 (2026-10-01, m1mini): ROOM DEPLOY OK d33bd3b5..8c9b17d7

One commit over dbb76109, `internal/daemon/prrunner.go` and its tests. Unsigned (no key on the m1mini worker).

**High, closed and proven.** `forkCommon` (prrunner.go:754) now builds `<run>/work`, rewrites `--setting-sources` to
`user`, and passes `--add-dir <run>/src`. `call` runs every fork with `Dir: pr.workDir`, and `prime` builds the
common args before naming `pr.srcAbs` in its prompt. Proof on m1mini, claude 2.1.287, with a `src/.claude/settings.json`
and a `src/.claude/settings.local.json` that each touch a marker on SessionStart and UserPromptSubmit, a
`src/CLAUDE.md` canary and a `src/.claude/skills/canary` skill:

- Control, the old shape (cwd `src/`, `--setting-sources project,local`): all 4 markers appear.
- Fixed shape (cwd `work/`, `--setting-sources user --add-dir src --tools Read,Grep,Glob`): no marker. The fork
  reports no canary instruction and no skill from src/. `--add-dir` loads no settings, hooks, CLAUDE.md or skills
  from src/.

**Medium closed.** Replay reads `steps/merge/findings.json` (prrunner.go:1293), a missing file reruns merge, and the
resend writes the accepted list back (prrunner.go:1373). `TestPRRunnerRetryAfterMergeKeepsTheFindings` and
`TestPRRunnerReplaysTheResentListAfterARetry` cover both.

**Lows closed.** Panel reset per panel step (prrunner.go:1021). `rename` reverts on a `SetPRFetched` error and removes
the new folder if the ctx was cancelled across the rename. That window has no test, as @runtime said; it reads right.

### New medium: `user` loads the operator's whole settings.json into every fork

`leanSettings` exists to pass a curated copy of the user settings through `--settings`: unknown keys dropped, and
SessionStart and UserPromptSubmit cut to atrium's own hooks. `--setting-sources user` loads `~/.claude/settings.json`
as well, unfiltered, so the lean filter is bypassed and atrium's hooks register from both places. Proven with a
throwaway `CLAUDE_CONFIG_DIR` whose settings.json has a SessionStart hook: with `user` the hook fires, with `""` it
does not. A `--settings` hook fires under both. On m1mini every user hook is atrium's, so nothing foreign runs today,
and the PR cannot reach this. That is why it holds nothing. The user CLAUDE.md and skills likely ride along too (not
proven: a throwaway config dir has no login). Fix: `lean[i+1] = ""`. claude takes an empty source list, and
`TestPRRunnerNeverRunsInThePRsCheckout` already allows it.

### Low

- `rename` ignores the error from the reverting `os.Rename(to, old)`. If that fails, `pr.dir` names a path that is gone.
- `TestKeepaliveForkCarriesALeanCardsPromptToolsAndMCP` fails on macOS, on claude/main too, so it is not this change.
  Its must-not `"/x"` matches the temp path `/var/folders/xd/...`. That is @runtime's.

### Tests

m1mini, detached worktree at 8c9b17d7: `go test ./internal/daemon/ -run 'TestPRRunner'`, 10 of 10 pass, the 3 new ones
included. The wider `-run 'PR|Pr|Lean'` fails only on the keep-alive case above.

Quality: after the Sonnet switch, the fix is tight and targeted, and its test pins the property, not the flag
spelling. `user` was picked without checking what that source brings in: the one gap.

Verdict: ROOM DEPLOY OK d33bd3b5..8c9b17d7. The setting-sources medium is to be fixed in the next r-pr-run patch.

## Re-read at a2a1909d (2026-10-01, m1mini): ROOM DEPLOY OK d33bd3b5..a2a1909d

One commit over b07cbfb2 (claude/landing merged in first). Unsigned.

- **The setting-source medium is closed.** `forkCommon` sets `--setting-sources ""` (prrunner.go:777). Forks start
  through `exec.CommandContext` with an argv (keepalive.go:475), so the empty value arrives intact on macOS and
  Windows. Proven on m1mini with the real login: from `work/` with `""`, the fork reads `src/file.txt` through
  `--add-dir`, and no src/ marker or canary appears. Hooks passed through `--settings` still fire, and the user
  settings.json's own hooks do not (the throwaway `CLAUDE_CONFIG_DIR` run above).
- **The rename low is closed.** A failed revert keeps `pr.dir` on the folder that exists and returns an error naming
  both failures (prrunner.go:625).
- Nit: the test asserts the value only when `--setting-sources` is present. If it were ever dropped, every source
  would load and the test would still pass. leanArgs always emits it today.

Tests: `go vet ./internal/daemon/` clean, `go test ./internal/daemon/ -run TestPRRunner` 10 of 10.

Quality: after the Sonnet switch, both fixes are minimal and match the review exactly, and the comment says why.

Verdict: ROOM DEPLOY OK d33bd3b5..a2a1909d.
