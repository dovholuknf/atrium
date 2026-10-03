

## Re-read: ca5a1be3

Range `1c2997c6..ca5a1be3`, one commit, for the room and the hub. It takes M1, M2 and L1 to L4.

Verdict: **HOLD** on M3, a one-line fix. Everything else is closed. No board reads `launcher_id` yet, so M3 makes the
field wrong but harms nothing today.

## How it was checked

- I read the diff, then `LauncherID` and `LocalCard` beside `GetByWireName`, `GetByAlias`, `Get` and `getBy`. I also
  read the task schema, the task insert, `launcherTags` with `matchCard`, and `myTags`.
- gofmt is clean on store, api, cli and link, and `go vet` passes on all four.
- Tests:
  - store, cli and link pass. I ran link alone, on the trial merge.
  - api fails only on TestTheWalkerLaunchSetAndClear, which is known.
  - daemon is unchanged in the range.
- Onto `claude/landing` 7ad42ff2 it merges with no conflict, and `go build ./...` passes.
- Timing for `GET /v1/tasks` with 301 cards, each launched by `boss` with spawned_by_id and `report_to`, best of 5:
  - **6.2ms at the tip**;
  - **42.4ms** with the per-row path forced, as at 1c2997c6;
  - **3.9ms** with no `launcher_id` at all.

  So the decoration now costs about 2ms, down from about 38ms.
- Mutants, with backups in my own temp dir:

| Mutant | Result |
|---|---|
| The batch's `report_to` self check dropped | killed |
| `?name=` ignored by `listTasks` | killed |
| A done alias wins over a live one in the batch | **survives** (L5) |
| The batch keeps an archived card's alias | **survives** (L5) |
| `myTags` without its one-card answer | **survives** (L6) |
| The wire map keeps the last row, not the first | survives, equivalent: `wire_name` is UNIQUE |

## Closed

- **M1.** `TestLauncherIDResolveAndTheBatchAgrees` covers:
  - `report_to` by alias, handle and id;
  - a stale `report_to`;
  - a self `report_to`, a self `spawned_by_id` and a self `spawned_by`;
  - `@human`, another room, and a card nobody launched.

  It holds `LauncherID` equal to `LauncherIDs` on every card. The batch's self check is pinned.
- **M2.** `LauncherIDs` is one SELECT, resolved in memory, in the same order as `LauncherID`: `report_to` through
  wire name, live alias, done alias, then id; then `spawned_by_id`; then `spawned_by` as a wire name, never `@` and
  never the card itself.
  - The alias maps copy `GetByAlias`: normalized, never dead or archived, live before done, newest first.
  - The wire map copies `GetByWireName`: `Qualify`, exact, and unique.
  - `withSeen` uses the batch for a list of more than one row. A single row, the event and PATCH keep `LauncherID`.
    That threshold is right.
- **L1.** `launcherTags` has a test for `boss@room` and for two dept tags, where the first is stamped.
- **L2.** `GET /v1/tasks?name=` answers `LocalCard(name)` as a list of one or none.
  - It is a parameterized lookup on the same route and auth as the full list, and the query value is escaped by
    both callers.
  - A name resolves to one card, never several: the wire name is unique, and the alias order is fixed.
  - On the hub, the answer still goes through `matchCard`. So a room that ignores `?name=` is matched exactly, and a
    new room's card that `matchCard` would not accept stamps nothing. That is the safe side.
- **L3.** `myTags` takes the one card the room resolved, so a bare name or an alias works on a new room.
- **L4.** The `view.LauncherID` comment now says the hub does not prefix the id and a remembered card has none.

## M3: a card with no wire name empties the whole batch

`wire_name` is a nullable column. The insert writes `nullable(t.WireName)`, so an intake card (`Offer`) or a work item
with no session is stored with NULL, and `scanTask` reads it as `sql.NullString`. `LauncherIDs` scans it into a plain
string instead. The Scan fails on the first NULL row. The error is dropped in `guard`, so `rows` holds only what came
before it. The query is `ORDER BY created_at DESC`, so one newer intake card means an empty map.

A probe proved it: a launched `kid` and one `Offer` made after it. `LauncherID(kid)` is boss's id. `LauncherIDs()` is
`map[]`. Because `withSeen` uses the map whenever it is non-nil, every row in the list loses its `launcher_id`, while
the single GET still has it.

Fix: select `COALESCE(wire_name, '')`, or scan into `sql.NullString`. Add one NULL-wire card, newer than the others,
to `TestLauncherIDResolveAndTheBatchAgrees`. As a second guard, have `LauncherIDs` return nil when the query fails, so
`withSeen` falls back to the per-row path rather than serving an empty map.

## Lows

- **L5: the batch's alias rules are not pinned.** Putting done before live, or keeping an archived card's alias,
  passes every test, and either would make the batch disagree with `GetByAlias`. Add a live card and a done card
  with the same alias, plus an archived card holding one, to the agreement test.
- **L6: the `myTags` paths are untested on the cli side.**
  - Dropping the one-card answer passes the cli tests, so the alias case of L3 has no cli test.
  - The one-card answer also trusts any single row. If a room ignores `?name=` and holds exactly one card that is not
    the caller (a hand-named session on an older daemon), its dept is stamped.
  - Check the row against `me` (handle, bare name or alias) before taking it, and test both cases.
- **N1: `?name=` answers a thinner row than the list.** It adds ask counts but not `withSeen` and the other list
  decorations, and it ignores `?status=`. That is fine for the two callers that want tags. Say so in the comment, so
  nobody uses it as a one-row list.

Atrium-Verdict: hold 1c2997c6..ca5a1be3
Quality: a well-aimed fix. One SELECT and one map copy the store's own lookup order, and the agreement test is the
right shape. The hold is a NULL scan that the test table never contains.

## Re-read: f7642322 (M3 closed)

Range `1c2997c6..f7642322`. The second commit, on ca5a1be3, takes M3, L5, L6 and N1.

Verdict: **OK** for room and hub.

- **M3.** `LauncherIDs` selects `COALESCE(wire_name, '')` and returns nil when the read fails, so `withSeen` falls
  back to the per-row path. The agreement test adds a newer intake card with a NULL wire name and asserts the batch
  is non-nil and agrees.
  - Every NULL-wire row now lands in `wire[""]`. That is harmless: `byWire` is only reached with a non-empty name
    (`r.by` is checked, and `local(r.reportTo)` is used only when `reportTo` is set), and `Qualify` never makes a
    name empty.
  - Skipping `r.wire == ""` when building the map would say so in the code (N2).
- **L5.** The alias rules are pinned: a live alias beats a newer done one, and an archived card holds none.
- **L6.** `myTags` trusts a row only when it is named `me` by wire name, bare name, alias or id. A cli test covers a
  single row that is not the caller.
- **N1.** The `?name=` comment says it answers the thinner single-GET row.
- **Checks.** I read the diff. Per @runtime, the mutants (no COALESCE, an archived alias, done before live, the L6
  check dropped) are each killed. My gates and the merge onto landing are below.

Atrium-Verdict: room-ok 1c2997c6..f7642322
Atrium-Verdict: hub-ok 1c2997c6..f7642322
