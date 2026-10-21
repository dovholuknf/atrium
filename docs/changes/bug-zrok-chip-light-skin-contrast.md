# The transport chip reads on every skin

Before and after, per skin: `docs/backlog/ui/bug-zrok-chip-light-skin-contrast/before` and `after`.

## What changed

`css/cards.css`: the chip's text is `color-mix(in srgb, rgb(var(--tp-rgb)) 40%, var(--head))`. The per-skin deep triples
and the list of five skins are gone. The list had `mint`, a dark skin, so mint wore the dark triple on a dark board
(1.9:1 to 2.4:1), and the four light skins' zrok and mTLS text sat at 3.7:1 to 4.0:1.

## Test plan

- `HEADLESS_UNITS=transportChips node scripts/test-board-headless.js` wears every skin found in `css/themes.css` and
  fails when any non-fallback chip is under 4.5:1 against the page it sits on, or when fewer than four light skins turn
  up. `TRANSPORT_SHOTS=dir` writes one PNG per skin.
- `node scripts/check-contrast.js internal/api/web/css` and `bash scripts/check-skins.sh` stay green.
- By eye: the Rooms tab on paper, daylight, linen, frost and mint, with a ziti, an mTLS and a zrok room.
