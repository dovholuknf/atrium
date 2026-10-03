# Review: r-worker-tags 73dd8df4

Range `d739825c..73dd8df4`, one commit, 10 files, room and hub.

- Both `atrium_launch` paths add the launcher's `dept:*` tag to a worker, through `link.WithLauncherDept`: the hub's
  `launchOnRoom` (the relayed launch takes it too) and the room's stdio `launchHandler`.
- A card row gains `launcher_id`, computed by `store.LauncherID`. It is on `/v1/tasks`, `/v1/tasks/{id}`, the SSE
  `task` event and the PATCH answer.
- A daemon test pins why auto_new_context `agents` mode skips workers.

Verdict: **OK** for room and hub. Two Mediums and four Lows can follow. M2 should be fixed before the board starts
reading `launcher_id`.

## How it was checked

- I read the whole diff. Beside it I read:
  - `launcherOf` and `currentLauncher`, `localTarget`, and `Qualify` with `SetLineage` (spawned_by is written once and
    stored qualified, and a `name@room` launcher is kept as it is);
  - `usageGroups`, the only reader of a `dept:` tag;
  - `matchCard`, and the hub's `agentOf` and `resolvePeer` flow;
  - the hub fan-out's id tagging, and `withSeen` and `withAskCounts`.
- gofmt is clean on the changed files. `go build ./...` and `go vet` (store, link, cli, api, daemon) pass at the tip.
- Tests at the tip:
  - store and cli pass.
  - link passes on its own (143s). Run beside the other four packages it hit the 10-minute default timeout, so that
    was load, not a hang.
  - api fails only on TestTheWalkerLaunchSetAndClear, which is known.
  - daemon fails only on known reds: the hostterm and ptyhost reattach and run tests, keepalive-fork, and
    TestNoTestHereCanReachALiveRoom.
- Onto current `claude/landing` (f0d3a1a4): `git merge-tree` conflicts only in `docs/test-plan.md`, where both sides
  append a section. The Go diff applied onto landing builds, and the new and related tests pass there.
- I checked `LauncherID` with throwaway store tests, since deleted. Every case returned what `launcherOf` would:
  - `report_to` as an alias, a handle and a card id;
  - a stale `report_to`, which falls back to spawned_by_id;
  - a `report_to` that names the card itself;
  - `report_to` set to `boss@other`;
  - a cross-room launch with a local `report_to`, which gives the local card;
  - an id-only lineage, which is empty on both sides.
- Cost: `GET /v1/tasks` with 301 cards, all with lineage and `report_to`, best of 5: **36.9ms with the `withSeen` loop,
  5.3ms without it.**
- Mutants, run on the merged tree, with backups in my own temp dir:

| # | Mutant | Result |
|---|--------|--------|
| 1 | hub path drops `WithLauncherDept` | killed |
| 2 | stdio path drops `WithLauncherDept` | killed |
| 3 | caller's own `dept:` no longer wins (`hasTagPrefix` forced false) | killed |
| 4 | `LauncherID` skips the `report_to` step | **survives** (api and the full store package) |
| 5 | `LauncherID` drops all three self checks | **survives** |
| 6 | `LauncherID` drops the `@` guard | survives, equivalent (a `name@room` never matches a local wire name) |
| 7 | `withSeen` loop removed | killed |
| 8 | `taskEvent` line removed | killed |
| 9 | `patchTask` line removed | killed |
| 10 | hub `launcherTags` keeps the `@room` suffix | **survives** |
| 11 | last `dept:*` instead of first | **survives** (no test launcher has two) |
| 12 | `omitempty` removed | killed |
| 13 | auto-context `agents` mode no longer skips `atrium:subagent` | killed |

## Their points

