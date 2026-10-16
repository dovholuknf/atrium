## Test plan

## @LETTER@. Landing refuses a private email

### @LETTER@1. The refusal

1. In a throwaway repo, run `git config user.email someone@example.com`, then `go run scripts/land-history.go` from it.
2. It stops with `STOP: user.email is "someone@example.com" (from file:...config ...)` before any commit is rewritten.
3. With the noreply address `46322585+dovholuknf@users.noreply.github.com` it goes on.

### @LETTER@2. Shepherd worktrees

1. After a shepherd run, `git -C <worktree> config user.email` prints the noreply address.
