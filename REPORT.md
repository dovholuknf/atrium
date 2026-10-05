# u-term-debug-and-lag

Branch `claude/u-term-debug-and-lag`, from claude/main e2ed7864, on m1mini.

## Part 1: the debug switches are per card

Commit 4c9d8371.

- `atrium.debug.typing.<card id>` and `atrium.debug.inputlag.<card id>` in localStorage replace the two single keys. The old keys are removed at load.
- `typingFollow()` and `lagFollow()` (js/typing.js, js/inputlag.js) set each switch, and the polling or timing behind it, from the card showing. They run:
  - when a terminal opens (`openTerm`)
  - when the pane clears (`clearTermPane`)
  - when the drawer draws (`dbgFollow` in js/peek-debug.js)
  - when another window changes a key
- Turning a switch on for one terminal leaves the others off, unpolled and untimed. Lag timing also ignores output from kept background terminals.
- The hub and the room log per machine, not per card. The board tells them `input_lag_log: true` while any card in this browser has lag logging on, and false once the last one is off.
- The machine's answer (ATRIUM_DEBUG_INPUTLAG, or another browser's switch) no longer ticks this card's box.
- The ATRIUM_DEBUG_INPUTLAG note stays a note: "ATRIUM_DEBUG_INPUTLAG is set on this machine, so the hub and the room log for every card as that says, and this box only switches this browser's timing."
- docs/terminal/input-lag-logging.md is updated to match.
- Tests:
  - the new headless section `termDebugPerCard`
  - termDebug, typing and prefsEverywhere moved to the per-card keys

## Part 2: the input lag

### Cause

This was a bug in the vendored xterm 5.5, and our code set it off.

1. **Every new line fires a selection redraw.** In xterm 5.5, each line the buffer scrolls fires `SelectionService.refresh()` from `onScroll` (`vendor/xterm.js:1:34498`). That happens whether or not anything is selected.
2. **The pause check is skipped.** On the next animation frame, `RenderService.handleSelectionChanged` (`xterm.js:1:114432`) passes the redraw straight to the renderer. It does not check `_isPaused`, which every other redraw path does.
3. **The DOM renderer redraws everything.** `DomRenderer.handleSelectionChanged` (`xterm.js:1:81820`) calls `renderRows(0, rows-1)`, which runs `createRow` for every row. `createRow` calls `WidthCache.get` for each cell. The WebGL renderer only notes the selection.
4. **Hidden cells force a style recalc.** Under `display: none`, `WidthCache._measure` (`xterm.js:1:93484`) reads `offsetWidth`. Each read forces a style recalc and returns 0. `WidthCache.get` (`xterm.js:1:93199`) only caches widths above 0, so it measures again on every call.

Our trigger is in `keepEnforce` (js/terminal.js). Only the 4 most recent kept terminals (`KEEP_GL`) keep WebGL. Past those, `dropWebgl` gives the context back, and xterm falls back to its DOM renderer while the terminal is hidden. That DOM renderer starts with an empty width cache that can never fill.

With the default of 8 kept terminals, 3 hidden terminals redraw every row for every line their agents print, forcing tens of thousands of style recalcs a second inside animation frames. That is the "'requestAnimationFrame' handler took 147ms" and "Forced reflow while executing JavaScript took 132ms" clint saw, and the long tasks every few seconds.

The comment at `dropWebgl` assumed the fallback renderer "is paused while the terminal is hidden". That is true for writes, but not for selection redraws.

A browser that refuses WebGL outright is also exposed through the same path, but less: its width caches mostly fill while each terminal is shown.

### Fix

`holdHiddenRedraw(t)` in js/terminal-list.js is applied to every terminal right after `term.open`.

- While xterm says the terminal is paused, a selection redraw only records the selection and sets `_needsSelectionRefresh` and `_needsFullRefresh`. When the terminal shows again, xterm's own intersection handler does a full redraw that includes the selection.
- When not paused, it calls xterm's method unchanged.
- It touches private fields of the vendored xterm 5.5. Each one is checked first, so a different xterm gets nothing patched.

