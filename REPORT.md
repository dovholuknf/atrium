# r-forge-gh report

Branch `claude/r-forge-gh`, based on 5fc5b390.

## Built
- `internal/forge`: `Forge` interface (`Kind`, `View`, `Diff`, `Head`, `FetchSpec`, `PRURL`), `Ref`, `PR`
  (title, author, head, head ref, base ref, `FromFork`, files), `Cmd`/`Runner`, `Pick`/`New`/`For`,
  `NoForgeError` (code `no_forge`), `AccessError{Tool, Host, Detail, NotInstalled}`. GitHub through `gh`
  (`github.go`). No "list review requests" method.
- Selection by host: an enabled provider with that host and its `forge` (a provider that says `none` decides),
  else `github.com` is github, `bitbucket.org` is bitbucket, else `no_forge`. Bitbucket and GitLab return a
  `no_forge` sentence saying the forge is not built.
- Store: migration `0081_provider_forge` added at the END of the slice (`forge`, `forge_cmd`, no CHECK).
  `ProviderForges` list validated in `SaveProvider`, forge lower-cased, `forge_cmd` must be a bare name (no path,
  space, `=`). Fields are on `store.Provider` JSON, so the provider API carries them. Did not touch `internal/api`.
- Runner (`internal/daemon/prrunner.go`): `fetch` uses the forge for view, diff, fetch spec. `prCmd` is now an
  alias of `forge.Cmd`. `runBounded` wraps with `%w` so a missing binary is detectable. `pr.json` is now the
  forge-neutral `forge.PR` (an old gh-shaped `pr.json` fails to read and the step refetches). `forgeOf` seam for
  tests. A failed row's reason is `fetch: <sentence>`, so `no_forge` and the AccessError sentence appear on it.
- Changelog: `changelog/runtime/2026-10-04-r-forge-gh.md`.

## Decisions
- `AccessError` has one extra field, `NotInstalled`, to pick the sentence. Not-logged-in is detected from gh's
  stderr text ("gh auth login", "not logged in", "authentication required", "http 401", "bad credentials"). Any
  other error passes through unchanged.
- Nothing else in `internal/forge` runs `gh auth status`. Preflight and `internal/api/web` untouched.
- An empty host on a row is treated as `github.com`, as before.

## Not found / left in place
- There is NO review-requested source code and no separate head check in this tree. `Head()` exists on the
  interface for the future head check, nothing calls it yet.
- `gh` is still named in: `internal/daemon/preflight.go:57` (tool table, f-forge-access owns it),
  `store.DefaultWalkerBrief` in `internal/store/prs.go` (a seeded, operator-editable prompt saying
  `gh pr view <n> --json headRefOid`, left because it is a migration-seeded string), and test fixtures
  (`prrunner_test.go` fake, `import_test.go`, `export_test.go`).

## Not done
Worktrees, room placement, board, preflight, Bitbucket, GitLab, polling.

## Tests
- `go test ./internal/forge ./internal/store`: ok.
- `go test ./internal/daemon -run 'PRRunner|Provider'`: ok (new: runs with a fake forge and no gh, no_forge sentence,
  AccessError sentence as the row reason, provider-host forge pick).
- Full `go test ./internal/daemon` had failures in host-terminal/reattach/keepalive tests
  (e.g. `TestReattachReplays...`, `TestNoTestHereCanReachALiveRoom`, `TestKeepaliveFork...`). I did not run them on the
  baseline, so I cannot say they predate this change. They are not in code I touched.
- `go test ./internal/api` had one failure, `TestTheWalkerLaunchSetAndClear` (compares a cwd, looks like a
  macOS `/private/var` path difference). Not in code I touched, not checked against baseline.
