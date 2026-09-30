## Test plan

## @LETTER@. Defender exclusions are worked out as the agents' account

Needs a Windows room reached over ssh as a non-admin account (sg3), and this machine.

### @LETTER@1. A dry run changes nothing

1. `pwsh -File scripts\room-defender.ps1 sg3 -Check`

**Expected:** the `paths` line lists that account's `go-build`, `go\pkg\mod`, `go-build\tmp`, the clone's
`build.claude` and `<clone>-worktrees`, all under that account's profile. `gotmpdir` and `exclude` say `todo` or `ok`,
and nothing on sg3 changes.

### @LETTER@2. Not elevated, it hands over the command

1. `pwsh -File scripts\room-defender.ps1 sg3`, then run it again.

**Expected:** the first run sets `GOTMPDIR` (`go env GOTMPDIR` on sg3 names `...\go-build\tmp`) and prints an
`exclude warn` line with the `Add-MpPreference` line under it. Nothing is written under `~\.atrium` on sg3. `go test`
still passes on sg3. The second run says `gotmpdir ok ... already set`.

### @LETTER@3. An administrator applies it

1. On sg3, in an elevated shell, paste the printed line.
2. `Get-MpPreference | Select-Object -ExpandProperty ExclusionPath` in the same shell.

**Expected:** the five paths are listed, and they are the agents' account's, not the administrator's.

### @LETTER@4. A runner cannot widen the list

1. In a shell as the agents' account, set `$env:GOCACHE = 'C:\'`, `$env:GOMODCACHE = 'C:\Windows\Temp\m'` and
   `$env:GOTMPDIR = "C:\Users\<account>\a'; calc; '"`, then `pwsh -File scripts\room-defender.ps1 local -Check
   -WorktreeRoot 'D:\,C:\Program Files\x\y'`.

**Expected:** one `paths warn left out ...` line for each of the five, and the printed line holds only the build
folder and new-worktree.ps1's root.

### @LETTER@5. Elevated and local needs the account named

1. In an elevated shell, `pwsh -File scripts\room-defender.ps1 local`.

**Expected:** `who fail`, saying the paths would be this shell's account's, and nothing changes. With
`-Runner <that account>` it applies the exclusions and says `exclude done`.

### @LETTER@6. Provisioning runs it

1. Provision a Windows room, then provision it again with `-NoDefender`.
2. Provision a macOS or Linux room.

**Expected:** the first Windows run prints the `room-defender` lines after the `room-git` ones, and the `-NoDefender`
run prints `provision defender skip -NoDefender` instead. The macOS or Linux run has no `defender` line.
