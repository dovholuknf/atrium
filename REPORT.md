# 75 report

## Done
- `scripts/provision-room.ps1`: the `-User` check is now `Test-Account` and the new localai check reuses it. A new machine
  (no manifest, no room.json) must be `localai`: missing prints the command and exits 11. Opt-out is explicit
  (`-KeepAccount`), and a machine that already is a room is detected and skipped (`account skip already provisioned
  under claude`), so sg3 is untouched. Both are available, detection is the default.
- `shared-folder` step: makes `C:\Users\Public\atrium`, `/Users/Shared/atrium` or `/srv/atrium`, or fails with the
  command and exit 12 when the login may not. `-NoSharedFolder` skips it. Clones go under it via new
  `room-git.ps1 init -GitRoot`.
- `-Check`: read only run of these steps, ends with `provision check ok`. Header, exit codes, docs/room-accounts.md,
  changelog and the item file updated.

## Left
- The room's `git_root` setting is not set: nothing in atrium writes it. Provisioning prints `git-root warn`. The
  question for clint is in docs/backlog/fabric/75.md (a Go verb, or sqlite3 on the remote).

## Verify
- Tested only with a fake ssh (scratchpad) and `-Check`: new without account (exit 11), login mismatch (exit 1), new
  with folder, folder missing, already provisioned, `-KeepAccount`. The exit 12 and real folder-making paths were not run.
