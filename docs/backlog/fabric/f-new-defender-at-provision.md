# f-new-defender-at-provision: Defender exclusions as part of install and room bring-up

Status: built, `scripts/room-defender.ps1` plus a `defender` step in `provision-room.ps1`. sg3 has GOTMPDIR set, and
its exclusions wait on an administrator pasting the line `room-defender.ps1 sg3` prints (the ssh login `claude` is
not an administrator). The elevated path is untested: no administrator login is reachable from here.

@review's HIGH on 53ccc3b4 (`f-new-review-53ccc3b4.md`) is fixed: it wrote a script in the runner's profile for an
administrator to run, which any agent could append to. Now nothing is written, the line is only printed, and every
path is checked against the runner's profile (from HKLM by SID), the worktree root and the build folder, so a
`GOCACHE=C:\` excludes nothing. The two files it had written, on sg3 and sg4, are deleted. (clint: "should prolly be part of the installation process or maybe
part of room bring up. sg3 prolly needs something similar").

"The local install script" is `room-defender.ps1 local`. `atrium-service.ps1 install` registers the logon task and
is not changed.

## Why

`docs/user-guide.md` Pattern 13 is a manual step, and clint's first run of it excluded his own profile instead of
the agents', because an admin shell expands `$env:LOCALAPPDATA` for whoever opened it. A room that needs a hand
fix is the thing `adding rooms must be easy` rules out: every per-room fix goes into provision.

## Wanted

- `provision-room` (and the local install script) has a Defender step on Windows rooms. It resolves the paths AS
  THE ROOM'S RUNNER USER (`go env GOCACHE GOMODCACHE GOTMPDIR`, the worktree root, the build directory), then
  applies them in the elevated part of provisioning, or prints the exact command when provisioning is not elevated.
- Opt out with a flag, and say in the provision output which paths were excluded.
- Set `GOTMPDIR` for the runner user to a directory inside an excluded path. `go test` links its test binaries
  under `%TEMP%\go-build*` by default, which is not excluded, and excluding all of `%TEMP%` would be too broad.
- Apply it to sg3 now that it exists (Windows, provisioned before this). m1mini is macOS: nothing to do.
- r-new-defender-advice (runtime) is the detection side and points at this for the fix.

## Evidence

sg4, 2026-09-30 17:40: after the path and process exclusions, `MsMpEng` still sampled 24% to 177% of one core over
10 s while tests ran. The test binaries in `%TEMP%` are the likely remainder.
