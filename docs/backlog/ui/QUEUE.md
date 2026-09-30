# @ui queue

Top down. @ui takes the next item from the top of this file, not from messages. The orchestrator reorders it by
editing this file. After a restart or a new context, @ui reads this file first.

Each item: what it is, where the spec is, and its state. @ui moves an item to "Done" with its landing sha when it is
live.

## Queue

1. **Paste spinner that works** (clint, top priority, exception to the hold): starts on the paste itself, stops on the
   room's `in-done` frame (@runtime's `claude/r-paste-done` 6ca9e81f), falls back to today's guess on an old daemon.
   Branch, land after clint's push, room deploy for the daemon half.
2. **Growler question card** (clint, exception to the hold): full body in a scroll box, auto-grow reply, expand to a
   full compose area, `{choices}` as buttons, no hover bob. Worker `u-growler`, branch `claude/u-growl-reply`. Land
   after clint's push.
3. **After the push: the headless suite without software WebGL.** From the orchestrator, 2026-09-30. In the 17:25
   run the gpu-process (`--use-angle=swiftshader-webgl`) used 5 min of CPU at 96%, because the board loads
   xterm-addon-webgl (`terminal-list.js`) and headless chrome renders it on the CPU through SwiftShader. Skip the
   addon under test (a flag or query param, so xterm uses its DOM renderer), or launch with `--disable-gpu` if xterm
   falls back cleanly. Measure CPU time for one full run before and after.

## Filed, not queued

- `docs/backlog/ui/u-new-suite-flakes-0930.md`: cacheChip fails in the full run only. heldLine is fixed (fcf3b974).
  phonePan ("the follow chip went away without input") fails alone on claude/main too, about 1 run in 6, more under
  load. peekEverywhere's half-second timing and shiftMenu failed in the full run only, 17:50.
- Card URLs lows from @review (`docs/backlog/ui/u-new-review-8333259b.md`): (1) a pop-out that reloads onto another
  card keeps `window.name` `atrium-term-<old id>`, set it from the resolved id in `bootTerminalOnly`. (2) sw.js
  compares paths exactly, so `/alias/foo/` misses `/alias/foo`.
- A popped-out card's growler reminder makes no sound while the board window has focus: the board skips a popped-out
  card and the pop-out skips when focus is elsewhere. Low.

## Done

- 2026-09-30 17:58, card URLs U1 and U2 (`/alias/`, `/room/`, `/m/` paths, clash chooser, readable links): claude/main
  afae3703, hub build afae370-aa1059bb.

- 2026-09-30 17:11, cross-window silence for the ready alert: claude/main 23fa4602, hub board 2bb25a14.
- 2026-09-30 17:11, growler U1, U2, U3 with the pop-out rules: claude/main eb94e016, hub board 2bb25a14.

- 2026-09-30 15:08, ready-alert spam fix (once per wait, 5 s quiet, focused window silent): claude/main 5e68b83f,
  hub build 19e2f84-7931c7d0.
- 2026-09-30 15:08, per-popout notification bell: claude/main 19e2f840, same build.
