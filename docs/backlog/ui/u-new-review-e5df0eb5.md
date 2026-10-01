# Review: no-ready-children e5df0eb5 (@ui)

Range bdbe44eb..e5df0eb5, one commit. Board only (cards.js, settings-spine.js, terminal-list.js, sharing.css) plus
the headless sections `noReadyChildren` and `childUnderParent`. Read against
docs/backlog/ui/u-new-no-ready-while-children-run.md and the test plan in docs/changes/no-ready-children.md.

## What holds

- `hasRunningChild` matches on the bare id, skips archived children, and reads `status === "running"`. The ready
  check and the looks-idle check both leave such a parent out, so the incident (a parent ending its turn on "I'm
  waiting for its done message") no longer rings.
- When the last child ends, the parent re-enters the waiting set with the `waiting_since` it already had. That wait
  never rang, so `check` treats it as fresh and `holdWaits` holds it for the 5 s quiet window. A child's report
  wakes the parent before the child exits (r-exit-on-report exits after the report), so the parent normally leaves
  the waiting set inside that window and nothing rings. A child that ends without a report rings the parent once,
  which is test plan step 2.
- `termNest` puts kid rows in a `.tkids` sibling after the parent's row, not inside it. A kid keeps its own inline
  `onclick`, and a click on it does not reach the parent's. Drag and pin order skip `.kid` rows, so a nested card
  cannot be dropped into the pinned order. Lineage loops and missing parents stay top rows. Totals leave nested
  cards out, so a heading's `7/15` still counts rows drawn.

## Findings

### 1. LOW: the spec and the build disagree on when the alert fires

The spec says "the alert fires on the first turn end with no running children". The build and test plan step 2
ring when the last child ends, on the turn end that happened while it ran. These differ for a parent whose
notices are held (`atrium:orchestrator`, `atrium:hold-notices`). Its child's report does not wake it, so 5 s after
the child ends it rings "is ready. finished its turn" for a turn that ended minutes ago. I think the build's
behavior is the more useful one ("your worker is done" is news), but the spec line should say what was built, or
the alert should say why it rang.

### 2. LOW: a child blocked on a permission or a question counts as not running

`hasRunningChild` reads `running` only. A child at `needs-permission` or `needs-input` lets the parent ring "ready"
beside the child's own alert, so one event costs two alerts, and the parent's is the wrong one. Counting any live,
non-archived child that is not `done` or `dead` matches the spec's "still running" better.

### 3. NIT

`every = cardList()` is computed on each pass, and `hasRunningChild` scans the whole list once per waiting card.
That is O(waiting x cards), which is fine at board sizes. A map by parent id is the fix if it ever is not.

## Verdict

OK, hub-ok and room-ok bdbe44eb..e5df0eb5. Two lows, one nit, no hold.

Quality: after the Sonnet switch, no drop seen. The headless sections cover both rules and the orphan and loop
cases. The lows are the usual second-order kind: the "child is still busy" states other than `running`, and the
held-notices parent that the 5 s hold does not save.
