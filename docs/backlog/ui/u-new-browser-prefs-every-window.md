# u-new-browser-prefs-every-window. A browser setting reaches every open window

Status: not started. Owned by @ui. Filed by the orchestrator 2026-10-01, from clint.

## What happened

clint unticked "log terminal input lag" in settings and the console kept printing `[atrium inputlag]` lines. The
console was in another window. `inputlag.js` reads `localStorage` once at load (`lagOn`, line 34) and nothing tells
an already-open window, so a popped-out terminal or a second tab keeps the old value until it reloads.

## Wanted

- Every setting marked "this browser" applies to every open window of the board in that browser at once: main
  tab, popped-out terminals, /m. A `storage` event listener (or a BroadcastChannel) per setting, applied the same way
  the checkbox applies it.
- Sweep every per-browser setting, not only input lag: the typing gate readout, sound prefs, terminal list hides,
  anything read from `localStorage` at load.
- clint's fallback, only for a setting that cannot apply live: the settings row says "N other windows are open,
  reload them to apply".
- Headless case: two pages on one context, toggle in one, the other changes without a reload.
