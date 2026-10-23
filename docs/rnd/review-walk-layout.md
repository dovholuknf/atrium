# The review walk without a terminal reflow

Status: design, 2026-10-23. Nothing here is built. Read first: `docs/rnd/review-tab-design.md` (the walk drawer,
sections 2 and 5). Code read: `internal/api/web/index.html` (`.term-body`), `internal/api/web/css/walk.css`,
`internal/api/web/js/walk.js` (`dockOpen`, `dockClose`, `walkApplyWidth`, `walkGripDown`),
`internal/api/web/js/terminal-links.js` (`fitTerm`, `onTermResize`, `sendResize`, `termMinCols`, `markWide`),
`internal/api/web/js/fixtures.js` (the `#t-screen` ResizeObserver) and `internal/daemon/supervisor.go`
(`agreedViewport`).

## 0. The answer in one paragraph

The walk drawer is a flex sibling of the terminal, so opening it takes its width out of `#t-screen`. The pane gets
narrower, the fit proposes fewer columns, the board sends the new size to the room, the room resizes the pty, and
Claude Code redraws its whole screen at the new width and shows its resize notice. Closing the drawer does it all again
in reverse. The recommendation is to **size the pty for the pane as if the drawer were shut**, and to put the drawer
where it costs the terminal nothing: in the card list's column when that column is wide enough, and as an overlay
drawer over the terminal's right edge when it is not (section 5).

## 1. Where the walk is laid out

`index.html` holds the drawer inside `.term-body`, beside the one xterm:

```html
<div class="term-body">
  <aside id="walk-drawer" class="walk" hidden> ... </aside>
  <div id="t-screen"></div>
  ...
</div>
```

`css/walk.css` makes that row a flex box. The drawer is `flex: 0 0 auto; width: min(58%, 760px); min-width: 320px`
and the terminal is `flex: 1 1 0`, so the terminal is whatever is left:

```
 before                                     after the walk opens
 ┌list──┬─#t-screen─────────────────────┐   ┌list──┬─.walk──────────┬─#t-screen─────┐
 │      │ 160 cols x 48 rows            │   │      │ rail │ finding │ 100 cols      │
 │      │                               │   │      │      │         │ x 48 rows     │
 └──────┴───────────────────────────────┘   └──────┴──────┴─────────┴───────────────┘
```

`js/walk.js` drives it. `dockOpen` sets `drawer.hidden = false`, applies the remembered width (`walkApplyWidth`,
`localStorage["atrium.walk.w"]`) and calls `onTermResize()` by hand. `dockClose` hides it and calls `onTermResize()`
again. The grip (`walkGripDown`) changes the drawer's width while dragging and calls `onTermResize()` on release.

The phone is different and already does not reflow: `css/phone.css` stacks the drawer above the terminal and slides
the terminal up as a sheet (`walkSheet`), and a phone never votes on the pty's size (`sendResize` returns early).

## 2. Why the terminal reflows

The chain, one step at a time:

1. The drawer appears and `#t-screen` loses its width. The ResizeObserver on `#t-screen` (`js/fixtures.js`) and the
   explicit call in `dockOpen` both run `onTermResize`.
2. `paneBoxUnchanged` sees a new box, so `fitTerm` runs. The fit addon proposes the columns that fit the narrower
   pane. `fitTerm` draws `max(fit, termPtyCols, termMinCols())`, so for a moment the grid holds the pty's width.
3. `termFitCols` changed, so `onTermResize` calls `sendResizeSettled`, and after it settles `sendResize` sends
   `{t: "resize", cols: max(fit, floor)}`.
4. The room takes the focused window's size for the pty (`attach.go`, the `focus` frame, and `agreedViewport` for
   the rest). The pty narrows and the runner gets a window size change.
5. Claude Code redraws at the new width. It reprints its conversation at every width it passes through, which is the
   reason the 120 column floor exists (`termMinCols`). Then the `{t: "size"}` frame comes back, `termPtyCols` drops,
   and the next fit draws the board's grid at the narrow width too.

The floor keeps the width at or above 120 columns, so the screenshot's terminal did not go below the floor, but any
change of width at all is a redraw, a resize notice, and another copy of the conversation in the scrollback. A walk
opened and shut a few times while asking the walker questions leaves several.

