# r-forge-wire report

## Built
- Job 1: `Daemon.ForgeAccessFrom(kind, err)` finds a `*forge.AccessError` with `errors.As` and calls `RaiseForgeAccess`.
  `Daemon.ForgeWorked(kind, host)` clears the open alert through `applyForge` when the forge next answers, and says
  nothing when none is open. The PR runner calls them after `View` and `Diff` (`onAccess`, `onWorked` fields). The
  pr-worktree verb calls them through new `Server.ForgeFailed` and `Server.ForgeWorked` seams the daemon fills.
- Job 2: the default PR head fetch now passes `-c credential.helper=` then `-c credential.helper=!<cmd> auth
  git-credential` for that one git command. `<cmd>` is the provider's `forge_cmd`, else `gh` for the github kind. A
  forge with no helper gets no change. No token is read, stored, logged or put in the argv. A failed fetch still
  answers "could not fetch the head of pull request N".
- Changelog `changelog/runtime/2026-10-04-r-forge-wire.md`.

## Decisions
- The alert's tool comes from the forge kind (github is gh), so a provider's wrapper command still raises the alert of
  its tool. An unknown kind falls back to the AccessError's own tool.
- "Scoped to the forge host" is met because the one fetch only talks to the forge's URL. The helper is not set as a
  host-keyed config, since an empty reset there would not drop the global helpers.

## Tests
New: runner access/success hooks, `ForgeAccessFrom`/`ForgeWorked`, pr-worktree raise and clear, helper argv, fetch
failure sentence. The helper path is tested at the argv level, not against a real https server.

## Not done / pre-existing failures
- Git fetch failures that are auth failures (not an AccessError) do not raise the alert.
- Unrelated failures here: TestTheWalkerLaunchSetAndClear (api), and in daemon the ptyhost unix-socket tests, 
  TestNoTestHereCanReachALiveRoom and similar, which fail on this machine's paths.
