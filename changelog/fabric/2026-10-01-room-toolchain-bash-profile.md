`room-toolchain.ps1` now writes a block into `~/.bash_profile` on a Windows room. A session's Bash tool is a login bash,
and on sg3 that is Cygwin's. Its profile put Cygwin's git (2.38, which cannot read a `C:/` worktree path) ahead of the
git the room was started with, left pwsh off the PATH, and pointed TMP at `V:\work\tools\cygwin\tmp`. From a session
there, `git worktree add` failed, so 8 `TestCull*` tests failed, and the daemon's prepare command fell back to Windows
PowerShell 5.1, which has no `ConvertTo-Json -AsArray`, so 3 `TestCaptureEnv*` tests failed. All 11 passed from a
PowerShell session on the same machine and on sg4. The block puts the toolchain record first, and sets TMP and TEMP to
the user's own Temp. It is replaced in place when the script changes it, and `-Check` warns when it is missing. Applied
to sg3, where the daemon tests named above now pass through a Cygwin login bash.
A login bash reads only the first of ~/.bash_profile, ~/.bash_login and ~/.profile, so the block goes in the first of
them that exists, and a new ~/.bash_profile is made only when none does.
