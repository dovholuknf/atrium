# r-new-mingit-no-http-backend

## Choice for point 1
Provision installs PortableGit instead of MinGit. Serving upload-pack without http-backend would mean reimplementing the smart HTTP protocol (and receive-pack's guards sit on top of http-backend too), and nothing in the code did that already. The install path (`scripts/room-mingit.ps1`, `room-git.ps1`) already existed, so the change is the asset (`PortableGit-<ver>-<arch>.7z.exe`, a 7z self-extractor run with `-y -o<dir>`), same SHA256 check, same `~\.local\git` and user Path. The install script refuses an archive with no `git-http-backend.exe`. Function names keep their MinGit prefix to limit churn.

## What changed
- `scripts/room-mingit.ps1`: PortableGit asset and extractor.
- `scripts/room-git.ps1`: `init` replaces a Windows git that has no http-backend (installs PortableGit over `~\.local\git`). `init -Check` (which room-check runs for the clone row) now fails such a git with the fix.
- `internal/gitsync/backend.go`: `backendMissing` and `serveCGI`. All four git CGI call sites answer 500 with a sentence naming the missing http-backend, instead of `cgi: no headers`.
- `internal/gitsync/hub.go`: when a collect fetch fails on a 500, the hub asks the room's git route once more and appends the room's sentence to the collect error (`whyRoomRefused`). Best effort, silent if the probe fails.
- Tests: `TestBackendMissingNamesTheCause`, three checks in `scripts/test-room-start.ps1` (61 pass, asset names updated). No migration.
- Changelog added.

## Tests
`go test ./internal/gitsync` with ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG cleared: TestGitCappedStopsGitAtTheCap (no signing key on this machine) and TestPushToHubRefuses... (known) fail. TestAChainNamingAnotherRoomsCardDoesNotHelp failed once and passed on rerun. Not run: a real PortableGit install, the hub-side probe against a real room, anything on sgg.

## What sgg needs by hand
Nothing was done on sgg. To recover once this is deployed: run `room-git.ps1 init <room>` (or room-check with -Fix) against sgg, which replaces `%USERPROFILE%\.local\git` with PortableGit. Or by hand, download PortableGit from git-for-windows releases, run it with `-y -o"%USERPROFILE%\.local\git"` and keep `.local\git\cmd` first on the user Path. Then restart the room on sgg so it sees the new git. The room binary needs this branch's build for the clearer errors.