Rows do not change, because the drawer sits beside the terminal and not above it. Every layout below only has to keep
the columns still. The height is never touched.

## 3. Three layouts that keep the columns and rows

### 3.1 An overlay drawer

The drawer floats over the terminal instead of beside it. `#t-screen` keeps its whole box, so the fit never moves and
no resize is sent.

```
 ┌list──┬─#t-screen, 160 x 48, unchanged ─────────────┐
 │      │ ● Bash(go test ./...)       ┌─.walk─────────┤
 │      │   ⎿ ok                      │ rail │ finding│
 │      │ ● Reading done. Nothing to  │ 01 ▸ │ LOW    │
 │      │   block on.                 │ 02   │ env... │
 │      │ > _                         │      │ [a][e] │
 └──────┴─────────────────────────────┴──────┴────────┘
          the drawer covers the right part of the terminal
```

- **Which edge.** The right. Claude Code's prose wraps at the pty width, but its tool lines, its prompt and the
  start of every answer are at the left, and the walker's reply to `a ask` is read from its first words. Covering the
  left would hide exactly what the walk sends the operator to read.
- **Seeing under it.** A key (`t`, matching the phone's `▴ terminal` handle) or the head's `×` slides the drawer off
  to a thin tab on the right edge and back. The drawer is translucent only at its edge, never over its body, or the
  findings are unreadable over moving output.
- **Cost.** Small. In `css/walk.css` the drawer becomes `position: absolute; top: 0; right: 0; bottom: 0` inside
  `.term-body` (which gets `position: relative`), with a z-index under the paste box and the held line. In
  `js/walk.js` the three `onTermResize()` calls go, and the grip moves to the drawer's left edge and stops refitting.
  Nothing in `terminal-links.js` changes. A headless check opens and shuts the drawer and asserts no `resize` frame
  was sent.
- **Trade-offs.** The terminal is partly hidden while the walk is open, so following the walker's reply means
  sliding the drawer away or scrolling the drawer narrower. A wide finding (a diff with long lines) is no wider than
  before. Pointer events over the covered part of the terminal go to the drawer, which is right, but a text selection
  started in the terminal and dragged under the drawer stops at its edge.

### 3.2 A separate tab or window

The walk gets its own surface and the terminal is never in the same row as it.

```
 tab strip on the terminal bar             or a popped-out walk window
 ┌──────────────────────────────────────┐  ┌ board window ─────────┐ ┌ walk window ────────┐
 │ [terminal] [walk 0/7]                │  │ list │ terminal       │ │ rail │ finding      │
 ├──────────────────────────────────────┤  │      │ 160 x 48       │ │ 01 ▸ │ LOW env.go   │
 │ rail │ finding                       │  │      │ (unchanged)    │ │ 02   │ [a][e][c][y] │
 │ 01 ▸ │ LOW envelope.go line 129      │  └──────┴────────────────┘ └──────┴──────────────┘
 │ 02   │ ...                           │
 └──────┴───────────────────────────────┘
```

- **The tab.** A `walk` tab beside the terminal in the pane swaps the walk in over the same box. The xterm is kept,
  hidden with `visibility: hidden` and not `display: none`, so its box keeps its size and the fit has nothing to
  react to. (A `display: none` pane measures zero and `fitTerm` returns early, which would also hold, but
  `paneBoxUnchanged` would then see a new box on the way back.)
- **The window.** A `#walk=<card>` route opens the drawer alone, full width, in its own browser window, the way
  `#term=` pops a terminal out today. It holds no xterm, so the rule that a card has one terminal per window is not in
  play, and a second monitor shows the walk and the terminal side by side at full size.
- **Cost.** Medium. The tab needs a tab strip on the terminal bar and the walk's key handling to follow the visible
  tab. The window needs a route in `js/route.js`, a page that boots only the walk, and `walkAsk` to type into the
  card's terminal from a window that has none attached (it would go through the room's input endpoint rather than
  the local socket). Both remove the three `onTermResize()` calls.
