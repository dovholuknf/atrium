# Review of fix-term-box 0440e4dd (@ui, the terminals list box that drew only the << button)

Range `d94090f6..0440e4dd`, one commit on claude/fix-term-box. Board only: `internal/api/web/js/terminal-list.js`,
`css/terminal.css`, the `termBox` headless section, a changelog file and a test-plan fragment. Order: clint's
screenshot, via the orchestrator, ahead of u-new-details-debug-section.

## Against the report

- **The cause is right.** Since 911cb3be the sort, hide and group controls live in the gear, and `termTrayHTML`
  held only `termListButtons()`. At full width that is one `<<`, so the box above PINNED was a frame with one arrow
  in it. Not a regression in the sense of a broken path: the box was left with nothing to say.
- **The fix reuses the control.** `termHideControlsHTML` takes an optional class, and `termTrayHTML(hideCounts)`
  draws it with `seg termhide tbarhide` ahead of the width buttons. The counts are the same object
  `paintTermGear(hideCounts)` gets in the same render, so the box and the gear cannot disagree. The click handlers
  are the same globals (`toggleHideAgents()`, `toggleHideSubagents()`), which re-render both.
- **The box is drawn whenever there are rows to hide from.** It sits inside the `tasks.length` branch, and `tasks`
  is the set BEFORE hiding. So with every row hidden the box and its lit pill stay, and the way back is on screen.
  With no tasks at all the empty-state panel is drawn, and there is nothing to hide.
- **Names-only mode drops it.** `.tl-mini .tbarhide { display: none }`. `tl-mini` is never set on a phone
  (`applyTermList`), so on a phone the pills show, which is also where the box used to be empty (phone.css hides
  `.tlcycle`).
- **No selector collision.** The box's pair carries `termhide` but not `trayseg`, so the old "no tray leftovers"
  check (it counts `.trayseg` in the list) still holds. Every existing `.termhide` query is scoped to
  `#gear-term-hide`.
- **The termBox case discriminates.** It renders hide off and on across supervised only, joined unpinned, some
  hidden, and all hidden pinned, and requires two pills in the box, `agents (2)` with both pinned rows hidden, and a
  lit pill. On d94090f6 the bar holds no `.tbarhide` at all, so every set fails. I read the case and did not run it,
  since board checks are @ui's.

## Findings

1. **Low.** Full mode's width can be dragged down to `TERMLIST_MIN` (150px). The box loses 8px to margin and border
   and 4px to the bar's right padding, and holds 6px of left padding, the two pills (3px 8px padding, `nowrap`), a
   4px gap, the bar's gap and the `<<` button. By estimate that needs about 165px with no counts and about 200px
   with both lit and counted, so near the low end of the drag the pills overrun. `.tbarhide` has `min-width: 0`,
   which lets the container shrink, but its `nowrap` buttons do not, so they spill out of it under or over the `<<`
   button rather than clipping. Not measured. Fix: let the pair clip (`overflow: hidden` on `.tbarhide`), or drop
   the counts below some width, and add a termBox case at `TERMLIST_MIN` that checks the pill's right edge sits left
   of the `<<` button's left edge.
2. **Nit, stale comments.** `termHideControlsHTML`'s header still says it "lives in the gear's `terminal list`
   section". The block above `setTermSort` still says "What stays is the bar with the list's width buttons". The
   CSS comment on `.termtray` still says "the width buttons. The sort, hide and group controls are in the gear".
   Each should name the hide pair in the box too.

Quality: after the Sonnet switch, no drop. The diagnosis went back to the commit that emptied the box, and the fix
reuses the one control and its counts instead of drawing a second. The narrow end of the drag was not considered.

**HUB DEPLOY OK and ROOM DEPLOY OK d94090f6..0440e4dd.** Finding 1 is cosmetic and only shows at a list dragged
near its minimum. It does not block.