1. **WithLauncherDept.**
   - **"Already carry one" means the requested tags.** That is the caller's `in.Tags` after `AgentLaunchTags`, as
     found in the r-stdio-launch-lineage review. So a caller can always choose its dept, and inheriting is only the
     default. Fine, because a dept grants nothing:
     - its only reader is `usageGroups`, which files spend under it;
     - the launch cap counts `atrium:subagent`;
     - no permission or routing reads `dept:`.
   - **Finding the launcher on the hub.**
     - `callerRoom` comes from the connection, and `agentOf` is the caller's self-asserted header.
     - So a card can name another card on its own room and inherit that card's dept. That gives nothing beyond writing
       `dept:x` itself.
     - It cannot reach another room's cards: the lookup is on `callerRoom`.
   - **"First `dept:*"`** follows the stored tag order, so it is deterministic. Mutant 11 shows the order is not
     pinned.
   - **A failed lookup** stamps nothing and never fails the launch. That is right.
2. **launcher_id.**
   - **It matches `launcherOf`** in every case above:
     - `localCard` is `localTarget` (handle, alias, id, all exact);
     - `GetByWireName` qualifies, so leaving out `Qualify` loses nothing;
     - spawned_by_id is a bare local id or `room~id`, and `Get` misses the second.
   - **The one difference:** a card whose spawned_by_id is its own id. `launcherOf` returns the card itself, and
     `LauncherID` skips it and goes on to spawned_by. `LauncherID` is the better answer. No real launch can produce
     this, since a card cannot launch itself.
   - **Read-only.** It never writes the `SetLauncher` correction that `currentLauncher` makes, which is right for a
     GET.
   - **omitempty** is right, and pinned (mutant 12).
   - **The exact-JSON test is honest about what it checks:** it decodes the real handler output, it requires the key to
     be absent, not empty, and it covers the list, the single GET and the event. It does not cover `report_to` (M1).
   - Cost: M2.
3. **auto_new_context `agents` mode not reaching workers. Agreed.**
   - `autocontext.go:206` requires `origin:agent` and no `atrium:subagent`, so workers are excluded on purpose.
     `atrium:auto-new-context` is how one opts in.
   - Their test pins it (mutant 13).

## Interactions

- **r-stdio-launch-lineage (632db27e).** `WithLauncherDept` wraps `AgentLaunchTags` and adds the tag after it, so the
  origin and worker markers are untouched.
  - The stdio path now reads `ATRIUM_AGENT_NAME` once, for both the tags and `spawned_by`.
  - The daemon sets that variable to the card's stored `WireName`, so `myTags`'s exact `t.Wire == me` matches a daemon
    launch (L3 covers a hand-set name).
- **r-owed-answers (8d1d31ed, not landed).**
  - It reads `launcherOf`, and `LauncherID` agrees with it, so the board and the owed-answer notices name the same
    launcher.
  - `git merge-tree 8d1d31ed 73dd8df4` conflicts in `docs/test-plan.md` and in
    `internal/cli/control_launch_lineage_test.go`. Both are appends at the end of the file, so whichever lands second
    keeps both sides.
  - For that review, not this one: its park check (`SpawnedByID == target.ID || Qualify(SpawnedBy) == WireName`) is a
    third way of finding a launcher, and it ignores `report_to`.
- **Landing.** The only conflict is the test-plan append, and the merged tree builds.

## Mediums

### M1: the `report_to` step, the step that matches `launcherOf`, is untested

The changelog sells `launcher_id` as "`report_to`, then `spawned_by_id`, then `spawned_by` as a wire name, as the
daemon's `launcherOf` resolves it". No test sets `report_to`. Removing that step leaves every test green (mutant 4),
and so does removing all three self checks (mutant 5).

My throwaway cases show the code is right today. Add these rows to `TestLauncherIDOnTheTaskRow`, or a store test:
- `report_to` by alias, giving a different card than spawned_by_id;
- a stale `report_to`, which falls back to spawned_by_id;
- a `report_to` naming the card itself, which falls back;
- a card whose spawned_by is its own handle, which is omitted.

### M2: `launcher_id` makes `/v1/tasks` about 7x slower, and nothing reads it yet

`withSeen` calls `LauncherID` once per row:
- one `ReportTo` query;
- then one to five lookups (`GetByWireName`, `GetByAlias`, `Get`).

Every other decoration on this path is one batched map (`RepliesOwed`, `OpenAskCounts`, `MergedViews`, `SeenAll`). At
301 cards the list went from 5.3ms to 36.9ms, all of it on the store's single connection, and the hub fans
`/v1/tasks` out to every room for every board.

No board JS reads `launcher_id` yet (`git grep launcher_id internal/api/web` is empty), so today this is pure cost.

Fix: add a `store.LauncherIDs()` that reads `id, wire_name, alias, spawned_by, spawned_by_id, report_to` in one SELECT
and resolves in memory, with the same order and self checks. Keep `LauncherID` for the single-card paths (the event
and PATCH).

## Lows

- **L1: the hub lookup's `name@room` handling and the first-dept order are untested** (mutants 10 and 11). Add a
  `launcherTags` case with `who = "boss@room"`, and a launcher carrying two dept tags.
- **L2: one more full list per launch.**
  - `launcherTags` (hub) and `myTags` (stdio) each GET the whole `/v1/tasks` to read one card's tags. With M2 that is
    the slow list.
  - On a cross-room hub launch, `resolvePeer` has just read the same room's list. Reuse it.
  - Otherwise fetch the one card, `/v1/tasks/{id}`, when an id is known.
- **L3: `myTags` matches only the exact wire name.** That is fine for a daemon-launched card, where
  `ATRIUM_AGENT_NAME` is its `WireName`. A hand-set bare name on a tenant-named room stamps no dept, though the room
  still qualifies `spawned_by` and files the launch. The hub's `matchCard` accepts a bare name and an alias. Use the
  same rule, or say why not.
- **L4: the `view.LauncherID` comment says "The aggregate view prefixes the room".**
  - The hub's fan-out tags only `id`, so `launcher_id` goes through bare, and a client has to pair it with `room`.
  - Remembered (offline) cards are served from the cached stored row, so they have no `launcher_id` at all.
  - Change the comment to say both. The changelog's "bare" is right.

## Test plan

The new section is still headed `@LETTER@`. Landing's `docs/test-plan.md` has IG, II, IN, IO, IP and IQ, with ID, IE,
IH, IF, IJ, IK, IL and IM reserved. **IR** is the next free letter.

Atrium-Verdict: room-ok d739825c..73dd8df4
Atrium-Verdict: hub-ok d739825c..73dd8df4
Quality: small and well aimed. One shared helper serves both launch paths, a failed lookup never fails the launch, and
`launcher_id` matches `launcherOf` case for case. What is missing is a test of its `report_to` step, and a batched
version before the board reads it.
