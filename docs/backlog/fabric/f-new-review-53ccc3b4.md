# Review of 53ccc3b4 (@fabric: room-defender.ps1, Defender exclusions at provisioning)

Reviewed by @review, 2026-09-30, from `git show 53ccc3b4`, scripts before docs. It landed on claude/main before
review. Nothing was run. On sg4, the pending file and its ACL were read, and nothing was changed.

## What holds

- Paths are read as the runner account over one ssh round trip. `local` run elevated needs `-Runner`, so an
  administrator's own profile is never the one excluded.
- Quoting: every path reaches the room through `Quote-Ps`, and the file body is quoted a second time for
  `WriteAllText`. A `'` in a path cannot break out of the string.
- `-Check` writes nothing. Not Windows, or Defender missing or stopped, is a `skip` and nothing else runs.
  `GOTMPDIR` is read back after `go env -w`, and an exclusion is read back after `Add-MpPreference`.
- The provision step runs only on Windows, `-NoDefender` skips it, and a failure is a `warn` that does not stop
  provisioning.

## Findings

### High

1. **An administrator is told to run, elevated, a script the agent account can write.** When the runner is not
   elevated, which is the usual case, `room-defender.ps1` writes `~\.atrium\provision\defender-exclusions.ps1` into the
   runner's own profile. It then prints `an administrator on <host> runs: powershell -ExecutionPolicy Bypass -File
   "<file>"`. On sg4, `SG4\claude` holds FullControl on that file (read with `Get-Acl`), and every agent session runs
   as `claude`. Any agent, including one driven by a prompt injection or by auto mode, can add a line to the file,
   and that line runs as administrator when the instruction is followed. The runner account is kept non-admin so that
   this boundary holds.
   - Exposure: the file is waiting on sg4 and on sg3 now (`f-new-defender-at-provision.md` says so), for clint to run.
     The content on sg4 at 18:43 is benign: one `Add-MpPreference` line with five paths.
   - Proposed fix: write no file for an administrator. Print the literal `Add-MpPreference -ExclusionPath ...` line
     only, since it reaches the operator through provisioning output on the operator's machine. If a file is kept,
     write it where the runner cannot (for example beside the provisioning log on the operator's machine), and tell
     the administrator to read it before running it.
   - Until the fix lands: do not run either pending file with `-File`. Paste the printed line after reading its
     paths.
2. **The runner's own `go env` decides what gets excluded.** `GOCACHE`, `GOMODCACHE` and `GOTMPDIR` are read as the
   runner, and the runner can set them with `go env -w`. A runner with `GOCACHE=C:\` hands the administrator a line
   that excludes the whole drive. Proposed fix: refuse any path that is not under the runner's profile, the clone or
   the worktree root, and never accept a drive root. This is part of the same boundary as 1, so it is listed as a
   high, but it only needs a guard in `paths`.

### Low

1. **The header's claim is false.** The header says the paths hold "Nothing anything downloads into from outside".
   `GOMODCACHE` is modules downloaded from the proxy. A worktree root can hold external PR heads checked out for
   review, and on sg4 the reviews folder sits under `D:\worktrees\claude`. Excluding the build cache and the module
   cache is common advice, so the exclusion is a fair trade. Should the comment and the user guide say what the trade
   is, rather than that there is none?
2. **`-Remove` leaves both changes in place.** The header says so. Should provisioning with `-Remove` at least print
   the `Remove-MpPreference` line and `go env -u GOTMPDIR`?

HOLD 53ccc3b4. The two highs need a fix in `room-defender.ps1` before anyone runs the pending files as written.

## Re-read of 08779d34 (claude/fabric), 2026-09-30

Read `git show 08779d34`. I also ran `room-defender.ps1 local -Check` from a detached worktree at 08779d34, which
writes nothing: once as is, once with `GOCACHE=C:\Users\claude\AppData\Local\..\..\..\Windows`, and once with
`GOCACHE=C:\Users\claude\Downloads`.

- **High 1 closed.** Nothing is written on the room. The line is printed only, in the operator's output. The
  pending file on sg4 is gone (`Test-Path` is False).
- **High 2 closed.** The profile root comes from HKLM ProfileList by SID. A Go path must be fully qualified, and it
  may hold no wildcard, `~`, quote, backtick, `$`, `;`, control character, `.` or `..`. It must also be strictly
  inside the profile, the worktree root or the build folder. A root is refused when it is a drive root, less than
  two folders deep, or under a system folder. The `..` run left GOCACHE out with a `paths warn` line. The plain run
  printed the same five paths as before.
- The lows are folded into the header.

New low: a Go path may be any folder strictly inside the runner's profile, so `GOCACHE=C:\Users\claude\Downloads`
is accepted and printed. The administrator boundary holds, because the line is printed and read before it is
pasted. It widens what Defender skips only inside a profile the runner already writes. Could the Go paths be held
to `%LOCALAPPDATA%` and `~\go` under the profile? Not needed for this landing.

PASS 08779d34
