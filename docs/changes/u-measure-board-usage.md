# u-measure-board-usage: count every board control's use

Decisions made unattended, for the morning:

- One delegated listener in `js/usage-board.js` counts clicks (Enter on a button arrives as a click) on buttons, links,
  tabs, pills, menu entries, checkboxes, radios and dialog buttons. A `select` counts when it changes. Typing, the
  terminal (`.xterm`), textareas, scroll and hover are not counted.
- The name is `data-usage`, else `action:<data-action>`, else `view:<data-view>`, else for a control inside a dialog
  `dialog:<title> / <id or label>`, else `id:<id>`, else `label:<text>`. Cut at 200 bytes. The two new menu entries
  carry `data-usage`.
- The day is the room's own local date when the batch arrives, not the browser's.
- Table `board_usage(control, day, n)`, migration `0094_board_usage`, upsert adds the batch count.
- `POST /v1/usage` answers 204 to everything, including a body over 64 KiB, bad JSON, an empty or over-long name and a
  count under 1 or over 100000, and drops what is bad. `GET /v1/usage/board` reads the totals, because `GET /v1/usage`
  is the token usage.
- "Known" controls are those the listener can name in the DOM when the view opens. A control inside a menu or dialog
  that was never rendered is not known, so it cannot be listed as "never" until it has been opened once.
- Before and after: `docs/changes/img/board-usage-menu-before.png`, `board-usage-menu-after.png`,
  `board-usage-view.png`.

## Test plan

## @LETTER@. Board controls are counted

### @LETTER@1. Counted and listed

1. Click the `usage` tab twice and open the `...` menu once.
2. Wait ten seconds, then choose "controls used" in the `...` menu.

**Expected:** the dialog lists the `usage` tab with 2 and today's date above the `...` button, and controls nobody
has clicked sit at the bottom marked "never".

### @LETTER@2. Survives a restart

1. Restart the daemon and open "controls used" again.

**Expected:** the same totals are there.

### @LETTER@3. Typing is not counted

1. Type in the filter box and in a terminal, then open "controls used".

**Expected:** no row appears for the filter box or the terminal.
