# Review of fix-term-box-low bae7d3f3 (@ui, the terminals list tray restored in the box)

Range `c5023d7a..bae7d3f3`, one commit on claude/fix-term-box-low. Board only: `js/terminal-list.js`,
`js/settings.js`, `css/terminal.css`, the `termBox` headless section, a changelog file and a test-plan fragment.
Order: urgent from clint, via @ui. It replaces the hide pills of 0440e4dd (u-new-review-0440e4dd.md) with the tray
911cb3be removed. The base c5023d7a is the fix-term-box landing, and I checked that it carries only 0440e4dd.

## Against the ask

- **It is the old tray.** `TERM_TRAY_KEY`, `termTrayOpen`, `toggleTermTray`, `TRAY_GROUP_WORDS`, `termTraySummary` and
  the `termTrayHTML` markup match 911cb3be^ line for line. The only differences are that the sort pair now comes
  from `termSortHTML()`, which the gear shares, and that the cache line (`#cache-line-terms`) is not restored. The
  cache summary stays in the gear (`cache-line-gear`). Nobody asked for it back.
- **@ui's question: folded by default is right.** Before 911cb3be, `termTrayOpen` returns true only when the per-device
  key reads `"open"` (911cb3be^ terminal-list.js:246), and its comment says "defaults to folded". The old screenshot
  shows a device that had opened it.
- **One state, two places.** The tray and the gear draw `termSortHTML`, `termHideControlsHTML` and `paintGroupSegs`
  with the same handlers (`setTermSort`, `toggleHideAgents`, `toggleHideSubagents`, `setGroupMode`), and each of
  those re-renders the list, which repaints both. `paintGroupSegs` gains `term-group-tray`, a new id, so nothing
  collides with the gear's `term-group`. The group modes are unchanged since 911cb3be (five modes and off), so
  `TRAY_GROUP_WORDS` still covers them.
- **The 150px low is closed.** The summary is a `flex: 1 1 auto; min-width: 0` toggle whose text wraps
  (`overflow-wrap: anywhere`). The width buttons are `flex: none`. The pills sit in `1fr` grid cells with
  `min-width: 0` and ellipsis on the button, so a cell can shrink below its words. The termBox case opens the tray,
  measures at 150px (and refuses to pass if the list is wider than 160px), and fails on any pill outside the tray, the
  summary over the arrow, or the arrow outside the box.
- **Folded, the body is `inert`** and its row is `0fr`, so the controls are out of the tab order while hidden.
  Names-only mode hides the toggle and the body, and keeps the width buttons.
- **Stale comments fixed.** `termHideControlsHTML`, the block comment above the tray, and the `.termtray` CSS comment
  now name both places.

## Findings

1. **Medium. The full board run goes red.** The tray-shut block in `main()` (scripts/test-board-headless.js:17738 at
   bae7d3f3) still asserts the tray is gone. It counts
   `.traysum, .traytoggle, .traybody, .cacheline, .trayseg, #term-group, .trayrow` in `#term-list` and fails with "the
   list still carries the sort, hide, group or cache controls". The restored tray draws every one of those classes
   except `.cacheline` and `#term-group`, folded too. So the whole-board run fails at bae7d3f3. Running termBox alone
   passes. Fix: replace the leftovers check with what is now true (folded by default:
   `.termtray:not(.open)`, a `.traysum` that starts with "sorted by", `.traybody[inert]`), or delete it, and update
   that block's first comment line ("The sort/hide/group controls are a panel of their own ... the default"), which
   is right again. Then run the whole board once, not only the sections.
2. **Nit.** No case pins the default: every termBox set writes `"open"` first. One assertion that a cleared key draws
   `.termtray:not(.open)` with an `inert` body would hold clint's answer in place. The fix for 1 can be that assertion.

Quality: after the Sonnet switch, the same pattern: the product code is a careful restore, and the named case and the
low are both covered, but the sibling check written for the removal was missed, and only the sections were run.

**HOLD c5023d7a..bae7d3f3.** The board code itself would be HUB and ROOM OK. The hold is for the red suite only. A
re-read of `c5023d7a..<tip>` needs only the test change.