- **Trade-offs.** The tab cannot show a finding and the walker's answer at once, and the walk is mostly a loop of
  "ask, read the answer, mark". Every ask becomes two tab switches. The window is the best layout on two monitors and
  the worst on one laptop screen, and it is one more window that must never decide the runner exited (the rule in
  `internal/api/web/AGENTS.md`).

### 3.3 A fixed split the terminal never reflows for

The drawer stays beside the terminal, but the terminal's grid and the pty no longer follow the pane while the walk is
open. Two ways to pay for the drawer's width without the terminal noticing.

**3.3a. The pty is sized for the pane without the drawer.** While `dock.open`, `fitTerm` and `sendResize` measure
`#t-screen` plus the drawer's width, so the board keeps sending the size it sent before the walk opened. The local
grid is then wider than the pane, which is the case the width floor already handles: `markWide` adds `.wide` and the
pane scrolls sideways.

```
 ┌list──┬─.walk──────────┬─#t-screen: a 100 column window onto a 160 column grid ─┐
 │      │ rail │ finding │ ● I found nothing to block on. I could not ru|n →      │
 │      │      │         │   tests: this machine has no go toolchain   |         │
 └──────┴──────┴─────────┴────────────────────────────────────────────────────────┘
                                                      sideways scroll for the rest
```

**3.3b. The walk takes the card list's column.** While the walk is open the drawer replaces `#term-list` in its
column (`termListW`, 260px by default, remembered in `atrium.termlist.w`). The terminal's left edge does not move, so
its box is the same box.

```
 ┌─.walk, in the list's column ─┬─#t-screen, 160 x 48, unchanged ─────────────┐
 │ rail │ finding               │ ● Reading done. Nothing to block on.        │
 │ 01 ▸ │ LOW envelope.go:129   │ > _                                         │
 └──────┴───────────────────────┴─────────────────────────────────────────────┘
```

- **Cost.** 3.3a is the smallest change in this document: a width term added in two places in
  `terminal-links.js`, gated on the drawer being open, and the three `onTermResize()` calls in `walk.js` kept (they
  now find nothing to send). 3.3b is small in CSS (the list hidden, the drawer placed in its column at its width) and
  needs the switcher reachable another way while walking, because the list is the switcher.
- **Trade-offs.** 3.3a hides the right part of the terminal behind a sideways scroll, which is the overlay's cost in
  a less obvious form, and a sideways scroll in a terminal is easy to miss. It does keep every character reachable,
  where the overlay covers them. 3.3b hides nothing in the terminal, but the drawer is as narrow as the list. At the
  default 260px it is under the drawer's 320px minimum and the rail folds to numbers, and widening the list to fit it
  is itself a terminal resize. The card list is gone while walking, which costs little because a walk is on one card.

## 4. Side by side

| | overlay (3.1) | tab or window (3.2) | pty held (3.3a) | list column (3.3b) |
| --- | --- | --- | --- | --- |
| terminal columns change | no | no | no | no |
| finding and reply both visible | partly, reply's left side | window only | partly, by scrolling | yes |
| works in a popped-out `#term=` | yes | yes | yes | no list there |
| cost | small | medium | smallest | small |
| new failure mode | text under the drawer | two surfaces in step | missed sideways scroll | narrow drawer |

## 5. Recommendation

Do two things, in this order.

**First, 3.3a as the rule, whatever the layout.** The pty's size should never be a function of whether a side panel
is open. Making `fitTerm` and `sendResize` measure the pane plus any open drawer is one place to say it, and it ends
the reflow today with the drawer exactly where it is. It also protects the next panel that sits beside the terminal.

**Then, 3.1 the overlay on the right edge, with 3.3b when the list is wide enough.** With the pty held, the remaining
question is only what the operator sees. The overlay keeps the terminal's left side, where the walker's answer
starts, in view without a scroll, and a key slides it away. When the list column is at least the drawer's 320px
minimum, opening the walk into the list's column instead is the better picture because nothing is covered, and the
overlay is the fallback for a narrow list and for a popped-out terminal, which has no list.

The separate tab is not recommended, because every ask becomes two switches. A popped-out walk window is worth
building later for two-monitor use, on top of this and not instead of it.
