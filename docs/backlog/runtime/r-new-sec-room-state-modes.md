# r-new-sec-room-state-modes: the room's state directory and atrium.db are readable by every local user

Status: held (pause, 2026-10-01). @runtime. Medium on a multi-user Linux host, Low on macOS. Source: docs/backlog/review/review-new-security-audit-kimi.md (the Kimi audit, checked by @review on 2026-10-01).

## Why

internal/daemon/daemon.go:319 creates the DB directory 0755, and SQLite creates atrium.db, -wal and -shm 0644. On
m1mini: `drwxr-xr-x ~/.atrium` and `-rw-r--r-- ~/.atrium/atrium.db`. That file holds the session cookie HMAC key
(anyone who reads it mints a session on the published board), the OIDC client secret and every card's launch_env.
The hub made its directory 0700 for this reason (internal/hubstore/store.go:120-127). The same pattern, lower
stakes: the cold event sink (internal/store/filesink.go:69,175), `ATRIUM_TAP_DIR`
(internal/daemon/supervisor.go:2067-2070), and the PR runner's run.log and review.json
(internal/daemon/prrunner.go:439,461,474).

## Wanted

- 0700 for the room state directory, with an existing one chmodded at start, and 0600 for atrium.db, -wal and
  -shm, matching the hub.
- 0600 and 0700 for the event sink, the tap and the PR runner files.
- A test that a fresh room's DB and directory are owner-only (skipped on Windows).
