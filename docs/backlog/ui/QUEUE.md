# @ui queue

Top down. @ui takes the next item from the top of this file, not from messages. The orchestrator reorders it by
editing this file. After a restart or a new context, @ui reads this file first.

Each item: what it is, where the spec is, and its state. @ui moves an item to "Done" with its landing sha when it is
live.

## Queue

1. **Cross-window silence for the ready alert.** @review's medium on 5e68b83f
   (`docs/backlog/ui/u-new-review-5e68b83f.md`): a focused window showing a card silences that card's ready alert in
   EVERY window, forwarded toasts and sound included. Worker `u-ready-spam`, branch `claude/u-ready-spam`, 32fceb70
   committed, finishing its tests. Then full suite, land, hub-only deploy, report line.
2. **Growler U1, U2, U3 with the pop-out rules.** `docs/rnd/persistent-growler-design.md` sections 4 to 8 and 10, plus
   `docs/backlog/ui/u-new-growler-popout.md`. Worker `u-growler`, branch `claude/u-growler`: U1 04b391fd, U2 f0678829,
   U3 719df4ba, now merging claude/main and adding the pop-out rules. Hub side R1 to R3 is live. Then full suite,
   land, hub-only deploy, one report line per stage.
3. **Card URLs U1 and U2.** `docs/rnd/card-urls-design.md` section 9. U1: the board reads `/alias/...` and
   `/room/...`, the lookup, the clash chooser, the per-path last id, the links the board builds. U2: the phone paths.
   R3 is live on the hub, R4 (the guest allowlist) is coming. Not started.

## Filed, not queued

- `docs/backlog/ui/u-new-suite-flakes-0930.md`: heldLine and cacheChip fail in the full run only.

## Done

- 2026-09-30 15:08, ready-alert spam fix (once per wait, 5 s quiet, focused window silent): claude/main 5e68b83f,
  hub build 19e2f84-7931c7d0.
- 2026-09-30 15:08, per-popout notification bell: claude/main 19e2f840, same build.
