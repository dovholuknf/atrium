## Test plan

## @LETTER@. The ubuntu CI container runs as the runner user

### @LETTER@1. go-all passes in the container

1. Dispatch ci with os ubuntu and suite go.

**Expected:** `check (ubuntu-latest, go-all)` passes. The build reports no VCS status error,
`TestWorktreeToggleOffRefusesAnUnreadableDirectory` and `TestKillEndsTheRunnersChildren` pass.

### @LETTER@2. phoneShare passes after the image is rebuilt

1. Rebuild and push `ghcr.io/dovholuknf/atrium-ci:1` with `bash scripts/ci-image.sh`, then dispatch ci with os ubuntu
   and suite ui.

**Expected:** the board-visual step reports `phoneShare` as passing.
