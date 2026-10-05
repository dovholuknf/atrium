# 75 report

## Done
- Rebased on claude/main.
- `scripts/provision-room.ps1`: the `-User` check is now `Test-Account` and the new localai check reuses it. A new machine
  (no manifest, no room.json) must be `localai`: missing prints the command and exits 11. Opt-out is explicit
  (`-KeepAccount`), and a machine that already is a room is detected and skipped, so sg3 is untouched.
- `shared-folder` step: makes `C:\Users\Public\atrium`, `/Users/Shared/atrium` or `/srv/atrium`, or fails with the
  command and exit 12. `-NoSharedFolder` skips it. Clones go under it via `room-git.ps1 init -GitRoot`.
- `-Check`: read only run of these steps, ends with `provision check ok`.
- git_root: decided as a Go verb (written in docs/backlog/fabric/75.md). `atrium room set git_root <path>` and
  `atrium room get git_root`. No existing verb wrote settings and the room has no settings API route, so the verb works on
  the stopped room's database file through `store.SetSetting`, and refuses a running room with `stop it first` (the same
  exclusive-lock probe `store.Compact` uses). `set` makes the database when none exists, so a new machine can be set before
  its first start. Requires an absolute path. Files: `internal/cli/roomsettings.go`, `internal/store/settingfile.go`.
- Provisioning calls `room set git_root <shared folder>` over ssh after the join, before any start, for a new machine. A
  refusal is a `git-root warn` with the command. Under `-Check` the line says what a real run will do.

## Verify
- `go test ./internal/cli ./internal/store -run "RoomSet|Compact"` passes (set and get, creates the db, refuses a held
  store, unknown key, relative path, missing db on get).
- Fake ssh (canned answers, never runs the script) with `-Check`: ssh, account, shared-folder, `git-root ok` and
  `check ok`. Earlier runs: new without account (exit 11), login mismatch, folder missing, already provisioned,
  `-KeepAccount`.
- NOT run: the post-join `room set` call itself (a real run needs a hub join, which the fake ssh cannot stand in for). The
  command it sends was rendered and the script parses. The exit 12 and real folder-making paths were not run either.
- Did not run the whole test suite, only the packages touched.
