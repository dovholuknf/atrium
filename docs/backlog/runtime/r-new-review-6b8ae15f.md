# Review: r-owed-answers 6b8ae15f

Range `fe891b44..6b8ae15f` on `claude/r-owed-answers`. This is W6 of docs/rnd/long-turn-checkin-design.md section 11,
which got doc-ok at 30cd5837. It touches 28 files:
- New: `internal/daemon/owed.go`, `internal/store/owed.go` and `reserved.go`.
- Hooks into finish, session, shelve, the say paths, the api view and `owed-dismiss`.
- The hub's `atrium_task dismiss`.

Commits on m1mini are unsigned.

Verdict: **hold** for room and hub, on M1 to M5. A probe test in scratch shows each one. The design is built as agreed,
and the tests that are there are good: six of seven mutants die.

## How it was checked

- I read every hunk of the diff. I also read the code outside it that uses `reported_at` and `OwesReport`: home.js,
  card.js, `stoppedSilently`, `launcherSeen` and `exitsOnReport`.
- Probe tests in a scratch worktree at the tip. The outputs are quoted under each finding.
- Mutants, run against the owed, reserved, dismiss and listing tests:

  | Mutant | Result |
  |---|---|
  | The `OwedClosedAt` reopen guard removed | killed by `TestADismissClosesAndAnExitClosesTheItem` |
  | The skip for an orchestrator's own worker removed | killed by `TestTheOrchestratorIsNeverToldAboutItself` |
  | `Pushed.IsZero()` removed | killed by `TestTheOrchestratorHearsOnce...` |
  | `listOwed` taken out of the compact path | killed by `TestTheListingLine...` |
  | The `reported_at` gate always true | killed by `TestAReportThatReachesNobody...` |
  | `isReserved` always false | killed by `TestTheAtriumHandle...` |
  | `host.ID != t.ID` in `openOwed` removed | **survived**, see L2 |

