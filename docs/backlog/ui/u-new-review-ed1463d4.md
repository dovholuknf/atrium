# Review: live412-home ed1463d4 (@ui)

Range dafd4a5c..ed1463d4, one commit on sg3/claude/live412-home. The fixes from the Live-412 phone test: 2d hide
subagents keeps live directors, 1e and 6b the sound hint, 3b-4 the card top bar on one line, 6g the docs line under
`room~id`, 7c the filter pill hit area.

The commit is UNSIGNED (`%G?` is `N`). sg3 has no signing key. Noted as @ui asked.

Board checks are @ui's. I read the headless changes and did not run them. @ui reports these green alone: liveHome,
mHome, mHomeOrder, mHomeLive, mHidden, mDocs, mCard, mCardUrl, mPinch, mSwitcher, mCompact, mWorking, mBell,
soundPhone, mGrowl, phoneBoot, bootClean.

## What holds

- 2d: `isSub` keeps any aliased card that is not done or dead, where it used to keep only a running one, and
  `atrium:context-ceiling` joins `NOT_SUB`. mHomeLive's mirror of `isSub` changed the same way. liveHome checks that a
  needs-input director with only the ceiling tag stays, and that an unnamed helper goes.
- 1e and 6b: the hint moves to the bottom edge with `pointer-events: none`. Sound is unlocked by the document-level
  capture listeners in bell.js `init` (pointerdown, pointerup, touchend, click, keydown), not by a handler on the
  hint, so a tap that passes through still unlocks. liveHome checks `elementFromPoint` at the hint's centre.
- 3b-4: the top bar is `nowrap` with the spacer giving way, and the buttons stop at 15px. mSwitcher's pinch check is
  moved to 15 for the pick and term buttons only. liveHome measures the bar at 14, 19.8 and 24px.
- 6g: `filedId` prefixes the card's room, else the page's room, else the only attached room. `mNet.rooms()` returns
  names (store.js:133), so `rooms[0]` is a string. A room's own board has no hub and no rooms, so it stays bare.
  An id that already has a `~` is left alone.
- 7c: the pad is a transparent `::after` on the button, so the hit goes to the button and the look does not change.

## Findings

### 1. NITS

- 7c: the test reads the computed `::after` height. It does not tap 6px off the pill as the test plan says. Wrapped
  pills sit about 34px apart (6px padding twice, a line, a 1px border twice, the gap), so a 40px pad overlaps the next
  line by a pixel or two, and the later pill takes that strip. Harmless, but a tap check would prove what the plan
  claims.
- 7c: the rule sits in phone.css's phone block and so reaches every `.seg` at phone width, dialogs included, not
  only the board's filter rows. It is fine where I looked. Scope it to `:is(#stack, #board-bar)` if a dialog ever packs
  segs tighter.

### 2. OUT OF RANGE, LOW (for @ui's queue)

`internal/api/web/m/js/card.js` lines 891 and 894 read `window.mNet.rooms()` as objects (`r.name`, `rooms()[0].name`).
It returns strings. So on a card URL whose room is not attached, the list shows blank entries linking to
`/m/room/undefined`, and with one room attached `one` is `undefined` and the `would_work` links lose their room.
Not from this commit.

## Verdict

OK hub and room, dafd4a5c..ed1463d4, 2 nits, 1 out-of-range low.

Quality: after the Sonnet switch, no drop seen. Each fix has a check that would fail without it, and the mirrored
`isSub` in the test was kept in step.
