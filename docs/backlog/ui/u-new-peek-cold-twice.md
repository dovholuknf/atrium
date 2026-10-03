# u-new-peek-cold-twice: the card hover popover states the cold cache twice

Filed from the desktop board, 2026-10-02. Low priority; build after the current items.

## What and why

The card hover popover shows the cold-cache line twice: once at the top under the path, and again at the bottom as
`cache: cold since ...` (`peekCache` in keepalive.js). The top line appears after the popover opens, so the card
jumps when it arrives.

## What to change

Drop the top line and keep the bottom `cache:` line. Check the popover does not change height when the cache state
arrives (the bottom line should have its place reserved or be present from the first paint).

## Done when

- The cold cache is stated once in the popover, at the bottom.
- Opening the popover on a cold card does not move anything.
- A headless check covers the single line.
