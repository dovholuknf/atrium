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
