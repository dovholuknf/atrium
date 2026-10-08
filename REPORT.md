# f-room-spec-hub report

Commit b955db6f on claude/f-room-spec-hub.

## What changed
- hubstore: migration 0013 (room_spec, room_lock, appended last, idempotent), `internal/hubstore/roomspec.go`:
  SetRoomSpec, RoomSpecOf, PutRoomLock, RoomLockOf, CheckRoomSpec, CheckRoomLock. Checks: version 1, name equals room,
  64 KiB spec / 256 KiB lock, refuses keys named like token, password, secret, credential, key.
- link: new `roomspec` connection kind on the room link (`internal/link/roomspec.go`). One HTTP request: GET
  /_room/spec, POST /_room/lock, as the room the certificate names (hub overwrites the header, path names no room).
  Board read only: GET /_hub/rooms/<name>/spec and /lock (405 on writes). Wired in atrium_run.go via SetRoomSpecs.
- Go functions: `link.FetchRoomSpec(ctx, dialer, room)`, `link.PostRoomLock(ctx, dialer, room, lock)`, and for
  phase A `cli.PostRoomLock(ctx, dir, lock)` (dir "" is the default key dir).
- CLI: `rooms add --spec`, `rooms spec get|set <name> <file|->`, `rooms lock get <name>`,
  `room spec pull [--out file|-]` (default room.yaml beside the room's cert).
- Docs: changelog entry, docs/changes test plan, section in docs/fabric/hub-and-rooms.md.
- Updated the kind list sentence in TestANewHubsRefusalNamesGit (git_test.go) since the refusal now names roomspec.

## Tests
- New: hubstore roomspec_test.go, link roomspec_test.go (including room X cannot read or write room Y, spoofed header
  and query), cli atrium_rooms_spec_test.go. All pass.
- Full `go test ./...`: only failures are internal/ptyhost TestStartRunsADetachedHostFromACopy and
  TestHostCloseKillsItsRunners (unix socket path too long under this macOS temp dir, unrelated), and the link
  refusal-sentence test which I fixed and reran.

## Notes
- There are no AGENTS.md files in this repo, so the function signatures are documented in the Go doc comments and in
  docs/fabric/hub-and-rooms.md.
- The room-identity test uses the plain dialer. Certificate-name-wins is the hub's existing rule for every kind
  (`identify` in take), not re-tested over TLS here.
- A legacy overlay room with no certificate is refused for this kind by the existing provenName check.

## Review round (HOLD, 0 HIGH)
- M1: changerequest_test.go now expects 0013_room_spec last and 0012 at n-2, comment fixed.
- M2: board read documented as board-visible by design (hub-and-rooms.md, change doc, roomspec.go). Unknown room and
  room with no spec now answer the same 404 text (tested for spec and lock).
- M3: the roomspec kind is refused outright to any connection `identify` cannot read a certificate from (the old
  overlay path included), whether or not the claimed room ever enrolled. Test
  TestLegacyConnectionCannotNameARoomForItsSpec. Documented.
- L1: store errors are logged, the answer is a fixed sentence. L2: tripwire adds privatekey, auth, authorization, bearer,
  cookie, session, with a table test (nested lists, mixed case) and the doc calls it a tripwire. L3: PutRoomLock logs
  `lock-posted`. L4: `room spec pull` verifies the body against the sha256 header and writes 0600 via temp file and
  rename.
- `go test ./internal/link` at 552205e4 (second worktree): ok in 164s. On this branch: ok in 164s. The reviewer's 600s
  timeout in events_test.go and the gitsync CGI handler did NOT reproduce here, either side, so I cannot tie it to this
  change. It looks environmental (load or a stuck CGI process on the reviewer's machine).
- Tests run: `go test ./internal/hubstore ./internal/cli` ok, link package in full ok, plus my link tests.
