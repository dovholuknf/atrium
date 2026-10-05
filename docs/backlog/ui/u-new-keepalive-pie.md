# u-new-keepalive-pie. The cache keepalive chip is a pie that fills in over time

Status: done on claude/u-new-keepalive-pie. Owned by @ui. Filed by the orchestrator 2026-09-30 on clint's word: "is it possible for the
cache thingy to be like a pie that gets filled in over time so i can see when it'll activate again".

## Wanted

The keepalive chip on a card (the prompt cache warmer, `keepalive` on the task view: `state`, `state_at`,
`refreshes`, `warm_until`) draws as a small pie instead of a word. The pie fills from `state_at` (or the last refresh)
toward the next refresh, so a glance says how long until it fires again. When the cache would go cold before the
next refresh, the pie says so.

- One look, no hover (the u-032 rule). A tooltip can carry the exact times.
- Each state gets its own look: `on` (filling), `stopped:break-even` and other stopped states (empty or struck
  through, with the reason in the tooltip), and warm versus cold.
- Redraw from the clock in the browser. Do not add an SSE event for every tick.

## Open for the design

- What the room knows about the next refresh time. If the view carries only `warm_until`, the pie may need a
  `next_refresh_at` from the room (a room-side field, @runtime).
- Where it sits on the card face and on the phone layout.

## Design note

- The view already carries what the pie needs: `last_refresh_at`, `next_refresh_at` (sent while the card is just
  waiting for its time) and `warm_until` (r-032). No room change was needed.
- The wedge runs from `last_refresh_at`, or from `warm_until` minus an hour when there was no refresh yet, to
  `next_refresh_at`. It is drawn in 15 degree steps from a `--pie` custom property and repainted by the existing
  keepalive timer, which now also wakes once a minute while a pie is filling. Past 92% the ring turns amber and the
  tooltip says "about to refresh".
- A warm card with no `next_refresh_at` (busy, small context and so on) keeps the old solid or ringed dot, since
  there is no refresh to fill toward. A cold card stays solid blue-grey. Stopped is an empty amber ring with a strike.
- Place: unchanged. The chip stays where the dot was, in the chip row of the board card, stack row, terminals list,
  terminal header and phone tray. It grew from 10px to 14px, which still fits the phone rows with no overflow.
