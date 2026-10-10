# repos-page-fast report

## Cause
`GET /_hub/git/repos` called `Store.View`, which ran `git for-each-ref` once per repository, one after another, on the
request (internal/gitsync/store.go `viewEntry` -> `heads`), plus a push-log query per repo. On sg4 a spawn is 50-100ms
and slower under load, so a list of repos took 2-12s.

## Fix
The request now reads `Store.Snapshot()` (internal/gitsync/viewcache.go): memory plus one directory listing, no git.
The snapshot is rebuilt in the background after a push, after an init, and every 20s. The answer carries `computed`,
`age_seconds` and `building`. Before the first build finishes it lists repos with no branches and `building: true`;
the page paints that and asks again after 1.5s. `Runner.Spawned` counts git processes; the test asserts 100 requests
start 0 and each takes under 50ms.

## Other hub GETs
NOT MEASURED. The only hub reachable from this machine (127.0.0.1:7777) answers 404 to every /_hub/* path (it only
serves git), and I may not touch sg4. Read from the code instead, for the GETs the board calls:
- git/pushed (changerequest.go servePushed): runs git (ancestry checks) on the request. Likely slow on sg4 with many
  branches. Called per change request row.
- deploy-ready: runs `<installed binary> version` on the request, cached by binary path/mtime/size, so only the first
  call after a deploy spawns. Also may run git for the branch tip. Worth timing.
- health: `buildFor` hashes the board tree at most once per viewTTL; first call after expiry reads files.
- rooms, presence, hosts, growls, growl-ladder, audit, views, inventory, setup-check, deploy-queue, backlog,
  change-requests, pr-claims: no process spawn found on the path (store/memory reads). Not timed.
Please run `curl -w %{time_total}` against sg4 for git/pushed and deploy-ready to confirm.
