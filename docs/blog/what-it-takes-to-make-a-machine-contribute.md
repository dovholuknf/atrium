# What it takes to make a machine contribute

Series: Rooms, the hub and federation. Status: idea. Audience: people provisioning dev machines for agents.

**Hook.** Antivirus at 103%, a byte-order mark that failed provisioning with exit 6, and Cygwin git first on the path.

**Angle.** A new room fails in small, local ways, and each one needs a check, not a memory.

**Rests on:** provision-room, room requirements, room-defender. See `docs/blog/inventory.md`.

## Story beats

1. Provisioning over ssh: install, join, hooks, gate, autostart, a smoke per runner.
2. A BOM in a profile broke the first line of a piped script.
3. Cygwin git and old PowerShell first on the login path; tests failing only from a session.
4. Defender at 103% while two directors built; exclusions, and printing the admin line instead of writing a script.
5. The requirements file and `room-check`.

## Screenshots and demos

- the provision run
- Task Manager at 103%

## Sources

- changelog/fabric/2026-09-29-provision-bom.md
- changelog/fabric/2026-10-01-room-toolchain-bash-profile.md
- docs/user-guide.md pattern 13
- atrium.requirements.yaml

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
