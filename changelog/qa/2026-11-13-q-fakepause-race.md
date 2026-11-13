# fakePause no longer races the page clock

`fakePause` in `scripts/lib/browser.js` threw "the page clock outran every margin" on GitHub macOS runners
(pasteTwice, pasteSpinner), even with the round-trip margin.

Cause: Playwright's `clock.pauseAt` first awaits `addInitScript` (a browser round trip) and only then evaluates the pause
in the page. The margin was sized from a plain `evaluate` round trip, so a slow `addInitScript` left the target past.

Fix: the page's own clock controller freezes the clock at its own `now()` in one evaluate, which cannot be past.
`clock.pauseAt` then gets that frozen time, so the pause is still logged for a navigation. No margin, no retries.
