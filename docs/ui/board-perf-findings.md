# Board performance findings, 2026-10-05

Measured by `scripts/perf-board.js` (item `docs/backlog/ui/u-new-board-perf-workup.md`). Raw numbers are in
`docs/ui/board-perf/`: `main.json` is claude/main d54cfc84, `main-gpufix.json` is the same with the
`u-new-board-gpu-cpu-fix` commit (1a84e706) cherry-pick applied and not committed, the `exp-*` files are CSS
experiments injected with `--inject`, and the `*-rerun.json` files are second idle runs to show the noise.

## Setup

An `atrium preview` of this worktree on its own db and ports, 120 cards (each a real pty), 12 working, the rest idle,
4 terminals attached, Chromium 153 on an M1 mini with the real GPU (headed-headless, SwiftShader off), device scale
factor 2, 1440x813. Load numbers are the median of 3. Idle numbers are a 20 s window per view, one sample, and the idle
CPU and GPU numbers move by about 3 points between runs of the same build (main idle GPU on the terminals view: 12.5,
then 14.9). Read differences of under 3 points as noise.

**The long memory run is shortened.** 20 minutes with a heap, node and listener growth rate per hour, not 1 h and 4 h.
That is long enough to see a leak with this load (about 8 events a second plus a view and terminal switch a minute) and
too short to see a very slow one. `--mem-mins` takes any length for a longer run.

## The numbers (claude/main)

| area | metric | value |
|---|---|---|
| load, cold | first paint / interactive / load event | 144 ms / 588 ms / 198 ms |
| load, warm service worker | first paint / interactive / load event | 84 ms / 421 ms / 81 ms |
| load | requests, transferred cold and warm, decoded | 124, 1348 KB and 575 KB, 3398 KB |
| load | blocking time (long tasks over 50 ms) | 0 ms |
| idle, terminals view | GPU / renderer CPU / frames per second | 12.5% / 5.9% / 60 |
| idle, board view | GPU / renderer CPU / frames per second | 9.7% / 13.3% / 60 |
| idle, stack view | GPU / renderer CPU / frames per second | 11.2% / 9.0% / 60 |
| SSE burst, 200 `task` events in 1 s | script / style / layout | 7 / 18 / 14 ms on the board view, 4 to 5 ms script elsewhere |
| SSE burst | rows rebuilt | 38 of 120 on the board (the cards whose `why` changed), 0 on terminals and stack |
| attach to first byte painted | WebGL / DOM renderer | 70 ms / 62 ms |
| 20 minutes | heap | 10.2 MB to 12.0 MB, flat after the first 3 minutes (0.7 MB per hour fitted from minute 2) |
| 20 minutes | DOM nodes, listeners | 14.5k to 40.4k and 1.5k to 2.7k in the first 3 minutes, then flat |

Idle burn is the only number that is not small. A 60 fps compositor with nothing happening costs about 10 to 15% of a
core in the GPU process and 6 to 13% in the renderer, on the best laptop GPU there is. Everything else is already cheap.

## Worst offenders, ranked by size of win

1. **Looping animations keep the compositor at 60 fps on an idle board.** At idle the page holds 109 running
   animations. 108 are `pulse` (the waiting dot, `css/cards.css:258`), 8 `livebreathe` and 4 `livebob` on the working
   cards, 1 `nudge` on the sound button. Frames per second is 60 on every view, and GPU is 10 to 12%.
   - The `nudge` is the larger half and is already fixed by 1a84e706: idle GPU on terminals 12.5 to 6.6, board 9.7 to
     4.2, frames on terminals 60 to 27, style recalcs per second 44 to 7 (table below). Cost: none, built.
   - **What is left is the `pulse` dots.** With 1a84e706 on, stack view GPU is 14.4%, 60 fps. With 1a84e706 and the
     pulse off (`exp-gpufix-pulse-off.json`) it is 0.6%, 0 fps. The 108 dots come from this fixture (108 cards waiting
     for input at once), so a real board pays less, but one waiting dot is enough to hold 60 fps. **Proposal, not
     built: stop the waiting dot after three pulses, as the sound button now does, or pulse only while the card is
     new to the waiting column.** Win: idle GPU on a stack or board with any waiting card from 14% to under 1%. Cost:
     a few lines of CSS. Trade-off: the dot is the only motion that says "this wants you", and a still dot is easier to
     miss. Clint's call.
