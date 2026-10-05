# r-hub-forge REPORT

## State

Incomplete. I stopped at about 151k context because the launcher told me to. A fresh context continues from BRIEF.md
and this file. Scope added by clint mid-run: issue data also comes through the hub, not only PRs. gh and bb
credentials live on the hub only.

### Done (committed, builds, `go test ./internal/forge` passes)
- `internal/forge/issue.go`: `Issue` type and the `IssueReader` interface, with `Issue` on gh
  (`gh issue view --json title,body,author,state,url`) and on bb (`bb api /repositories/o/r/issues/N`). It is a
  separate interface, so the existing fake forges in tests still satisfy `Forge`.
- `internal/forge/exec.go`: `forge.Exec(prepare)`, a bounded Runner for the hub. It does what the PR runner's
  `runBounded` does, with `%w` on a run error so `exec.ErrNotFound` still turns into an AccessError.
- `internal/forge/forge.go`: `FetchSpec.Hub`. When it is set, Remote is empty and the head is a ref in the hub's
  store.
- `internal/forge/remote.go`: the wire types of the hub routes (`HubPRPath` `/_forge/pr`, `HubIssuePath`
  `/_forge/issue`, `HubRepoPath` `/_forge/repo`, `HubAsk`, `HubPR`, `HubIssue`, `HubRepo`, `HubError` with codes
  no_forge/access/fetch/other) and `forge.Remote`, the room's Forge over a `Call` func:
  - View asks with `Fetch: true`, so the hub fetches the head before answering.
  - Diff asks with `Diff: true`, and Head asks with `HeadOnly`.
  - Issue and Repo are their own calls.
  - FetchSpec answers `{Hub: store name, Refspec: refs/atrium/pr/N}`.
  - PRURL is the URL the hub gave, else the built-in shape for the host.
  - `PRRef(n)` and `StoreName(host, org, repo)` are helpers. StoreName spells github.com as `github`.
- `internal/gitsync/storepr.go`:
  - `Store.Hold(ctx, url)` is Init and answers the store name, so a room's request can put a repo in the hub's store.
  - `Store.FetchPR(ctx, name, remote, refspec, dst, base, helper)` fetches the head into `refs/atrium/pr/N` with
    fsck on and the forge CLI's credential helper for this one command only. When main is empty it also fetches the
    base branch into main, which covers a private repo the seed could not read.
  - `FetchError{Auth}` marks a missing credential, for fix-up c.
  - Not yet tested.

### Remaining, in the shape decided (adjust if the code disagrees)
1. **Hub route** `internal/link/forgeroute.go`, served on the git kind beside the PR claim. In `internal/link/git.go`
   serveGit, dispatch the `/_forge/` prefix to a new `Hub.Forge http.Handler` and set the asking room in a header, as
   PRClaim does.
   - The handler picks the forge per host from hub-side entries (fix-up a), held in a hubstore setting such as
     `forge.entries` as JSON `[{host, forge, cmd}]`, with `forge.For` and the built-in table as the fallback.
   - It runs with `forge.Exec(hideWindow)`.
   - `/_forge/pr`: View, Diff when asked, and on Fetch it does
     `Store.Hold("https://host/org/repo")` then
     `Store.FetchPR(name, spec.Remote, spec.Refspec, forge.PRRef(N), pr.BaseRef, helper)`. The helper is
     `!gh auth git-credential` for github, as `credentialHelper` in `internal/api/prworktree.go` does, and none for
     bb.
   - `/_forge/issue` uses `forge.IssueReader`. `/_forge/repo` uses Hold.
   - Errors go out as `forge.HubError` JSON with a non-200 status.
2. **Hub alert**:
   - Raise: an AccessError, or a `gitsync.FetchError` with Auth (fix-up c), raises a growler through `growlRaiser`
     with id `forge|<tool>@<host>`, hung on the asking room. The title says the hub's CLI is not logged in, and the
     body gives the login command to run on the hub.
   - Clear: a later success ends the growler.
   - Check: add an operator-local `POST /_hub/forge/check` that runs `<tool> auth status` on the hub. Port
     `checkForge`, `forgeScopesIn` and `forgeSay` from `internal/daemon/forgeaccess.go`. It takes the same
     `{tool, host, scopes}` list as the `forges:` requirement. Add a GET/PUT for the hub entries.
