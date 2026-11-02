## Test plan

## @LETTER@. land-check

### @LETTER@1. Check a hub branch

1. In a checkout with a `hub` remote, run `atrium land-check <branch>` for a branch the hub holds.
2. It prints the commits ahead of claude/main, `conflict: no` or the conflicted files, and the diffstat.

**Expected:** exit 0 when clean, 1 on a conflict, 2 for a branch the hub does not have.
