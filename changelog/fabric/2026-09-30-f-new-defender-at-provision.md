- New `scripts/room-defender.ps1 <room|local>` reads a Windows room's Go caches, build folder and worktree root as the
  account the agents run as, and sets `GOTMPDIR` inside the Go cache so test binaries stop landing in `%TEMP%`. It
  excludes those paths from Defender when elevated, and otherwise prints the one line an administrator pastes. Every
  path must lie inside that account's profile, the worktree root or the build folder, or it is left out.
  `provision-room.ps1` runs it on every Windows room (`-NoDefender` skips it). (f-new-defender-at-provision)
