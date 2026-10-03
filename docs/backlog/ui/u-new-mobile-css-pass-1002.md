# Mobile CSS check for everything new since the last mobile pass

Asked by clint, 2026-10-02, by the orchestrator. Queued after the context-bar badge and the usage chart land.

Check phone (390) and narrow (about 900) layouts, paper and dark, for what is new since the last mobile pass: keep-alive (off on phone, the gear field and hint), the context-bar land-the-plane badge (the phone row "LAND 222k", the narrow terminals list "LAND"), the usage chart (axis labels, percent scale, touch-drag readout), and the repos tab and scm views as they land (shelf, ledger, feed; the known repos lows: the ledger strip edge at 390, the empty room on operator pushes).

Fix only what is broken or missing. If nothing needs it, say so in one line. Headless sections at the phone width where the layout is testable; screenshots as file paths; through @review.

## Done

Checked at 390, 360 and 768, paper and dark; shots in /tmp/u-mobile-pass/ (before-/after- prefixes).

- Repos ledger: the repo strip cut a repo mid-word at the right edge at 390/360. It now snaps and fades at the edge
  (hubrepos.css, under 900px). Assert: the strip has the fade and snap at 390; mutant fails.
- Repos shelf and feed: nothing needed (no sideways scroll, one column at 390).
- Usage chart: nothing needed (headline strip on phones, no sideways scroll, touch scrub readout; the strip is hidden at 768 where the hover readout takes over).
- /m bubbles, list idle/waiting labels: nothing needed at 360 and 768; no sideways scroll.
- LAND badge: nothing needed (already asserted inside its row at 340px). Keep-alive chip and gear field: nothing needed
  (already asserted at 390 with no run-off).
- Not changed: the empty room on operator pushes.