The other option was raising `KEEP_GL` so no kept terminal ever loses WebGL. I didn't do that: it costs GPU memory, it leaves a browser that refuses WebGL exposed, and the fix above covers both.

### Before and after

These come from `node scripts/perf-board.js --only lag --terms N --lag-secs 30`:

- a real `atrium preview` with 120 cards
- real ptys streaming a line every 150ms
- activity on 12 cards 4 times a second, and a 50-card edit burst every 2s
- "a" typed every 150ms into the shown terminal
- Chromium headed-headless on the real GPU of m1mini, dpr 2

| 8 terminals, WebGL (default keep 8) | before | after |
|---|---|---|
| long tasks / min | 0–36 (5–8s probes: 67–120) | 2 |
| worst animation frame | 22.1–22.6 ms | 0.9 ms |
| frames over 16 ms | 112–119 | 0 |
| forced layouts in rAF | 67,000–72,000 | 403 |
| forced layout time in rAF | 1,410–1,525 ms | 31 ms |
| renderer CPU | 55–57 % | 9.9 % |
| GPU process CPU | 5 % | 6.8 % |

| other loads, after | 4 terminals, WebGL | 8 terminals, DOM only (`--lag-dom`) |
|---|---|---|
| long tasks / min | 2 | 2 |
| worst animation frame | 1.1 ms | 4.0 ms |
| forced layouts in rAF | 437 | 771 |
| renderer CPU | 9 % | 15.8 % |

Before the fix, 4 WebGL terminals were already fine (worst rAF 0.5–5 ms, about 420 forced layouts). The problem started at the fifth kept terminal.

After the fix, the remaining long task, about 1 per run (worst task 73–84 ms), is the one-off `new AudioContext()` in js/notify.js when the first keydown unlocks sound. It does not repeat.

The remaining forced layouts are xterm's `Viewport._innerRefresh` (`xterm.js:1:50050` reads `offsetHeight`, `:50436` reads `scrollTop`) once a frame on the shown terminal, at well under 1 ms each. The `perf-board` timing puts the shown terminal's viewport refreshes at 0.6 ms worst, and hidden ones at 0.2 ms worst. These are not worth patching.

Not reproduced here: Brave's GPU process at 739%. On this machine's Chromium the GPU process stayed at 5–7% both before and after. If Brave stays high after this lands, the next place to look is Brave itself, with WebGL contexts: 5 here (the shown terminal plus 4 kept).

### Repro and tests

- **`scripts/perf-board.js --only lag`.** Options:
  - `--lag-secs`
  - `--lag-trace FILE` (Chrome trace)
  - `--lag-dom`
  - `--lag-depth N` (stack frames per forced-layout line)
  - `--lag-probe` (kept terminals' renderer and pause state, and the stacks that ask the selection to redraw)

  It prints the top forced-layout stacks.
- **Headless `termLag`** (not in the default list; run it with `HEADLESS_ONLY=termLag`): a 4-terminal, 120-card mock board with a CDP trace. `TERMLAG_STRICT`, `TERMLAG_TRACE`, `TERMLAG_PROBE`, `TERMLAG_SECS` and `TERMLAG_CARDS` control it.
- **Headless `termHiddenRedraw`** (in the default list): a kept, hidden terminal that has dropped WebGL gets 500 lines and must measure under 200 cells. Without the fix it measured 25,176. Shown again, it must draw the last line.

### Test runs

- Passed with the fix: `HEADLESS_ONLY=bootClean,termDebugPerCard,termHiddenRedraw,termDebug,typing,prefsEverywhere,termLag node scripts/test-board-headless.js`, and `go build -o build.claude/ ./...`.
- `bash scripts/check-board.sh` exited 1. The failures are in places this branch does not touch:
  - ReferenceErrors for `attachIsInFlight`, `keepReconcile` and `termBgRun` in test-term-nesting, test-term-retag and test-term-resize-settle
  - the follow-scroll and isAutoReport terminal invariants
  - native titles in hubrepos.js and changereq.js
  - the website skin's frosted header
  - phoneListFit card widths
  - a page.click timeout

  I started a comparison run on base e2ed7864 and stopped it when the orchestrator asked, so this branch has not been checked against base. The orchestrator runs the whole suite after landing.
