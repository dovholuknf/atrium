# Fabric 1: the toolchain for a room

## Changelog

- `scripts/room-toolchain.ps1` (new) installs go, node and, on Windows, a real Git for Windows and pwsh 7 into the
  remote user's home, over ssh, with no admin, no sudo and no package manager. `pwsh -File scripts\room-toolchain.ps1
  <user@host>`. It has the shape of `room-git.ps1` and `provision-room.ps1`: pwsh here, `sh -s` on macOS and Linux,
  Windows PowerShell 5.1 by `-EncodedCommand` on Windows (gzipped inside, so the payload stays under the cmd.exe
  limit), and one `room-toolchain <step> <ok|done|skip|warn|fail> <detail>` line per step, ending `room-toolchain done
  ok` or `done fail <code>`. Exit codes 0 to 5 are in the script header.
- Each tool is looked for the way the room will see it. A good-enough one makes the step `ok` and nothing is
  installed. Good enough: go at least what `go.mod` needs, node the same major as `-NodeVersion` or newer, Git for
  Windows 2.39 or newer (a Cygwin or MSYS git does not count), pwsh the same major as `-PwshVersion` or newer. A good
  copy that sits in the prefix but is not on the room's PATH is adopted: its folder is recorded, nothing is unpacked.
- Otherwise the official archive is downloaded on the remote and its sha256 is checked against the publisher's own
  list (go.dev `?mode=json`, nodejs.org `SHASUMS256.txt`, the GitHub release digest for PortableGit and pwsh). A
  mismatch unpacks nothing and exits 4. The code is read from an `rc=` line the payload prints, because the exit code
  that arrives over ssh from a Windows remote is not always the one the script exited with. Installs go under `-Prefix`, default `~/.local/share/atrium-tools/<tool>`.
  Versions are `-GoVersion` (default what `go.mod` says), `-NodeVersion v24.21.0`, `-GitVersion 2.56.0` and
  `-PwshVersion 7.6.6`. A directory already at the destination that is not a working install is left alone unless
  `-Force`, and even then it is removed only after the new download has been verified.
- PATH is recorded in `~/.atrium/toolchain/path.txt`. On macOS and Linux one marked line in the login profile
  (`.zprofile` for zsh, `.bash_profile` or `.profile` for bash) reads `~/.atrium/toolchain/path.sh`. On Windows the user
  and machine Path are never touched, because the machine Path comes first and a Cygwin git on it breaks tests. The
  room has to be started with the record in front, through `~\.atrium\toolchain\room-env.ps1`. See "How the room
  start reads it" below.
- `-Check` reports every step and writes nothing. `local` as the target runs the same payloads on this machine
  instead of over ssh. The script never restarts a room. When a room answers on 7781 after a change it prints a
  `restart warn` line.

## How the room start reads it

**macOS and Linux.** The room is started through a login shell (`provision-room.ps1` runs `$SHELL -lc 'exec atrium room
--detach'`), so the profile line is all it needs. Restart the room and it has the tools.

**Linux systemd user unit.** A user unit reads no profile. `scripts/atrium-service.sh` writes the login shell's PATH
into the unit when it INSTALLS it, and that is where the two meet. So run `room-toolchain.ps1` first and then
`provision-room.ps1 -Autostart`, and the unit's PATH holds the tools. If the unit was made earlier, run
`scripts/atrium-service.sh install` on the remote again (it is idempotent) and restart the unit. `provision-room.ps1`
says `autostart ok` for a unit that exists and does not rewrite it.

**Windows.** Two starts, and both need one small change. Neither is made in this branch, and the atrium binary does not
change.

1. `provision-room.ps1`'s start step (the Windows `$ds` in section 8) is `& $Bin room --detach`. It should dot-source
   the file first when it exists:

   ```powershell
   $ds = "`$ErrorActionPreference = 'Continue'`n`$e = Join-Path `$HOME '.atrium\toolchain\room-env.ps1'; if (Test-Path `$e) { . `$e }`n& `$Bin room --detach 2>&1`nexit `$LASTEXITCODE"
   ```

   `room-env.ps1` prepends every line of `path.txt` to `$env:Path`, and `atrium room --detach` passes its environment on
   to the room and to every runner the room starts. That is the whole change, and it is how sg3 was restarted by hand.

2. `atrium-autostart.ps1` registers a task whose action is `conhost.exe --headless <exe> room ...`, which starts the
   room with the logon environment, machine Path first. When `~\.atrium\toolchain\room-env.ps1` exists at
   registration, the action should be `conhost.exe --headless powershell.exe -NoProfile -NonInteractive
   -ExecutionPolicy Bypass -EncodedCommand <base64 of ". '<room-env.ps1>'; & '<exe>' <room args>; exit $LASTEXITCODE">`.
   `-EncodedCommand` avoids the quoting a task's argument string would need. It is not made here because fb01
   rewrote that registration (by XML) and the change belongs on top of it, and because a logon task cannot be
   registered or run on sg3 without touching a real room. Re-register after installing the toolchain: the file is read
   at start, but the action is fixed at registration.

Dot-sourcing `room-env.ps1` by hand needs `powershell -ExecutionPolicy Bypass`, or the new pwsh, because a plain Windows
PowerShell on a machine with a Restricted policy refuses to run a script file ("running scripts is disabled"). The room
starts are not affected: provision and the autostart action both run with `-ExecutionPolicy Bypass`, and with Bypass
a hand check takes the same path the room start does.

A room that is already running keeps the PATH it started with.

## Test plan

### FC. The toolchain for a room

- FC1. On a Windows room whose machine Path has a Cygwin git, run `pwsh -File scripts\room-toolchain.ps1 <target>
  -Check`. `git` is a `warn` that names the Cygwin git and what would be installed or recorded, go and node are `ok`
  where they are found, and nothing is written under the remote home. The exit code is 0.
- FC2. On sg3 with `local -Check`: go and node are `ok`, git and pwsh are `warn` saying the good copy in
  `~\.local\share\atrium-tools` is not seen by the room and its folder would be recorded.
- FC3. On a machine with none of them, or with `-Prefix` in a temp folder, run without `-Check`. Each tool is `done`
  with its sha256 shown as matching, `path` is `done`, and each `<tool>.verify` is `ok`. On Windows a shell started with
  `powershell -ExecutionPolicy Bypass` (a Restricted policy refuses the file otherwise) that dot-sources `room-env.ps1` finds Git for Windows first in `git --version`, and the user and machine Path values are
  unchanged.
- FC4. Run the same command again. Every step is `ok`, `path` is `ok`, nothing is downloaded.
- FC5. Run with `-TestBadHash` for a tool that would install. The step is `fail`, the exit code is 4, and the prefix
  holds no unpacked tool. With `-Force` over a working install, the old install is still there afterward.
- FC6. On macOS arm64 (m1mini) run `-Check`. go and node are `ok` from the login shell's PATH. On a Linux machine with
  neither, run without `-Check`: go and node are `done`, the profile gets exactly one `# added by atrium
  room-toolchain` line, and a second run adds no second line. A fresh `ssh <target> 'zsh -lc "go version; node
  --version"'` (or the bash equivalent) works.
- FC7. On a Linux machine with a systemd user unit, run the toolchain and then `provision-room.ps1 -Autostart`, or
  `atrium-service.sh install` again. `systemctl --user show atrium -p Environment` carries the toolchain directories.
- FC8. With a room running, a change ends with a `restart warn` line and the room is not restarted. A run that
  changes nothing has no such line.
