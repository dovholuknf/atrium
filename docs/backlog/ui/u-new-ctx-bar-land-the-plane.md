# u-new-ctx-bar-land-the-plane: the context bar must shout at the land-the-plane point

clint (2026-10-02, via the orchestrator, with a screenshot of the orchestrator row). The status line (the operator's
status line script) says "LAND THE PLANE 201461/1000000 (20%)": the zones are
ABSOLUTE token counts, up to 150k sweet, 150k-200k getting full, past 200k LAND THE PLANE, whatever the window is.
The board's row bar for that card sits at about 90% (it already scales to the autocompact window, limit + 10%, not
to 1M) but shows no badge, and a bar at 90% looks like a thin pink line. Only the status line text says 20%, against
the 1M window. The 200k scale idea is dropped.

Asks (batch after keep-alive u-switch-latency; through @review):
1. Past the land-the-plane point the row gets a red, unmissable badge. Today it gets none. Check why the existing
   "limit" chip (u-ctx-bar, ctxLine in js/board.js, ctxMeter in js/peek.js) did not show on that card: is the
   threshold the card carries (context_size.threshold_k, peekThresholdK) different from the 200k the status line
   uses, or is the card not flagged at/over its own limit? Say which, with the row's real context_size.
2. A bar at 90% must look nearly full and alarming: thicker where it fits, the colour shifting to red near the
   end (not a thin pink line), still one pulse at the limit, no looping animation. Same drawing for the popover meter.
3. A tooltip on the bar: tokens / limit and the window, e.g. "201k of 200k (land the plane), window 1M".
Check whether the land-the-plane figure should be read from the same place as the status line or stay the board's
own context_threshold_k setting; do not hard-code a second 200k without saying so.
