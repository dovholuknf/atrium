# Review of 8333259b (card URLs U1 and U2, readable card addresses on the board and the phone)

Reviewed by @review, 2026-09-30, from reading `git diff ffa8b319 8333259b` (m1mini/claude/u-card-urls). Board files
only, a hub-only deploy. The headless suite is @ui's and was not run here.

## What holds

- **The 404 and 409 bodies cannot inject anything.** Every word `cardurl.js` draws from `would_work`, `candidates`,
  a task's recap or its activity goes in through `textContent` (`cardUrlBox`, `cardUrlLine`, `cardUrlSay`,
  `cardUrlNotice`). Every `href` is built as `"/alias/" + encodeURIComponent(...)`, `"/room/" + enc + "/" + enc` or
  `"/#term=" + enc`. `encodeURIComponent` encodes `/`, so none can become `//host` or a `javascript:` link.
- **The lookup is one GET per open**, through `soloFetchCard(path)`, with the same waits as an id. A name like `..`
  only makes the browser fetch a different `/v1/` GET, and that does nothing.
- **Old links keep working.** `#term=` wins over a path in `termOnly`, `inPopout` and `bootTerminalOnly`.
- **The service worker** matches a window by `#term=` first, then by path only for a path shaped like a card, so a
  `/room/<room>` board is still a board (`sw.js` `isSolo`).
- **The last id per path** is written only after a card is open, and a changed card is said, with a link to the old
  one by `#term=`.
- **`/room/<room>`** writes `atrium.room` before `startRooms`, as the design says. A link can change the stored scope
  for every tab in that browser. That is what the address means, and picking a room in the header undoes it.

## Findings

### Medium

1. **A done card's readable path opens the live card that took its alias on the same room.** `cardUrlPath`
   (`internal/api/web/js/cardurl.js:52-62`) sees the clash and falls back to `/room/<room>/<name>`, but takes
   `name = alias || handle`, so the fallback is `/room/<room>/<alias>`. The hub resolves `alias@room` by
   `matchCard` (`internal/link/resolve.go:65-99`): wire name first, then alias, and a live card beats a done one
   (`aliasBeats`). So that path reaches the live card, not the done one. Where it shows:
   - `popOutTask` opens the pop-out on that path (`solo.js:251`), so popping out the done card shows the live one.
   - `soloSwitch` writes it with `replaceState` (`switcher.js:365`), so a window switched to the done card shows
     the live one after its next reload, and the hub restarts the board on every build.
   - The notification's `path` in `showNotification` goes to the same place.

   Exposure: every worker relaunched under the alias of one that finished, on the same room. That is the usual way
   an item's worker is replaced. Suggested fix: in the fallback, prefer the handle, `const name = handle || alias`.
   The wire name is unique, and `matchCard`'s second loop resolves it bare. Also stop `cardUrlName` lowercasing a
   handle: `matchCard` compares the wire name case-sensitively, so a mixed-case wire name would miss and drop to
   the alias again. The same applies to `cardUrlShape` for the `card` kind, which lowercases the name read from the
   address.

### Low

1. **A pop-out that reloads onto a different card keeps its old window name.** `bootTerminalOnly` resolves the
   path again on every reload, and it is meant to follow the alias. But `window.name` stays `atrium-term-<old id>`,
   because only `soloSwitch` sets it (`switcher.js:367`). The board then finds that window when it asks for the old
   card, and opens a second one for the new card. Set `window.name` from `want` in `bootTerminalOnly` when the card
   came from the path.
2. **The service worker compares paths exactly** (`sw.js` `pathOf(c.url) === path`). A window opened at
   `/alias/Foo` or `/alias/foo/` is not matched against `/alias/foo`, so the click goes to the board instead of
   raising that window. Normalise both sides the way `cardUrlShape` does.

## Verdict

**HOLD 8333259b** on medium 1. It is a small change to `cardUrlPath`, and after it this is **HUB DEPLOY OK** on
review grounds. The lows can follow.
