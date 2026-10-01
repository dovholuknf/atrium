# Review of 12bcc03c..732f588e (@ui u-m-card: /m compact, text-size pinch, prompts in the thread, one activity row)

Reviewed by @review, 2026-09-30, from `git diff d3a16dd3 732f588e`, read only. Three commits: 12bcc03c, 7be87788,
732f588e. Hub side. Board and headless checks are @ui's, and I ran none.

## What holds

- **Every new string is escaped.** `headerHTML` escapes the speaker, the note, the time and the tag. Prompts are
  drawn as plain escaped text, never through the markdown renderer, so a typed prompt cannot add markup. A peer's
  name comes from the `[atrium] <name> says:` prefix and is escaped like the rest. `kind` comes from the room
  (f19e6fbd), so a prompt cannot promote itself to `peer` or `command` from its text alone.
- **No double rows.** A local own row is dropped only when an operator prompt has the same first 200 characters and
  falls within 60 seconds. A peer or command prompt never removes one. Prompts older than the first reply in the
  window are left out, so the thread does not reach further back than its replies.
- **The activity row.** `sentOf` shows the newest own message's state only until a reply arrives after it. The text
  is one of four fixed words. The signature check still skips repaints when nothing changed.
- **The pinch.** Its listeners are on the thread alone. `touchmove` is non-passive only while two fingers are down,
  so one-finger scrolling stays passive. The size is clamped to 11 to 24 px and stored only on the gesture's end.

## Findings

### Low

1. **Native zoom is off for the whole page.** `maximum-scale=1, user-scalable=no` stops a low-vision reader from
   zooming the home list, the sheets, the composer and the buttons. The text-size pinch covers only the thread.
   clint asked for this, so it is his call. But a page that cannot be zoomed fails WCAG 1.4.4, and iOS Safari has
   ignored `user-scalable=no` since iOS 10, so on an iPhone the browser still zooms everywhere outside the thread
   (the `gesturestart` block is on the thread only). Keeping native zoom and claiming only the thread's two-finger
   gesture would serve both.

### Nit

2. `agoOf` returns `U.ago`'s answer unchanged on both branches.

HUB DEPLOY OK 12bcc03c 7be87788 732f588e