- Store, link, api and daemon tests, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` unset. Store and link pass.
  api and daemon fail only on reds that were there before this branch:
  - hostterm and ptyhost;
  - the keepalive fork test;
  - `TestNoTestHereCanReachALiveRoom`, which fails on `fe891b44` too;
  - `TestTheWalkerLaunchSetAndClear` (api), which fails the same way on `fe891b44`.
- vet is clean. gofmt flags only two files, and this branch changes neither:
  - `internal/daemon/fyi_test.go`, already unformatted on landing;
  - `cmd/ptyhost-spike/pipe_windows.go`.
- Four test fixtures rename the card `atrium` to `atriumx` and change nothing else: `resumeclaim_test.go` in daemon
  and in store, `alias_test.go` and `tenant_test.go`. Every assertion is kept, including the qualified `sg4/atriumx`
  lookup.
- The merge onto `claude/landing` conflicts only in `docs/test-plan.md`. There, IG and this item's section are added
  at the same place. Keep both. The letter is **IJ**, since r-hub-remote has landed as II. The merged tree builds and
  vets, and the owed tests pass on it.

## The design acceptance

| Earlier note | State |
|---|---|
| M1: local tag, else the hub's through cross-room, else the chip | Built in `remoteOrchestrator`. Its handle fallback is M1 below. |
| M2: a done report with no ask closes its own debt; a fyi opens nothing | Done: `owedKind`, `SetOwedAsk`/`ClearOwedAsk` in finish, and tested. |
| M3: a mechanical close when the reason is gone | Done: `owedPass` re-runs `owes` before every push. |
| L1: the bound is 2+N, and the orchestrator is never told about itself | Self-skip done and tested. The bound is broken by M2 below. |
| L2: the reservation also catches NameFromDir | Done. The launch path is missed, M5. |
| L3: W0's row | `owed`, `owed_since` and `owed_no_launcher` are on the view. |
| L4: an operator-started director has no launcher | It owes nothing, because `owes` needs `reportsToLauncher`. Its workers keep their items on its card. |

## The six points

1. **RemotePeer tags and the handle fallback.** Not safe yet, see M1.
   - An old hub sends no `tags`, so today every peer reaches the handle check.
   - Cross-room senders arrive as `handle@room` (`control_relay.go:40`), so a remote card cannot pose as bare
     `atrium`.
2. **The `atrium` reservation.**
   - Holds for case, whitespace and `sg4/atrium`. An alias also refuses lookalikes, by its ASCII shape.
   - A folder-derived name becomes `atrium-dir`, the same each time.
   - The launch path is missed (M5), and told names still accept lookalikes and `atrium@x` (L1).
3. **Nothing is written to the worker.**
   - True in practice: every push goes to the host, the orchestrator or the launcher, and `listOwed` skips the
     worker's own item.
   - The one guard that keeps a held notice off the worker's own card has no test (L2).
4. **The bound, reopening, self-pushes and closes.**
   - One push per item, and none about the orchestrator itself.
   - Both SessionStart paths carry the listing line.
   - A close by exit or dismiss does not stick (M2), and an orphan's done report sends three notices (M3).
5. **`reported_at` only when a notice or relay is queued.**
   - `exitsOnReport` does not read it.
   - `stoppedSilently` and `launcherSeen` read `OwesReport`, which is false for any card not launched by an agent.
     So an agent-launched card that reached nobody now stays owing: that is the intent, and it leads to M3.
   - The operator's "report waiting" mark and the card's "Last report" line now never show for a card the operator
     launched (M4).
6. **The report-no-launcher hold.**
   - It goes to the local orchestrator, or else to the hub's orchestrator through the relay outbox.
   - It is skipped for an orchestrator-tagged worker, and held, never typed.
   - Tested by `TestAReportThatReachesNobody...`.

## Mediums

### M1: an untagged card called `orchestrator` on any room gets every orphan push and orphan report, and is cached for good

`remoteOrchestrator` accepts `len(p.Tags) == 0 && EqualFold(p.Handle, "orchestrator")`. The hub sends no tags today,
so this is the path that runs.

Any card on any room matches if it was told the name `orchestrator`, or if it started in a folder of that name. It
then receives every owed push and every orphan report, including the worker's last 200 characters and the full report
body. That is an information leak across rooms.

The first match is written to `orchestrator_remote`, and it is read back whenever Peers fails or no longer lists a
match. Nothing checks it again.

Probe:
- A fake relay listed `{Handle: "orchestrator", Room: "someroom"}` with no tags.
- That peer got `its launcher has not answered worker for 10 minutes...`, and `orchestrator@someroom` was cached.
- With the peer list emptied, `remoteOrchestrator()` still returned `orchestrator@someroom`.

Fix:
- Match by tag only. With no tagged peer, push nothing: the chip is the agreed floor.
- Have the hub send `tags` on `/peers` (a note to @fabric).
- Use the cache only when Peers fails. Clear it when Peers answers and no tagged peer is listed.
- Add a test that an untagged `orchestrator` peer gets nothing.

### M2: a closed item reopens as soon as the worker's last activity moves, which breaks the 2+N bound

`owedPass` reopens an item when `since.After(OwedClosedAt)`. For an `ended` item, `since` is `LastActivityAt`, and
`SetStatus` (`tasks.go:745`) and several other writes move it.

The everyday case:
1. A worker ends with no report, and the item opens.
2. The launcher exits it, and `StopRunner` closes the item.
3. The runner goes, the card turns `dead`, and `last_activity_at` moves.
4. The item reopens, and the launcher gets a second "owes an answer" notice.

A dismiss followed by any status change does the same.

Probe: open an item, run `closeOwed` ("dismissed"), then `SetStatus(dead)`, then `owedPass`. The result was 1 item
open and 2 held notices.

Fix:
- Key the reopen guard on the reason. For example, store the closed item's reason and `since`. Reopen only for a
  different reason, or for a new launcher prompt (`OwedAt` after the close).
- Add a test for an exit followed by `dead`.

### M3: an orphan's done report sends the orchestrator three notices, and two of them are wrong

`reported_at` is not stamped, so `OwesReport()` stays true. `owes` then opens an `ended` item on top of
`orphanReport`'s notice, and the orchestrator gets three notices:

1. the `report-no-launcher` notice, which is correct;
2. "owes an answer: worker (done, no report)", which is wrong: it did report;
3. at ten minutes, "its launcher has not answered worker", although there is no launcher.

Probe: an orphan worker, a done report, then `owedPass` at 0 and at 10 minutes. The orchestrator held exactly those
three notices.

Fix:
- Have `orphanReport` count as the item. Or keep `owes` from opening an `ended` item for a card whose last report
  went to the orchestrator.
- Word an orphan's push without "its launcher".

### M4: the operator's "report waiting" mark is dead for every card the operator launched

Two board marks read `reported_at`:
- `home.js:72` shows "report waiting" when `spawned_by === "@human" && reported_at > human_at`.
- `card.js:310` shows "Last report N ago".

A card the operator launched has no `launcherOf` and no relay. So after this change its report never stamps
`reported_at`, and both marks go dark. That is the opposite of the r-scm-clone fix, which was about orphans that an
agent launched.

Probe: a card with `spawned_by @human` reports done, and `reported_at` stays nil.

Fix:
- Stamp when `r.Notice != nil || r.Relay != nil || !launched`, so only the agent-launched orphan skips it. Or keep a
  separate `last_report_at` for the board.
- Add a test for a card the operator launched.

### M5: a launch with no title in a folder named `atrium`, or with the title "atrium", now fails

`launchedName` takes the title slug, or else the folder name. It passes that to `Register` as a told name with no
`NameSource`, so `ErrReservedName` fails the launch.

This repository's own checkout is named `atrium`. So a launch there with no title, from the board or from
`atrium_launch`, breaks.

Probe: `launchedName("", ".../atrium")` gives `atrium`, and `Register` refuses it. The title `Atrium` does the same.

Fix:
- Have `launchedName` treat the reserved handle as taken, so it moves on to `atrium-2`. Or map it to
  `ReservedDerived`.
- Add a test.

## Lows

- **L1: lookalikes and `@` in told names.**
  - `Register` accepts these as told names: `atrium@x`, a Cyrillic `аtrium`, and `atrium` with a zero-width space.
  - The first renders like atrium's voice on room x. The others render as `atrium`.
  - An alias is safe because of its shape check. Give told names the same ASCII shape, or at least refuse `@` and
    anything that is not ASCII in a name.
- **L2: no test keeps a notice off the worker's own card.** The `host.ID != t.ID` mutant in `openOwed` survived.
  - Add an orphan with no orchestrator.
  - Assert that the worker's own card holds no `owed` notice.
- **L3: `atrium-dir` and existing `atrium` cards.**
  - A folder-derived `atrium` joins an existing card that was told `atrium-dir`.
  - An existing card named `atrium` from before the upgrade is passed over. A derived session makes a new
    `atrium-dir` card, and a told resume of the old one is refused.
  - Say this in the changelog, or rename such cards once at startup.
- **L4: the stdio `atrium_task` seems to lack `dismiss` (plausible).** Only the hub's `control_mcp.go` gained it, and
  a grep of the cli finds nothing. If the stdio tool is meant to match, add it there.
- **L5: a worker whose launcher is on another room opens as an orphan (plausible, not probed).** `openOwed` asks only
  `launcherOf`, which is local, so the item goes to the local orchestrator marked as an orphan. Check this against
  `launcherRelay`.

Atrium-Verdict: hold fe891b44..6b8ae15f
Quality: a careful build of section 11 with good tests: six of seven mutants die. The holds are all at the edges:
- the untagged-handle fallback;
- a close that a status change undoes;
- an orphan report counted twice;
- the board's mark for cards the operator launched;
- the launch path the reservation missed.
