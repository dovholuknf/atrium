# Review of d4868e18 (@ui u-gear-list: gear pills repaint at once, the hub hosts row)

Reviewed by @review, 2026-09-30, from `git diff claude/main...d4868e18`, read only. Hub side. Board and headless
checks are @ui's, and I ran none.

## What holds

- **The pill fix.** `toggleTermSort` and `setHideMode` call `paintTermGear()` (terminal-list.js) before
  `renderTermList`, so the gear's pills follow the click in the same frame instead of after the card fetch. The
  tests now read `#gear-term-hide`, where the pills have lived since 911cb3be. No assertion is weakened.
- **The hosts row draws with `textContent` only.** That covers names, the reasons a name is ignored, `$ATRIUM_HOSTS`
  entries and the hub's refusal text. A name like `<img onerror>` stays text. Each remove button closes over its own
  name and puts no string into a handler.
- **It follows the server's rules and does not repeat them.** The board sends the list as typed, and `serveHosts`
  applies the public-suffix rule and the bounds (df724652). An ignored entry shows the hub's reason. Since LP1
  (465a2c30) the PUT needs `edge.LocalOperator`, so over a share it gets 403, and the row turns read-only with
  "Set on the hub's machine". A guest never fetches it, and any non-ok GET hides the row.
- Loaded on gear open and on stream reopen, with no timer.

## Findings

### Nit

1. Over a share the row shows as editable until the first PUT comes back 403. The GET does not say whether this
   caller may write, so the first add over zrok always fails before the row says why. A `writable` field on the GET,
   computed with `edge.LocalOperator`, would let the row open read-only. That needs a Go change from @runtime.
2. A PUT replaces the whole list from this tab's copy. Two tabs that edit at once lose one edit. Rare, and nothing
   breaks, since a later GET shows what was kept.

HUB DEPLOY OK d4868e18
