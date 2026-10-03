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

## Re-read: 49488e18

One fix commit on top of the rebased W6 commit, on landing 8bfdcdb1, so the range is `8bfdcdb1..49488e18`.

Closed:
- **M1.**
  - `remoteOrchestrator` matches by tag only.
  - When the hub answers and lists no tagged peer, the remembered orchestrator is cleared.
  - The remembered one is used only when Peers fails.
  - Tested: the untagged peer probe now gets nothing and is not remembered.
  - The changelog says plainly that a push to another room reaches nobody until the hub sends tags (f-new-peers-tags).
- **M3.**
  - `orphanReport` marks the report as told, and `owes` opens no `ended` item for that report.
  - An orphan's push reads "has no launcher".
  - My probe (orphan, done report, `owedPass` at 0 and at 10 minutes) now leaves exactly one notice on the
    orchestrator.
- **M4.** `UnheardIsUnsent` is set only for an agent-launched worker, so a card the operator launched stamps
  `reported_at` again. Tested. Both mutants, the flag forced true and forced false, fail a test.
- **M5.** `launchedName` treats the reserved handle as taken, so the folder `atrium` or the title "Atrium" gives
  `atrium-2`. Tested.
- **L1.** The skeleton compare drops marks and invisible characters, folds full-width and look-alike letters, and
  ignores anything after `@`.
  - Caught: `аtrium` (Cyrillic а), `аtrіum`, `atr<ZWSP>ium`, `<ZWSP>atrium`, `atrium<ZWJ>`, `ａｔｒｉｕｍ`, `ＡＴＲＩＵＭ`,
    `atri<U+0301>um`, `atrium@x`, `Atrium@sg4` and `sg4/atrium`.
  - Passed as legitimate: `atrium-2`, `atriumx`, `my-atrium`, `atrium-dir`, `atrum`, `trium` and `x@atrium`.
  - Mutants on all five of the skeleton's steps fail `TestTheReservedHandleCannotBeSpelledAround`.
- **L2.** Tested: an orphan with no orchestrator writes nothing to its own card. The `host.ID != t.ID` mutant now
  fails.
- **L3.** The changelog says that an `atrium` card from before this change is passed over, and how to rename it.
- **L5.** `RemoteLauncher` is named on the item and in the push. Tested, and the mutant fails.

M2 is closed for the cases it named:
- An exit followed by `dead`, and a dismiss followed by `done` and `dead`, stay closed with one notice.
- A new question after a dismissed one opens a new item.
- A second permission wait after a dismissed one opens a new item, and the same wait does not.

The fix opens M6 below.

### M6: a launcher's typed message to a worker never lets a new `ended` item open

`mayReopen` reopens an `ended` item only when `t.OwedAt.After(c.At)`. When a say is typed, two things happen in this
order:
1. The `prompted` event is written, which sets `owed_at` to the time it was typed: `notePeerTyped` in peers.go, and
   the typed path in messages.go.
2. `peerSaid` runs, `owedSaid` closes the item, and the close is stamped with `now()`.

So the close is always later than the prompt that went with it. Suppose the launcher re-tasks a worker by typing to it,
and the worker ends again without a report. Then `owed_at` is earlier than the close, and nothing reopens. The debt is
lost, the launcher holds nothing, and the orchestrator is never told.

The queued path does not have this problem, because there the prompt is written later, at delivery.

Probe:
- A worker ended, and its item opened.
- On the worker: status running, then `AppendEvent(prompted, from_peer=launcher)`, then `peerSaid` from the launcher.
- The item closed, with `owed_at` = 01:34:02.951 and close `At` = 01:34:02.951.
- The worker reached done again, and `owedPass` ran.
- Result: **0 items**.
- The same steps in the queued order (`peerSaid` first, then the prompt) give 1 item.

Fix: compare the prompt with the ended item's `since`, not with the close. Reopen an `ended` item when
`t.OwedAt.After(c.Since)`. `c.Since` is when the worker last ended, so a prompt after that is new work. A status change
moves `last_activity_at`, not `owed_at`, so exit-then-dead still stays closed. Add the typed-order case as a test.

### Lows

- **L6: four guards have no test that fails without them.** Each of these mutants survives, apart from the known reds:
  - in `mayReopen`, `since.After(c.Since)` and `since.After(c.At)`, each dropped on its own;
  - a different reason always reopening;
  - the orphan-report mark never expiring on a later `owed_at`.

  The third would let a stale question reopen after a dismissed permission wait. The fourth would let a re-tasked
  orphan that ends silently owe nothing. Add a test for each.
- **L4 (still open, now confirmed).** The stdio `atrium_task` exists, at `internal/cli/control_peers.go:139`, and has
  no `dismiss`. Only the hub's `control_mcp.go` gained it. Either add it there, or say in the tool's description that
  a dismiss goes through the hub.