2. **The same for `livebreathe` and `livebob` on working cards** (12 cards here). Not separated in this run because
   the pulses hide them. Same trade-off, and `u-new-board-gpu-cpu-fix` already measured `working.gif` at about 5 GPU
   points. Proposal only, not measured alone.
3. **Page weight on a warm load.** Warm transfer is 575 KB of 1348 KB cold, and the request count does not fall (124
   both ways), so the browser revalidates every script and style file on each load. Interactive is 421 ms warm against
   588 ms cold, so the saving is bounded at about 170 ms. Fix is `Cache-Control` and ETag on `/js`, `/css`, `/vendor`,
   which is hub side (@fabric). Trade-off: a stale board after an upgrade unless the URLs carry the build id. Not
   built.
4. **Bundling the 120 scripts and styles.** 124 requests over loopback is cheap (first paint 144 ms). It would matter
   over a remote hub (zrok), where each request costs a round trip. Not measured here. Cost: a build step the board has
   deliberately not had. Not recommended without a remote measurement.
5. **DOM size.** 40k nodes once every view has been opened (4.5k on load), flat afterwards. Not a leak. The cost is
   memory only (heap is 12 MB), and style recalcs per second scale with it. Not recommended.

## Things that look expensive and are not

- **SSE burst.** 200 whole-row `task` events in a second cost 7 ms of script and 32 ms of style and layout together,
  with no long task and no fetch. Rows are patched by key: on the terminals and stack views none are rebuilt, and on the
  board only the 38 cards whose `why` changed. Nothing repaints a whole list (`docs/ui/board-repaint.md` holds).
  The activity hook does not reach the board as a `task` event unless the row changed, so hook storms are cheaper still.
- **Attach.** WebGL is 8 ms slower than DOM to the first painted byte (70 against 62 ms) and a fraction of the idle cost.
  Both are far under a perceptible 100 ms.
- **Memory.** No growth that matters: heap is flat after view and terminal cycling for 20 minutes (the item's
  retained terminals and toast logs do not show). A kept terminal is released on the keep limit.

## The fix already built, measured by this method

`u-new-board-gpu-cpu-fix` (1a84e706), main against main plus the cherry-pick, same load, one idle sample each (a second
run of each is in the `*-rerun.json` files and agrees within noise):

| metric | main | with 1a84e706 |
|---|---|---|
| idle terminals, GPU / frames per s / style recalcs per s | 12.5% / 60 / 43.9 | 6.6% / 27 / 6.6 |
| idle board, GPU / renderer CPU | 9.7% / 13.3% | 4.2% / 8.8% |
| idle stack, GPU | 11.2% | 14.9% (14.4 on the rerun, the dots, see 1) |
| load, attach | unchanged | unchanged (within noise) |

## Fixes built in this item

None. The one clear win with no visible trade-off is 1a84e706, which exists on its own branch and was not rebuilt
here. Everything else in the ranking changes how the board looks or moves, or sits on the hub, so it is a proposal.

## Traces

Performance panel traces were not saved. The harness takes a `viz` trace for the frame count (`Display::DrawAndSwap` per
second) and reads the animation list from the page, which names the offenders directly (`--only anims`). Run
`node scripts/perf-board.js --only anims` to list them, and `--inject FILE.css` to measure a CSS change without editing.

## Limits

Loopback only, so network cost is not in the load numbers. One machine, one GPU. The idle sample is 20 s. Mobile (`/m`)
is not measured: the harness drives the desktop board and the item put `/m` after it.
