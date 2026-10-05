# r-new-sec-room-state-modes

Nothing was already met: every point was still at 0755/0644 on bcdbb267.

Changed:
- daemon.go: state dir made 0700 and an existing one chmodded at start.
- store.go: a fresh atrium.db is created 0600 before SQLite opens it, and db, -wal, -shm are chmodded 0600 after open (covers existing files).
- filesink.go: dir 0700, segments 0600.
- supervisor.go: tap dir 0700, tap file 0600.
- prrunner.go: run.log, review.json and written files 0600, folders 0700. prs.go run folder subdirs 0700.

Test: TestFreshRoomStateIsOwnerOnly (internal/daemon/daemon_modes_test.go, skipped on Windows). Passes.
store package tests pass. Windows and linux cross-builds pass.
Full daemon package run has failures from this machine (unix socket path too long for hostterm tests, claude.cmd path in testguard), not from this change. I did not run a baseline to confirm.
Left: nothing.
