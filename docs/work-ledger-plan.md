# The work ledger, the plan

The full design is `docs/work-ledger-design.md`.

## What goes wrong today

- A card reaches `done` when a worker says so, when its session exits, or when somebody drags it. So `done` says a
  session stopped, not that the work is finished.
- A worker's report goes into the launcher's terminal and nowhere a reader can find it later. After a crash or a
  `/clear` it is gone.
- Where the work lives (branch, commits, files) is prose inside that report.
- So when sa19 and sa20 died in a crash, their cards still read `done`, and the orchestrator rebuilt the truth by
  hand a day later.

## What changes

- Every card another session launches gets a work item in atrium's store. It holds the brief, who launched it, a
  work state, where the output is, and a log of every report, every message between worker and launcher, and
  every verdict.
- The work state sits beside the card's column and does not replace it. The column says what the session is
  doing. The work state says where the work is.
- A worker's `done` report moves the work to `reported`, waiting on the launcher. Nothing closes until the launcher
  accepts it, rejects it back to the worker with a reason, or abandons it with a reason. The human can overrule
  any of these.
- A session that ends before a `done` report, by crash, kill or exit, moves its work to `ended-without-report`. This
  happens in the same write that records the exit, so no exit path can miss it. A sweep on startup catches the rest.
- After a crash, atrium flags those items, tells each launcher once (queued, so a launcher that died too hears it
  when it comes back), and shows them red on the board. It never resumes or relaunches anything. The launcher or
  the human decides.
- A final report names its outputs in a fixed shape: repo, branch, commits, files. Atrium checks that each one
  exists and marks what it could not find. It still accepts the report, and the launcher judges it.
- Work that moves to a new card, as sa19 and sa20 did, links the old item to the new one, so the list shows one
  open piece of work, not two.
- The log is bounded per item, and nothing that changed the state is ever trimmed. Items outlive their cards.
- The room also rewrites a `work-ledger.md` file beside its database on every change, so the list can be read with
  the daemon down.

## What you see

- A Work view on the board: every launched card, its launcher, its state and for how long, its last report, its
  outputs with a mark per check. Ended-without-report rows first and red, then work waiting on a launcher.
- Opening a row shows the whole history, which is what used to scroll away in the orchestrator's terminal.
- The main board gets a small work chip on each launched card, and the columns stay as they are.
- `atrium_peers mine` gives a launcher its open work, including workers whose sessions are gone. That replaces
  building `AGENTS-UNFINISHED.md` by hand. `atrium_task` shows one item's state, outputs and recent log.
- A new `atrium_verdict` tool is the launcher's one verb: accept, reject, abandon or reopen. Only the launcher or
  the human can use it on an item.

## Open for you

- A worker's `done` report still moves its card to the `done` column, with the work chip saying `reported`.
  Recommended, so the board does not ring you for the launcher's decision.
- Cards you launch from the board get no work item unless you ask for one with "track this work". Recommended.
- Work waiting on a launcher does not nag anybody. The board shows how long it has waited. Recommended.

## Build stages

1. The ledger records: reports, messages, the ended-without-report flag, the crash notice, the snapshot file.
2. The launcher rules: `atrium_verdict`, and the work state in `atrium_peers` and `atrium_task`.
3. Structured outputs: branch, commits and files on the report, each one checked.
4. The Work view on the board, with verdict buttons for you.
5. Continuing work on a new card, tracking any card by hand, and handing an item to a new arbiter.
