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

## Correction: vertical space first (clint's screenshot, 21:06)

`.atrium/incoming/20260930-210641-Screenshot_20260930_210634_Brave.jpg`, a phone on bc49d82c: a full screen shows
about 2.5 messages. What eats the space, all of it in scope:

- big bubble padding and margins: tighten them
- "YOU" and "just now delivered" on lines of their own: one header line, speaker, age and delivery state
- a large "thinking" box: a thin one-line strip, or folded into the header
- a separate "sent" line above the composer: drop it
- a tall composer and a tall header row: slimmer
- horizontal whitespace: his own bubbles start about 70px in, and every bubble has wide side margins and inner
  padding. Near-full-width bubbles for both speakers, told apart by tint and the header line rather than indent

The history count is a separate cause: /m asked for 3 replies (now 10), and the room caps at 10 (@runtime asked for
50 plus a `before` cursor for "load older").

## Tests

Headless sections: the header is one line at phone width for an agent message and for "you". A synthetic two-finger
pinch changes the variable, stays inside the clamp, survives a reload, and leaves the thread anchored. The page has no
horizontal scroll at the largest size.

@review before the hub deploy.
