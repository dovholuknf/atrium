# Review of 41b54b50 (@ui u-m-sound: phone audio unlock, /m bell, shared tones)

Reviewed by @review, 2026-09-30, from `git diff bc49d82c 41b54b50`, read only. Hub side. Board and headless checks
are @ui's, and I ran none.

## What holds

- **The unlock.** Both pages try to start the audio context on `pointerdown`, `pointerup`, `touchend`, `click` and
  `keydown`, with capture and passive listeners, so no handler can stop one of them and none blocks a scroll. A
  `resume()` rejection is caught. `statechange` repaints, so the "tap to enable sound" pill goes away once the
  browser accepts.
- **`focusIsHere` adds `visibilityState !== "hidden"`.** That only takes a claim away. A hidden page no longer
  silences its own alerts or claims focus for other windows. On a desktop a hidden tab already had no focus, so
  nothing changes there.
- **The /m bell escapes everything it draws.** Title, body, count and `data-id` go through `U.esc`. Entries it
  writes into the shared `atrium.toastlog` have the board's shape, with `goTo` and `key` empty. The board's tray
  escapes every field and routes a click through `data-` attributes, so an entry written on one page is safe on
  the other. Card names and the tool name come from the room and are escaped on both sides.
- **What rings.** The first read is a baseline and says nothing. After that, a new permission rings, and so does a
  card newly in `needs-input`, unless it is `isDoer` (`cardrules.js`, which /m loads) or its sheet is open on a
  visible page. Mute is the board's `atrium.sound.muted`, so muting one page mutes both. A `storage` listener
  repaints the other tab.
- **The tones moved without change.** `SOUNDS`, `DEFAULT_PREFS` and `loadPrefs` are byte-identical in `js/sounds.js`,
  which loads before `core.js` on the board and before the /m scripts.

## Findings

### Nit

1. The comment block that explains `inForeground` moved to the end of `sounds.js`, but the function stayed in
   `logs-rules.js`. In `sounds.js` it now describes nothing, and `logs-rules.js` lost it.
2. If the board and /m are open in the same browser, both record the same event into the shared log and both ring.
   The log's repeat bump only catches back-to-back entries with the same text, and the two pages word a wait
   differently. This is rare on a phone.

HUB DEPLOY OK 41b54b50
