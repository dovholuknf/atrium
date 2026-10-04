# r-pr-worktree report

Branch `claude/r-pr-worktree`, based on `claude/r-forge-gh` (84cf15ab). That branch's report is replaced here, its
commit is 84cf15ab.

## Built
- `POST /v1/providers/{name}/pr-worktree` with `{org, repo, number}` (`internal/api/prworktree.go`).
  1. Reads the PR through the forge (`View`), picks the branch: the real head ref for a same-repo PR, `pr-<N>` for a
     fork PR (also when the head ref is empty).
  2. Repo path is the provider's checkout when there is one. Otherwise it goes through the scm clone path
     (`SCMClone`, wired to `Daemon.prWorktreeClone` over `gitsync.SCM.Clone`, so the `hub` remote and the guarded
     `origin` come with it) and uses the clone it returns. The "atrium does not clone" refusal is gone for this verb.
  3. A worktree already on that branch returns `existed: true` with its path.
  4. Fetches the PR head with the forge's `FetchSpec` into `refs/atrium/pr/<N>` (https only), then `git worktree
     add`. A new local branch starts at that ref, and a local branch that already exists is checked out as it is.
- Server seams: `PRForge`, `SCMClone`, `PRFetch`. The daemon sets the first two, the third is a test seam.
- The older `/worktree` verb is unchanged and still refuses to clone, its test still passes.
- Changelog `changelog/runtime/2026-10-04-r-pr-worktree.md`.

## Decisions
- The forge is asked FIRST. A missing or logged-out CLI answers its `AccessError` sentence unchanged (HTTP 400,
  `error` field) before anything is cloned. `no_forge` does the same.
- A clone failure answers `gitsync.CloneFailed` unchanged (400). A failed fetch is its own sentence, "could not fetch
  the head of pull request N: ...", since it is not a clone failure.
- The fetch uses the forge's `FetchSpec` remote (the forge's https URL, `pull/N/head`), not the `hub` remote. The
  hub's store has no PR refs that I could find, so the PR head has to come from the forge host. It carries no
  credential helper, so a private repo's PR fetch fails with the fetch sentence. Say if you want the clone's
  credential helper passed to the fetch.
- There is no card on this verb, so the one yes for a clone the OPERATOR made in the scm folder cannot be asked on
  the board. That case answers a sentence saying to run `atrium_git_clone` from a card first (`errPRCloneAdopt`).
- Idempotency is on branch, as the old verb. A worktree for the same PR on a differently named path is not found.
- The clone url is `https://<provider host or github.com>/<org>/<repo>`.

## Not done
- Room placement, claims, access alert and requirements, board, Bitbucket, GitLab, polling: out of scope.
- No UI or tool calls the new route yet.
- The real `SCM.Clone` is not run by a test of this verb. Its own tests in `internal/gitsync` cover it, and the
  verb's tests use a fake clone seam that makes a local clone.

## Tests
- `go test ./internal/api -run 'PRWorktree|Worktree'`: ok. New: same-repo real branch (and worktree at PR head), fork
  `pr-7`, idempotent second call, no checkout goes through the clone seam (called once, worktree under the worktree
  root, second call existed), exact `CloneFailed`, `AccessError` unchanged with no clone.
- `go build ./...` and `go vet ./internal/api ./internal/daemon`: ok. Full package suites not run.
