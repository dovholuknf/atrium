# u-new-quiet-subagents-not-origin

## What changed
- `isSubagent` (tag `atrium:subagent`) added to `internal/api/web/js/cardrules.js`.
- `quietDoer` (`notify.js`) keys on it, so an `origin:agent` resident (director, orchestrator) notifies again. A
  permission still notifies from every card and a card with its own tone still notifies.
- Terminals "subagents" hide toggle, its counts and tooltip, and the phone hide-subagents filter moved to the same tag.
- Settings label is now "don't notify me about subagents", with a hint naming `atrium:subagent`.
- Not done on purpose: retagging the orchestrator card (left in the item for the orchestrator).

## Design notes
Also in the item file under "## Design note". The toggle moved to the one tag as recommended, no reason found against it.
`isDoer` stays for `agentIdle`. The stored setting key `quietDoers` is unchanged.

## Tests
- `HEADLESS_ONLY=quietDoer node scripts/test-board-headless.js`: passes. It covers a resident with `origin:agent` only
  notifying, an `atrium:subagent` card quiet, a subagent permission notifying, and an own-tone card notifying.
- Fixtures for the hide toggle and the phone filter now carry `atrium:subagent`.
- Full run (`node scripts/test-board-headless.js`, after `npm install` and `npx playwright install chromium`): two
  failures, neither from this change. "the rendered board still has native titles" names the Shelf/Ledger/Feed/Requests
  buttons, and `eventDriven` crashes the page. I did not run the suite on the base to confirm they pre-exist.
  The phone section after `eventDriven` did not run because of the crash.

## Pictures
`docs/screens/u-new-quiet-subagents-not-origin/before.png` and `after.png`, the real settings row. Taken with
`QUIET_SHOT=<file> HEADLESS_ONLY=quietDoer`, a hook added to the quietDoer section. The before picture uses the old
`index.html` with the new scripts, which is fine because only the text differs.

## Second pass (director's review)
Sections run on this branch with `HEADLESS_ONLY`, all passing: quietDoer, notifyOff, termBox (hide toggle), gearTermList,
ctxLine, noReadyChildren, mHomeOrder, mHidden, mHomeLive (phone hide-subagents filter), mAgentIdle, mHome, mCard,
termListLastRow, eventDriven, idleBudget, pollsGone, phoneNudge, phoneView, phoneBoardCompact, mPerms, mServe, mGrowl,
growlQuiet, cardUrlNotify, mBell, mOlder.
One failure: `phoneListFit` ("terms: the cards differ in width [341,341,341,341,341,355]"). It fails the same way on the
base commit c721241b, so this branch did not cause it.

## Base comparison (c721241b, separate worktree, since removed)
- "the rendered board still has native titles" fails on the base too, with the same Shelf/Ledger/Feed/Requests buttons.
- `eventDriven` run alone passes on the base and on this branch. In a full run both the base and this branch print the
  eventDriven lines and then stop with no further output and no FAIL line. The two outputs are identical apart from
  timings, so the stop is not from this change. I did not find its cause.
- Net: no failure found that this branch caused.
