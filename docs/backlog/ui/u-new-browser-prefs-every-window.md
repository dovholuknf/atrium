# u-new-browser-prefs-every-window. A browser setting reaches every open window

Status: done on branch claude/u-new-browser-prefs-every-window, not merged. Owned by @ui. Filed by the orchestrator 2026-10-01, from clint.

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

## Design note

One helper, `prefLive(match, apply)` in `js/prefs-live.js`, loaded first by the board and by /m. It owns the single
`storage` listener. A setting registers its key (or a `prefix*`) and an `apply` that does what the control does minus
the write. A cleared store calls every `apply` with a null key. The browser never fires `storage` in the writing
window, so the writer applies its own change as before.

The sweep, every `localStorage` key read at load or held in a variable (setting, key, how it follows):

| setting | key | follows |
| --- | --- | --- |
| input lag log | atrium.debug.inputlag | live (own listener, already there) |
| typing gate readout | atrium.debug.typing | live (own listener, already there) |
| growler | atrium.growler | live (own listener, already there) |
| notifications off, per card | atrium.notify.off* | live (own listener, already there) |
| sound mute, volume, tones, expiry | atrium.sound | live, new (`alerting` prefs refilled, settings repainted) |
| copy on select | atrium.copyOnSelect | live, new |
| focus on hover | atrium.hoverFocus | live, new |
| card colours | atrium.cardColors | live, new |
| terminal row wear | atrium.termWear.* | live, new |
| text size, whitespace | atrium.uiscale, atrium.density | live, new |
| terminal list sort | atrium.termSort | live, new |
| terminal list mode and width | atrium.termlist.mode, .w | live, new |
| terminal tray, subagent and agent hides, folded groups | atrium.termtray*, atrium.hidesubagents*, atrium.hideagents*, atrium.termfolded | live, new |
| terminals kept hidden | atrium.termKeep, atrium.termKeepLines | live, new |
| board sort | atrium.boardsort | live, new |
| stack filter | atrium.stack.show | live, new |
| switcher key | atrium.switchkey | label live, new (the key itself was read per keydown) |
| card font and phone zoom | atrium.termfont.*, atrium.termzoom.* | live for the attached card, new |
| toast log badge | atrium.toastlog | live, new |
| grouping, folded cards, new-card chips, landing key, repos view, phone header and tray | atrium.grouping, atrium.folded, ... | live (own listeners, already there) |
| /m list order, filters | atrium.m.homeopts | live, new |
| /m needs or all | atrium.m.mode | live, new |
| /m text size | atrium.mfs | live, new (thread, docs and change request sheets) |
| /m bell and sound | atrium.toastlog, atrium.sound | live (own listener, already there) |

Read on every use, so already current with nothing to do: pop-out size and placement, switcher recents, the dialog
skip list, the walk width, overlay and expose open state, notify off key, the card URL memory.

Left alone on purpose: where the window is (`atrium.view`, `atrium.term`, `atrium.room`, the settings pane, details
open state), which belong to one window, the skin (the daemon owns it, the key is a boot cache), the per-card
desktop or fit view toggle (it reloads its own window by design and a card is attached in one window at a time),
`atrium.downskin` (the daemon down page, which is its own document), and the console-only `atriumScroll` flag.

No setting needed the "N other windows are open, reload them to apply" fallback, so no settings row text changed and
there are no before and after PNGs.

Test: `prefsEverywhere` and `mPrefsEverywhere` in `scripts/test-board-headless.js`.
