# u-011 interview: one filter drawer for stack, board and terminals

Interview with clint, 2026-10-12. The question-by-question record, his answers verbatim, is `INTERVIEW.md` at the root
of the `claude/interview-board-controls` branch. Reference shape: the stack tab's current control (SHOW chips with
counts, a search box, SORT BY, GROUP), screenshot `.atrium/incoming/20261012-113136-pasted.png`.

Note: `docs/backlog/ui/u-011.md` in this repository is an older item (usage tab cache reads). This interview is for the
backlog store's u-011, the shared board control.

## Which tabs

**stack**, **board** and **terminals**. The control appears per tab, and only where it is useful. Other tabs (perms,
history) keep their own bars. Already settled before the interview: group, sort and show stay per tab. Each tab keeps
its own view, stored per device in localStorage as today.

## The shape

Stack's existing widget is used on all three tabs: SHOW chips with counts, a search box, SORT BY, GROUP. It lives in a
drawer that **rolls down from the top, just under the title (tab) bar**, over the content below. It is an overlay, so it
never resizes the list or a terminal.

### Collapsed line

- One line under the title bar. It reads the current settings, e.g. `ready 34 · name ↑ · by age`, with a `filters`
  handle at the right that shows how many settings differ from their defaults.
- **The search box stays on the collapsed line.** It takes typing without opening the drawer.
- A click anywhere else on the line opens the drawer.
- On a tab without search the line shows only the summary, e.g. board's `by project · last active`. Board does gain
  the search box, because it is stack's widget (see below).

### Open and close

- Only a click (desktop) or a tap/pull (phone) opens it. **No key opens the drawer.**
- A click inside the drawer (chip, sort, group) applies at once and leaves it open.
- A click outside closes it, and that click is **swallowed**. It does not also act on what it landed on.
- Esc closes it and returns focus to where it was.
- The handle at the drawer's bottom edge slides it back up.

### Keyboard

- **ctrl-/** focuses the collapsed line's search box. The drawer stays closed. A bare `/` is too common a key.
- In the search box, Esc clears the text, and a second Esc leaves the box.
- In an open drawer, Tab/Shift-Tab move between rows, arrow keys move along a row, and Enter/Space press a chip.
- ctrl-/ does nothing while a terminal has focus, so it never steals keys from xterm.

### Phone (mobile-native first)

- A pull-down from the top: the same gesture the terminal's title-bar tray already uses on a phone.
- The drag **starts only on the collapsed line**, never on the list. The drawer follows your finger. Released past about
  a third of its height it snaps open; short of that it snaps back.
- A plain tap on the line also opens it. A tap on the search box only types.
- A swipe up on the drawer, or a swallowed tap outside it, closes it.
- Pull-to-refresh is off on that strip (`overscroll-behavior`).
- On a narrow screen the collapsed line is the search box plus a `filters N` handle, and the summary is dropped if it
  does not fit. The open drawer is capped at about 70% of the screen height and scrolls inside.
- Each tab has its own drawer. This replaces today's shared `body.filters-open` flag and `phone-filters.js` button.

## What SHOW means on each tab

Same widget, with the chips that matter on that tab. Every chip carries its count. The attached card always stays,
whatever is off.

### stack (interviewer's recommendation, accepted by clint)

Unchanged chips and default: `all · ready · running · finished · shelved`. They add up; ready alone is the default;
turning off the last chip shows everything. It gains the same `subagents` chip, off by default: off, a subagent is not a
row of its own, its parent's row shows a `3 subagents` badge, and a subagent that needs you still shows.

### board

- One chip per column, `inbox · needs permission · ready · running · finished · shelved`, with counts. All are on by
  default. Turning one off removes that column and gives its width to the others. Turning off the last chip brings
  every column back.
- A `subagents` chip, off by default. Off, a subagent is not drawn as its own card, its parent's card shows a
  `3 subagents` badge, and a subagent that needs you still shows.

### terminals (replaces the "mid" hide)

- `running` and `ready` sessions are always listed. They get no chips.
- **`shelved N`** and **`dead N`** chips, both off by default. They are there for resuming. Dead means exited or stopped.
- A **`subagents`** chip, off by default. Off, every parent row starts **folded** under the existing `termNest`
  nesting, with a count on its chevron (`▸ 3`), and a child that needs you still shows through the fold. On, parents
  start expanded.
- Removed: the `hide inactive` agents/subagents pair (`atrium.hideagents`, `atrium.hidesubagents`), `show stopped`, and
  the copy of these controls in the gear's `terminal list` section. The new keys replace them; the old keys are cleared
  on load, as `clearLegacyHideKey` does today.

## Defaults and persistence (interviewer's recommendation, accepted by clint)

- The drawer **starts closed** on every load and every tab switch. Its open state is not remembered.
  `atrium.termtray` is retired.
- Chips, search text, sort and group are per tab and per device in localStorage, like `atrium.stack.show` today. Search
  text is per tab and kept for the session only (sessionStorage).
- The `filters N` count on the handle counts settings that differ from that tab's defaults above.

## The test that proves it (interviewer's recommendation, accepted by clint)

One Playwright check, `scripts/check-board-drawer.js`, run from `check-board.sh`, at 1280px and at 390px, on each of
the three tabs:

1. Load the tab with no stored state. The drawer is closed; the collapsed line shows the summary and the search box;
   the defaults above are in force (e.g. terminals has no shelved or dead rows and parents folded).
2. Press ctrl-/ with nothing focused. The search box has focus and the drawer is still closed. Type, and the list
   narrows.
3. Click the collapsed line outside the search box. The drawer is open, over the list, and the list's box did not move
   or resize.
4. Click a chip. It applies, its count is right, the drawer stays open, and the handle's `filters N` went up by one.
5. Click a card row below the drawer. The drawer closes and the card did **not** open.
6. Open it again and press Esc. It closes, and focus is back where it was.
7. Change sort on stack, switch to board and back. Each tab kept its own sort, show and group.
8. At 390px with touch: drag the collapsed line down by a quarter of the drawer's height and release (it snaps back),
   then by half (it opens). Swipe up on it (it closes). A drag that starts on the list scrolls and never opens the
   drawer.
9. With a terminal focused on terminals, ctrl-/ goes to the terminal, not the search box.

## Interviewer's call, not asked

On terminals the drawer spans the full width under the title bar, over the session list and the terminal pane both. It
is an overlay, so the terminal is never reflowed. Revisit it if covering the top of the terminal gets in the way.
