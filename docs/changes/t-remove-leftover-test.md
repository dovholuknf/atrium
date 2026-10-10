# t-remove-leftover-test

`TestRemoveLeftoverRemovesWhatGitLeftAndNamesWhatItCannot` failed on macOS.

## Cause

The code was wrong, not the test. `worktreeListed` compared the path it was given with the paths in
`git worktree list --porcelain` as plain strings. Git prints the resolved path, so on macOS a worktree under `/var/...`
is listed as `/private/var/...`. The two never matched, `removeLeftover` believed git no longer listed the worktree and
deleted a live one. The same would happen for any worktree root reached through a symlink.

## Fix

`worktreeListed` now also compares both sides after resolving symlinks (`realPath`). The test is unchanged and no
assertion was weakened.

## Test plan

## @LETTER@. A symlinked worktree is not swept

### @LETTER@1. Leftover removal keeps a listed worktree

1. Run `go test -tags integration -count=1 -run TestRemoveLeftover ./internal/api` on macOS.

**Expected:** the test passes, the listed worktree is kept and reported as still a worktree.
