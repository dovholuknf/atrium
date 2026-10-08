# u-dropdowns-instant

Cause: `openLaunch` awaited `/v1/harnesses` and `openPickRepo` awaited `/v1/providers` on every open. Fixed by reading
both once in `bootBoard` and filling from held state. The audit and usage room selects now rebuild only when their set
changes and not while focused. Rooms were already held from the stream. Phone selects have no fetch.

Test: `HEADLESS_ONLY=dropdownsInstant node scripts/test-board-headless.js` (also registered as a unit after bootClean).
It holds every /v1 answer 500ms, asserts the dialog is open and both selects filled the frame after the call, and that
nothing is rebuilt for 1.2s. Before the fix it fails four assertions, after it passes. bootClean passes.

Shots (native select popups do not render headless, so the room select is focused): before
docs/changes/u-dropdowns-instant/before.png, after docs/changes/u-dropdowns-instant/after.png.
