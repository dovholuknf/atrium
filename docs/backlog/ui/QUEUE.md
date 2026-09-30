# @ui queue

Top down. @ui takes the next item from the top of this file, not from messages. The orchestrator reorders it by
editing this file. After a restart or a new context, @ui reads this file first.

Each item: what it is, where the spec is, and its state. @ui moves an item to "Done" with its landing sha when it is
live.

## Queue

1. **Card URLs U1 and U2.** `docs/rnd/card-urls-design.md` section 9. U1: the board reads `/alias/...` and
   `/room/...`, the lookup, the clash chooser, the per-path last id, the links the board builds. U2: the phone paths.
   Worker `u-card-urls` on m1mini, branch `claude/u-card-urls` 8333259b done and reviewed. Then full suite, land,
   hub-only deploy, report line.

## Filed, not queued

- `docs/backlog/ui/u-new-suite-flakes-0930.md`: cacheChip fails in the full run only. heldLine is fixed (fcf3b974).
- A popped-out card's growler reminder makes no sound while the board window has focus: the board skips a popped-out
  card and the pop-out skips when focus is elsewhere. Low.

## Done

- 2026-09-30 17:11, cross-window silence for the ready alert: claude/main 23fa4602, hub board 2bb25a14.
- 2026-09-30 17:11, growler U1, U2, U3 with the pop-out rules: claude/main eb94e016, hub board 2bb25a14.

- 2026-09-30 15:08, ready-alert spam fix (once per wait, 5 s quiet, focused window silent): claude/main 5e68b83f,
  hub build 19e2f84-7931c7d0.
- 2026-09-30 15:08, per-popout notification bell: claude/main 19e2f840, same build.
