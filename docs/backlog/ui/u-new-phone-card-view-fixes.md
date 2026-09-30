# u-new-phone-card-view-fixes: the /m card view, five things clint hit on 2026-09-30

Status: queued for @ui (clint via @runtime, 2026-09-30 evening). Board only unless noted.

1. **The reply box is stale mid-turn.** It refreshes only when the card opens and at turn end (`m/js/card.js:131`,
   "never on a timer"). During a long turn it shows the previous answer. Refresh while the card is running: on the
   card's SSE events, or on the room's `output_at` (@runtime queue item 8, the room-side data this needs).
2. **The thinking badge does not scroll correctly.** Reproduce on the phone at 390 and 412 wide and fix.
3. **The bubbles do not always work.** Which bubbles is not known yet: @runtime asked clint. Ask again if the answer
   has not arrived, and reproduce before fixing.
4. **No upload on the phone.** u-028 (54368c03, in claude/main a06ca6ce) adds it, and the hub has served a06ca6ce
   or later since 18:28. So a phone that shows no upload has an old /m cached. Check that the service worker picks
   up a new board build without the user clearing anything, and fix it if it does not.
5. **The recap goes to the top.** Move it from the bottom of the card view to a "Recap" control at the top, and open
   it as a pop-up sheet on tap, not inline (`recapHTML` in `m/js/card.js`).
