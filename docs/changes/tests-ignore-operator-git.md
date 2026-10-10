## Test plan

## @LETTER@. Tests ignore the operator's git config

### @LETTER@1. Hostile global config

1. Make a hooks directory whose pre-push, pre-commit and commit-msg hooks run `exit 1`.
2. Make a gitconfig with `core.hooksPath` at it and `commit.gpgsign=true`, and no user.name or user.email.
3. Set `GIT_CONFIG_GLOBAL` to it and run `go test -tags integration ./internal/gitsync/... ./internal/link/... ./internal/api/... ./internal/daemon/...`.

**Expected:** no test fails on a hook, a signature or a committer identity.
