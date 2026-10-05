# r-hub-forge REPORT

## State

Done. Branch `claude/r-hub-forge`, based on claude/main d125ca85. It builds (`go build -o build.claude/ ./...`) and
every test this branch added or touched passes. clint's rule holds: a room attached to a hub never runs gh or bb and
never fetches from a forge host. Only the hub talks to GitHub or Bitbucket, and it only reads. Issue data was added
mid-run and also goes through the hub. The gh and bb logins live on the hub alone.

## What was built

- **The hub runs the forge.** `internal/link/forgeroute.go` serves `POST /_forge/pr`, `/_forge/issue` and
  `/_forge/repo` on the link's git kind, beside the PR claim. The asking room comes from the hello (`serveGit`), never
  from the room's headers. Questions are shape-checked before any argv is built. The forge runs through `forge.Exec`.
- **PR heads in the hub's store.** On `fetch` the hub holds the repository (`Store.Hold`) and fetches the head into
  `refs/atrium/pr/<N>` (`Store.FetchPR`, `internal/gitsync/storepr.go`). That fetch uses https only, has fsck on, and
  uses the forge CLI's own credential helper for that one command. When the store's main is empty (a private repo the
  seed could not read) it fetches the PR's base branch into main in the same command. `FetchError.Auth` marks a
  credential refusal.
- **Rooms ask the hub.** `forge.Remote` is the room's Forge over `Room.Forge`, set with `d.SetHubForge` when the room
  is started with a link. The PR runner, the PR worktree verb, the scm clone path and the recogniser use it. Rooms read
  the head and a clone from the hub's store through `gitsync.HubLoopback`. That is a loopback over the link's
  transport that serves `info/refs?service=git-upload-pack` and `POST git-upload-pack` for one repository and refuses
  everything else.
- **A room with no link keeps the local forge,** since then it is the hub. A room with a link whose hub is down fails
  with a sentence and never falls back to gh. A fallback would break the rule.
- **Access check and alert on the hub.** An `AccessError` from the hub's CLI, or a head fetch with `FetchError.Auth`
  (fix-up c), raises one growler `forge|<tool>@<host>|<ts>` hung on the asking room. The sentence names the hub and
  the login to run there. It is raised once while open (also across a restart, through `Live`), and the next answer
  that works ends it. `POST /_hub/forge/check` runs the hub CLIs' status commands. It answers and ends the alert on
  ok. It raises no growler on a failure, since no room is waiting on it.
- **Provider per host on the hub (fix-up a).** The hub setting `forge.entries` (`GET`/`PUT /_hub/forge`) names the
  forge and command per host, with the built-in table as the fallback. The room's PR worktree verb finds its provider
  by name, else by the body's `host`, so names need not match across rooms.
- **A refused placement releases the claim (fix-up b).** `releaseOnRefusal` wraps the writer when a placement made a
  claim. On a 4xx or 5xx from the placed room it calls `ReleasePRClaim(key, room)`, which deletes only while that
  room still owns it, and ends any offline warning.
- **Recognisers.** A built-in fetch `forge pr` or `forge issue` with gh's fact names. With a hub it peeks (no head
  fetch for a paste). A row whose fetch runs gh, bb or glab is refused on a room with a hub. `scripts/recognisers/`
  rows use `forge`.
- **Room forge config removed.** The board's "forge logins" block and its js, the `forge` settings key,
  `POST /v1/forge/check`, `store.ForgeConfig`. Migration `0083_drop_room_forge_config` deletes the `forge.%` rows. A
  room preflight on a room with a hub runs no CLI and says the logins are the hub's. `forge_access` (open alerts) stays
  in the room settings for a room with no hub.
- **Docs.** `docs/rnd/scm-forge-design.md` sections 0, 2, 5, 6, 8, 9, 10, Built, and clint's rule recorded as answer 9.
  `changelog/runtime/2026-10-04-r-hub-forge.md`. The `forges:` comment in `atrium.requirements.yaml` and the
  `requirements.Forge` doc now say it is the hub's login.

## Where the shape differs from the brief, and why

- The hub's routes are on the link's git kind (`/_forge/...`) and not a new kind. The PR claim already uses that
  kind with the room taken from the hello, so the trust model did not change.
- Rooms read the store through a loopback proxy and not the `hub` remote's card-token forwarder. The store's
  upload-pack needs no card, and the loopback is narrower (one repo, fetch only).
- Room fetches from the store are whole fetches (no `--depth`, no `--filter`), because the store refuses both.
- The forge entries per host are a hubstore setting and not provider rows, because providers are per room.
- Hub alerts are growlers, because that is how hub alerts already reach the board.

## Tests added

- `internal/forge/remote_test.go`: Remote round trip (View fetches, Diff, Head is not recorded, Peek, Issue, Repo,
  FetchSpec and PRURL before and after an answer, a HubError handed back), StoreName.
- `internal/gitsync/storepr_test.go`: FetchPR against a local bare repo, an empty main filled from the base, refusals
  (dst outside the prefix, option or two-sided refspecs, a repo not held), a missing head that is not Auth, the auth
  classification of git's words, and that the helper is set for that one command.
- `internal/gitsync/forward_test.go`: HubLoopback serves only one repo's upload-pack and refuses receive-pack, other
  repos, other paths and wrong methods.
- `internal/link/forgeroute_test.go`: the hub fetches a head into its store end to end, forge picked by host and
  no_forge, bad questions run nothing, the growler raised once and ended on success, a fetch auth failure raises it
  and a plain fetch failure does not, the operator check raises nothing, the entries GET and PUT with validation, a
  room asking over a real link, and a hub with no forge answering a sentence.
- `internal/link/prworktreeroute_test.go`, `internal/hubstore/prclaim_test.go`: a refused worktree releases the claim
  and a retry is placed again, a refusal leaves an older claim alone, and ReleasePRClaim deletes only the room's own.
- `internal/daemon`: the PR runner on a hub Remote fetches through the loopback (no gh, no forge URL, whole fetch,
  loopback closed). forgeFor answers the hub's forge for every host. The recogniser forge fetch for pr and issue, a gh
  fetch refused, and a bad argument. A preflight on a room with a hub runs nothing.
- `internal/api/prworktree_test.go`: a hub head fetched with no credential and http only, the hub store path taken
  with its error said, and the provider found by host.

## Test run (macOS)

`env -u ATRIUM_LOCATION -u ATRIUM_DEBUG_INPUTLAG go test ./...`: everything passes except packages with failures that
are environmental and not in this branch's code:
- `internal/daemon` and `internal/ptyhost`: `listen unix ... bind: invalid argument` (the temp socket path is too long
  on macOS). `TestNoTestHereCanReachALiveRoom` sees a Windows `claude.cmd` path. `TestKeepaliveForkCarriesALeanCards...`.
- `internal/api`: `TestTheWalkerLaunchSetAndClear` on the /private/var vs /var symlink.

## Commits

f302096e groundwork, 906d2126 steps 1-5, bfea028e recognisers, f47a3549 room forge config removed, 2dc2bd27 tests,
c55d417a docs and changelog, 615163fa pr-worktree tests, and this report.

## Not done, and open

- GitLab is later, as the brief says. `glab` is refused as a fetch on a room with a hub and has no forge.
- Not exercised against a real gh or bb login or a real multi-machine link. The end-to-end hub fetch test uses a
  local bare repo standing in for the forge.