3. **Room client**: add `Room.Forge(ctx, path, in, out)` in `internal/link`, as `Room.ClaimPR` does (GitTransport,
   `HubServesGit`, decoding a HubError on a non-200). In `internal/cli/roomrun.go`, give the daemon a
   `d.SetHubForge(forge.NewRemote(...))`.
4. **Room uses the hub when it has one** (a room started with a link, attached or not). A room with no link keeps the
   local forge, because it is the hub. A room with a link whose hub is down fails with a sentence and never falls
   back to gh.
   - `prrunner.forgeFor`: answer the Remote.
   - `fetchSource`: when `spec.Hub != ""`, fetch through a loopback, read-only proxy to the hub store over the link's
     GitTransport. Still to write as e.g. `gitsync.HubLoopback(rt, name) (url, stop, err)`. It serves only
     `info/refs?service=git-upload-pack` and `POST git-upload-pack` for that one repo, under a random secret path
     prefix, and closes after the fetch. The store's upload-pack needs no card. It has
     `uploadpack.allowFilter=false`, so `--filter=blob:none` is ignored with a warning, which is harmless.
   - `internal/api/prworktree.go`:
     - Forge: View through the Remote.
     - Repo with no checkout: when a hub is present, `Remote.Repo` then clone from the loopback URL. Add an
       exported source override to `gitsync.SCM`, then set origin's URL to the forge https URL so the guard and
       originMismatch keep working.
     - Head: fetch it from the loopback into `refs/atrium/pr/N` instead of `fetchPRHead` against the forge.
   - Fix-up a: find the provider by name, else by `host` from the body (the hub already sends `host`), so the name
     need not match on every room.
5. **Fix-up b**:
   - Wrap the ResponseWriter in `internal/link/prworktreeroute.go` (or proxy.go around `placePRWorktree`). When the
     placed room answers 4xx or 5xx for a key this request claimed (`made`), release the claim.
   - Add `ReleasePRClaim(key)` to `internal/hubstore/prclaim.go`, deleting the row and ending any warning growler.
6. **Issues and recognisers**:
   - Add a built-in recogniser fetch `forge` with args `pr` or `issue` in `internal/daemon/recognise.go`. With a hub
     it goes through the Remote, and without one through the local forge. It emits facts under gh's names (pr:
     title, headRefName, baseRefName, isDraft. issue: title, body).
   - On a room with a hub, a row whose fetch names gh, bb or glab is refused with a sentence telling the operator to
     use `forge`.
   - Update `scripts/recognisers/*.json`.
7. **Remove the room forge config**:
   - The board's "forge logins" block: `internal/api/web/index.html` near line 2060, plus saveForge and checkForge in
     the js.
   - The `forge` key in the room settings GET and PUT: `internal/api/settings.go` lines 198, 307, 667 and 959-977.
   - `/v1/forge/check`: `ForgeCheck` in api.go and `handleForgeCheck`.
   - The forges in room preflight: `internal/daemon/preflight.go` lines 86-179.
   - `store/forgecfg.go` settings: keep the tool table, drop `forge.<tool>.host/cmd`. Add an append-only migration
     at the end of `internal/store/schema.go` that deletes the `forge.%` settings rows.
   - A room with no hub keeps `RaiseForgeAccess` with the default command names.
8. **Tests** for each:
   - Remote forge round trip with a fake Call.
   - FetchPR against a local bare repo (`Store.Protocols = "file"`), including the auth classification.
   - The hub route with a fake forge, and the growler raised on access and on a fetch auth failure.
   - The claim released on a refused worktree.
   - The PR runner on a hub Remote fetching through the loopback.
   - The recogniser `forge` fetch.
9. **Docs and changelog**:
   - Update the Built section and sections 0, 2, 6, 9 and 10 of `docs/rnd/scm-forge-design.md`: the hub runs the
     forge and rooms never do.
   - Write `changelog/runtime/2026-10-04-r-hub-forge.md`.
