# Handoff: sa63, backlog-2 item 63

Read BRIEF.md first, then this. Branch claude/start-card-room. Item 63's text is on claude/main
(`git show claude/main:docs/backlog-2.md`, line 1273). It is not in this branch's copy.

## Where things stand

- **Stage 1: done, accepted, deploying.** Commit e7d15a1, HUB-SIDE (`internal/link/cardroute.go`, `proxy.go`).
  saorch and atrium-87300 were both told. atrium-87300 widened it mid-way from "body task_id" to "every request that
  names a card, path or body, plain or tagged", which is what e7d15a1 does.
- **Stage 1 docs: done.** 330020c has the CHANGELOG entry (top of Unreleased) and `docs/test-plan.md` section CD.
- **Stage 2: design note written and reviewed. Build B1 to B4 written and committed (f0ba928), tested except the full
  Go suite on the last change.** Still owed are the last checks, a CHANGELOG line and test-plan steps for stage 2,
  and the final report.

## Stage 2 decisions and why

Design is in `docs/card-room-routing.md`. Rule: outside a room a card id is `room~id`, only the hub strips the tag
(on the hop into the room), and nothing that names a card is routed by a header.

- Root cause: `rooms.js` `window.fetch` put `writeRoom`, a stale global left by the last per-machine editor, on
  every write not matching `/v1/tasks/<id>/<verb>`. So `POST /v1/launch` and `PATCH /v1/tasks/<id>` got it.
- B1 (board, `rooms.js`): new `namesACard(url, init)`. A write naming a card (`/v1/tasks/<id>` or deeper, other than
  `prune`/`pin-order`, or a launch whose JSON body has `task_id`) never gets `writeRoom`.
- B2 (board): the three launch sites send `task_id` as held, tag and all (`card-menu.js` resumeStart,
  `fixtures.js` launchRunnerHereNow and the launch form). The hub strips it. The room's `launch.go` safety net also
  strips a tag.
- B3 (hub): `placeCard` sets `taggedKey` when the named id was tagged (path or launch body), so `retagCard` retags
  the launch's answer too. The old path-only `taggedKey` set in ServeHTTP was removed as redundant.
- B4 (ROOM-SIDE, needs a room restart): `api.NotOnRoom(id, room)` gives "card X is not on room R". It's used by
  `patchTask` (a 404 now, before any write) and by `launch.go` onto-card (`could not start onto it: card X is not on
  room R`). `api.Server.Room` is set from `opts.Room` in `daemon.go`.
- Stage 1 resolves only with 2+ rooms attached (single-room fake rooms in tests don't answer the probe, and one room
  is nothing to choose). `cardRoomTTL` was raised from 5s to 2 min, because every card-naming request now resolves.
- Deferred, as the note says: tag ids in scoped views too, and minting ids with their room.

## Mercurius

Session `s_Od6hnzbqLjbc` (working_dir this worktree). It is still open, with round 1 collected and notes not
recorded. Round 1 was needs_changes with one finding:
- C1 (major): B1's predicate must match `/v1/tasks/<id>` with no verb. This is FIXED in f0ba928 (`namesACard`).
- A1 (advisory): cover a launch with task_id under a stale writeRoom. FIXED: headless section `cardRoute`.
Next: `mercurius_record_round_notes` (C1 fixed, A1 fixed), optionally a round 2 on the code diff (mode `code`), then
`mercurius_close_session`. The `mercurius` CLI is not on PATH, so poll with `mercurius_collect_round`.

## Files touched

Stage 1: `internal/link/cardroute.go` (new), `internal/link/cardroute_test.go` (new), `internal/link/proxy.go`.
Stage 2: `docs/card-room-routing.md` (new), `internal/api/web/js/rooms.js`, `internal/api/web/js/card-menu.js`,
`internal/api/web/js/fixtures.js`, `internal/link/cardroute.go`, `internal/link/cardroute_test.go` (tagged-launch
retag test, test room echoes `saw_task_id` because the hub retags `task_id`), `internal/link/proxy.go`,
`internal/api/api.go`, `internal/api/notonroom_test.go` (new), `internal/daemon/daemon.go`,
`internal/daemon/launch.go`, `scripts/test-board-headless.js` (section `cardRoute`, in the main flow and in
`HEADLESS_ONLY`). Docs: `CHANGELOG.md`, `docs/test-plan.md`.

## Tests run on f0ba928

- `go test ./internal/link/ ./internal/api/`: ok.
- Headless full suite (`node scripts/test-board-headless.js`): passed. `HEADLESS_ONLY=cardRoute`: passed.
  Playwright was installed into this worktree with `npm install` and `npx playwright install chromium`.
- NOT yet run after B4: `go test ./internal/daemon/` and the full `go test ./...`. Clear ATRIUM_LOCATION and
  ATRIUM_DEBUG_INPUTLAG first. TestRealSessionsKeepTheirText is known noise. TestTypingDuringTheCountdownStartsTheWaitAgain
  in `internal/link` failed once and then passed on rerun, so it's flaky and unrelated.

## Exact next steps

1. `go build -o build.claude/ ./...`, then `go test ./...` with the two env vars cleared. Fix anything that breaks,
   likely a daemon test that expects the old "no card X to start onto" wording.
2. Add a stage 2 CHANGELOG bullet under Unreleased: the board no longer sends `writeRoom` on card writes, launches
   send tagged ids, the launch answer is retagged (all HUB RESTART, since the hub serves the board), and the room's
   not-found names card and room (ROOM RESTART). Add test-plan steps to section CD (for example CD5 the retagged
   launch answer, CD6 the room-side 404 wording against a room reached directly on :7781 style port). Wrap at 120,
   with no em-dashes, double hyphens or semicolons in prose.
3. Mercurius: record round 1 notes, optionally a code round, then close the session.
4. Commit (one-line subject). Then send ONE `atrium_report` status done with the final sha. Say it is HUB-SIDE for
   B1 to B3 and ROOM-SIDE for B4 (can wait for the next room deploy), and give the tests.
