# u-new-m-compact: /m takes less vertical space, and pinch sizes the text

From clint by the orchestrator, 2026-09-30 evening, on the phone. One item, after u-m-card and u-m-home land.

## What

1. **One-line message header.** The speaker and the relative age share one line. The speaker is the card's
   `alias@room` for an agent message and "you" for clint's own. The age is the short relative form ("1m").
2. **Smaller default type.** A smaller default font and size across /m.
3. **Pinch changes the text size, not the page zoom.** Today a pinch scales every element, and that is not what
   clint expects.
   - Native page zoom is off on /m: the viewport meta carries `user-scalable=no, maximum-scale=1`, and a
     `touch-action` stops the browser's own pinch.
   - /m catches the two-finger pinch itself and changes one CSS variable, the font size of messages (and of the file
     viewer, `u-new-m-file-viewer.md`).
   - Text reflows to the screen width. No sideways scroll. The header, composer and buttons keep their size.
   - Clamped to about 11px to 24px, remembered in localStorage.
   - The place in the thread holds while the size changes: the message under the pinch stays under it.

An earlier version of this request asked for pinch zoom with `user-scalable=no` absent. clint corrected it: the
text-size pinch above replaces it.

## Tests

Headless sections: the header is one line at phone width for an agent message and for "you". A synthetic two-finger
pinch changes the variable, stays inside the clamp, survives a reload, and leaves the thread anchored. The page has no
horizontal scroll at the largest size.

@review before the hub deploy.