Gates, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` unset:
- store, link and cli pass.
- api fails only on `TestTheWalkerLaunchSetAndClear`.
- daemon fails only on reds that were there before this change:
  - the hostterm set, including `TestAnOldExitedRunDoesNotKillACardWithANewerLiveOne`;
  - `TestKeepaliveForkCarriesALeanCardsPromptToolsAndMCP`;
  - `TestNoTestHereCanReachALiveRoom`.
- vet is clean.
- gofmt flags only `internal/daemon/fyi_test.go` and `cmd/ptyhost-spike/pipe_windows.go`, and this branch changes
  neither.

The merge onto `claude/landing` 110c224a is clean. `docs/test-plan.md` then reads ID, IE, IH, IF, IG, II and **IJ**.
IJ is used once, and IG and II are intact.

The changes reach the hub's `atrium_task dismiss` in `internal/link/control_mcp.go`, so both verdicts apply when this
lands.

Atrium-Verdict: hold 8bfdcdb1..49488e18
Quality: a thorough fix round. Every finding is closed, mostly with a test a mutant fails, and the look-alike check is
careful. The reason-keyed reopen compares a launcher's typed prompt with a close stamped after it, which loses a
re-tasked worker's second silent ending.

## Re-read: 8d1d31ed

Range `632db27e..8d1d31ed`, rebased onto landing 632db27e. Fix commits 816130f5 (M1 to M5) and 8d1d31ed (M6, L4,
L6). I ran `go vet` and the tests with ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG unset.

Closed:
- **M6.** A finished item now reopens when `t.OwedAt` is after the item's `Since`, which is when the worker ended.
  `TestARetaskedWorkerOwesAgainInTheTypedAndTheQueuedOrder` runs both orders: retask then say, and say then retask.
  Each time it ends the worker again and wants one open item on the next pass, and both orders pass. Changing the
  check back to `t.OwedAt.After(c.At)` fails the test.
  - Can a pass move `Since` forward in between? No. `openOwed` returns early when an item already exists, so `Since`
    is written only when the item opens.
- **L6.** The dropped `since.After(c.Since)` term was redundant. `CloseOwedItem` stores `Since` as it was at open
  time, and the close time `At` comes later. Nothing writes `Since` again, so `c.Since <= c.At` always holds and
  `since.After(c.At)` implies the dropped term.
  - The only way to break it is the wall clock stepping back between open and close. Then the new form reopens a
    little sooner than the old one, which is the safe side.
  - Every remaining branch of `mayReopen` has a test that fails when the branch is mutated:
    - a same-reason item that always reopens fails `TestADismissedQuestionStaysClosedUntilANewOne` and
      `TestAQuestionAskedWhileAnItemIsOpenIsClosedWithIt`;
    - a different reason that always reopens fails `TestADifferentReasonFromBeforeTheCloseDoesNotReopen`;
    - an orphan mark that never expires fails `TestAnOrphanThatIsRetaskedAfterItsReportOwesAgain`.
- **L4.** A dismiss closes only an item on the caller's own card, on both paths.
  - **stdio:** the card to dismiss on is the caller's own `ATRIUM_AGENT_NAME`. An explicit `card` only picks the card
    read afterwards, and never the card the item is closed on. Changing the target to the worker's card fails
    `TestStdioTaskDismissClosesAnItemOnTheCallersOwnCard`. The name in that variable is the caller's own claim, the
    same trust as every other loopback call.
  - **hub `control_mcp`:** a non-empty `card` is refused. The caller's card comes from the connection (`agentOf`),
    and the worker must be in the caller's own scope.

Lows, neither holds:
- **N1: the two paths treat an explicit `card` differently.** stdio dismisses on your own card and then reads `card`.
  The hub refuses the call. Pick one; refusing is clearer.
- **N2: the hub dismiss has no test in `internal/link`.** Add one case each for the refused `card` and for a worker
  in another scope.

Test plan: IJ appears once, at "## IJ. Owed answers survive (r-owed-answers)".

Merge onto claude/landing a9307bc7:
- `docs/test-plan.md` conflicts at the end of the file, where landing adds IO and IP and this branch adds IJ. Keeping
  both sides resolves it.
- The Go code merges cleanly, `go build ./...` passes, and store, cli and link pass.
- daemon shows only the known reds: hostterm, ptyhost, keepalive-fork and TestNoTestHereCanReachALiveRoom.

Atrium-Verdict: room-ok 632db27e..8d1d31ed
Atrium-Verdict: hub-ok 632db27e..8d1d31ed
Quality: M6 is fixed at its root, with each time compared against the right moment. The tests run both orders and
fail on every mutant I tried.
