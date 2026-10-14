# Phone board: delay between the thinking indicator and the response showing up

From clint, 2026-10-13, during the unified-card interview: "On Mobile there is a substantial delay now between
thinking and my response showing up." Filed by the minion. Diagnose first, no build yet.

## Symptom

On /m the card shows it is thinking, the agent's reply is already written, and the reply takes a noticeable time to
appear. "Now" suggests it is new, so a recent change is a suspect.

## Diagnose

1. Reproduce on a phone or a 390px headless page: send a say to a card, time thinking start, the reply event on the
   room, the reply in the DOM.
2. Split the delay: room to hub, hub to the board's event stream or poll, the poll interval, the repaint.
3. Check what changed on the phone path recently: the notify and seen handling (notify.js, seen.js), the /m
   keepalive and polling (m/js/), and the repaint throttles.
4. Check whether the reply waits on the next poll rather than an event, and whether a backgrounded tab throttles it.

## Done when

The cause is named with a measurement, and a fix is proposed as its own item. Headless test for the fix to follow.
