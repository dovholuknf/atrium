# u-new-keepalive-pie. The cache keepalive chip is a pie that fills in over time

Status: not started. Owned by @ui. Filed by the orchestrator 2026-09-30 on clint's word: "is it possible for the
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
