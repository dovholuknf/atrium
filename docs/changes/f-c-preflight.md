## Test plan

## @LETTER@. room-toolchain -Profile c checks the MSYS2 target and the git identity first

### @LETTER@1. An unreadable drive is needs-human, in -Check and in a run

On a room where `V:\` is not readable by the room account, run
`pwsh -File scripts\room-toolchain.ps1 <room> -Profile c -Msys2Dir V:\work\msys64 -Check`. The `msys2` step is
`needs-human` with the `icacls V:\ /grant ...:(OI)(CI)RX` line, never "would install", and the exit code is 6. The same
without `-Check` installs nothing and exits 6.

### @LETTER@2. A read-only folder and a full drive say so

Point `-Msys2Dir` below a folder the account cannot write: the step names that folder and prints the `icacls` Modify line.
Point it at a drive with under 6 GB free: the step says the free space and the 6 GB, and prints `Get-PSDrive`.

### @LETTER@3. An explicit git identity is honoured

On a room whose global git config has another email, run with `-GitUserEmail new@example.com -Check`: the step says
would set it and shows the current value. Without `-Check` it is set and `git config --global user.email` prints the new
value. Without `-GitUserEmail` the config is left alone. Run `pwsh -NoProfile -File scripts/test-room-toolchain-c.ps1`
and `pwsh -NoProfile -File scripts/check-powershell.ps1`.
