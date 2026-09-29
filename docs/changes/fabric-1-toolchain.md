## Changelog

- **A room's toolchain over ssh.** See `docs/backlog-2.md` item 46 and `scripts/room-toolchain.ps1`.

  - `scripts/room-toolchain.ps1` (new) installs go, node and, on Windows, a real Git for Windows and pwsh 7 into the
    remote user's home, over ssh, with no admin, no sudo and no package manager: `pwsh -File
    scripts\room-toolchain.ps1 <user@host>`. pwsh here, `sh -s` on macOS and Linux, Windows PowerShell 5.1 by a
    gzipped `-EncodedCommand` on Windows. One `room-toolchain <step> <ok|done|skip|warn|fail> <detail>` line per
    step, exit codes 0 to 5 in the script header.
  - A good-enough tool, looked for the way the room will see it, makes its step `ok` and nothing is installed. Good
    enough: go at least what `go.mod` needs, node the same major as `-NodeVersion` or newer, Git for Windows 2.39 or
    newer (a Cygwin or MSYS git does not count), pwsh the same major as `-PwshVersion` or newer. A good copy in the
    prefix but not on the room's PATH is adopted by recording its folder.
  - Otherwise the official archive is downloaded on the remote and its sha256 checked against the publisher's own
    list. A mismatch unpacks nothing and exits 4. Installs go under `-Prefix`, default
    `~/.local/share/atrium-tools/<tool>`. A directory already there that is not a working install is left alone
    unless `-Force`, and even then removed only after the new download is verified.
  - PATH is recorded in `~/.atrium/toolchain/path.txt`. On macOS and Linux one marked line in the login profile reads
    `~/.atrium/toolchain/path.sh`. On Windows the user and machine Path are never touched: the room is started
    through `~\.atrium\toolchain\room-env.ps1`, which `provision-room.ps1`'s start step now dot-sources when it
    exists.
  - `-Check` reports every step and writes nothing. `local` as the target runs on this machine. The script never
    restarts a room, and prints `restart warn` when a room is running after a change.
  - Not done: the `-Autostart` logon task still starts the room with the logon environment. When `room-env.ps1`
    exists at registration, its action should dot-source it first. The Linux systemd unit gets the tools when
    `atrium-service.sh install` runs after the toolchain.

## Test plan

## @LETTER@. The toolchain for a room

### @LETTER@1. Check a Windows room with a Cygwin git

On a Windows room whose machine Path has a Cygwin git, run `pwsh -File scripts\room-toolchain.ps1 <target> -Check`.

**Expected:** `git` is a `warn` naming the Cygwin git and what would be installed or recorded, go and node are `ok`
where found, nothing is written under the remote home, and the exit code is 0.

### @LETTER@2. A good copy the room does not see

On sg3, run `local -Check`.

**Expected:** go and node are `ok`, and git and pwsh are `warn` saying the good copy in `~\.local\share\atrium-tools`
is not seen by the room and its folder would be recorded.

### @LETTER@3. Install from nothing

On a machine with none of the tools, or with `-Prefix` in a temp folder, run without `-Check`.

**Expected:** each tool is `done` with its sha256 shown as matching, `path` is `done`, and each `<tool>.verify` is
`ok`. On Windows, a `powershell -ExecutionPolicy Bypass` shell that dot-sources `room-env.ps1` finds Git for Windows
first in `git --version`, and the user and machine Path are unchanged.

### @LETTER@4. Run it again

**Expected:** every step is `ok`, `path` is `ok`, and nothing is downloaded.

### @LETTER@5. A bad hash

Run with `-TestBadHash` for a tool that would install, then with `-Force` over a working install.

**Expected:** the step is `fail`, the exit code is 4, and the prefix holds no unpacked tool. With `-Force` the old
install is still there afterward.

### @LETTER@6. macOS and Linux

On m1mini run `-Check`. On a Linux machine with neither go nor node, run without `-Check`, twice.

**Expected:** on m1mini go and node are `ok` from the login shell's PATH. On Linux go and node are `done`, the
profile gets exactly one `# added by atrium room-toolchain` line and the second run adds no second one, and a fresh
`ssh <target> 'zsh -lc "go version; node --version"'` (or the bash equivalent) works.

### @LETTER@7. The systemd unit

On a Linux machine with a systemd user unit, run the toolchain and then `provision-room.ps1 -Autostart`, or
`atrium-service.sh install` again.

**Expected:** `systemctl --user show atrium -p Environment` carries the toolchain directories.

### @LETTER@8. A running room is not restarted

With a room running, run a change, then a run that changes nothing.

**Expected:** the change ends with a `restart warn` line and the room is not restarted. The run that changes nothing
has no such line.

### @LETTER@9. Provision starts a Windows room through room-env.ps1

On claudevm, never a real room, after the toolchain has written `room-env.ps1`, run `provision-room.ps1` without
`-Autostart`, then read the room's `git --version` from a card on it.

**Expected:** `start done`, and the card's git is Git for Windows from the toolchain, not a Cygwin git.
