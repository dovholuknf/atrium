# u-new-card-urls. A card has a readable URL that can be bookmarked, and shares keep it

Status: not started. Owned by @ui, with @runtime for the routes. Design through @rnd, together with
r-new-handle-addressed-http, because both need one resolver from a readable name to a card. Filed by the orchestrator
2026-09-30 on clint's word:

> i'd also like a nicer url for cards so that if i go to
> http://localhost:7778/#term=sg4-control~01a0f2e3-6732-785b-8888-708ce20d917b i'd rather go to
> http://localhost:7778/room/sg4/sg4-control something nice that i can bookmark. i'd also like zrok shares and
> openziti shares to take this sort of thing into consideration

## Wanted

- A path URL per card built from names, not ids: the room, then the card's alias (or its handle when it has none).
  The exact shape is an open question below. The board serves the same page on that path and opens the card's
  terminal, the way `#term=` does today.
- It survives the card being relaunched or moved: a bookmark names the work, not one card id. When the name now
  resolves to a different card, or to nothing, the board says so rather than showing an empty terminal.
- `#term=<room~id>` keeps working, so existing links do not break.
- zrok and OpenZiti shares serve the same paths, so a bookmark made on the share opens the same card. That includes
  lending one session (`docs/overlays.md`): a lent session's URL can be the readable one, and the guest allowlist in
  `overlay_guest.go` has to allow the new route for that card only, never as a way to name another card.
- The phone board (`/m`) gets the same URLs.

## Open for the design

- The shape. clint's example is `/room/sg4/sg4-control`. Is that `/room/<machine>/<room>` for a room, with
  `/room/<machine>/<room>/<alias>` for a card, or `/room/<room>/<alias>`?
- Two cards with the same alias across time (a relaunch leaves the old one done): the live one wins, then the most
  recent.
