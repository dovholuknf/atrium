- New `scripts/room-defender.ps1 <room|local>` reads a Windows room's Go caches, build folder and worktree root as the
  account the agents run as, and sets `GOTMPDIR` inside the Go cache so test binaries stop landing in `%TEMP%`. It
  excludes those paths from Defender when elevated, and otherwise writes the exact command for an administrator.
  `provision-room.ps1` runs it on every Windows room (`-NoDefender` skips it). Applied to sg3. (f-new-defender-at-provision)
