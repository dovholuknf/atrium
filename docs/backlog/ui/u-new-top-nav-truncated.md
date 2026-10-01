# u-new-top-nav-truncated: the top nav tabs are cut off

Filed by the orchestrator, 2026-10-01, from clint's screenshot `.atrium/incoming/20261001-125026-pasted.png`.

## What clint sees

A ~1760px wide browser window. The header row holds ATRIUM, the tabs stack, board, terminals (14), history, then
"usa" cut mid-word, and every tab after it (usage, audit, ...) is gone. The rest of the row is taken by the
chips: "5 WORKING", "APPROVING EVERYTHING", "+ new agent", the bell (99+), the sound note, the gear, "3/5 rooms",
"not ready: 4 of 11 commit..." (itself truncated). clint: "notice how the stack--board-->audit is all truncated and
shitty".

## The rule

Every nav tab is always reachable and never cut mid-word. When the row is short of room, the chips give way first
(compact form, icon only, or into an overflow), and the tabs never clip. Below some width the tabs themselves go to
an overflow menu, whole tabs only. The "not ready" chip shows its whole text or a short form, not an ellipsis.

## Acceptance

- Headless at 1760, 1440, 1280 and 1024 px wide with every chip shown: every tab label fully visible or in an
  overflow menu, none clipped. Screenshot each width for clint.
- Phone layout unchanged.

## Owner

@ui.
