# Review of 3eee75da (@ui: /m card view, working line, own messages, Recap sheet)

Reviewed by @review, 2026-09-30, from `git show 3eee75da`, `m/` files only, read only. Hub side. Board and headless
checks are @ui's, and I ran none.

## What holds

- **Escaping.** A stored message goes through `U.esc` as text, and its `at` goes through it as the `datetime`
  attribute. `U.esc` covers `& < > " '`. The working line escapes the tool name. The recap still renders through
  `MD.render`, the escape-first renderer, and its time through `U.esc`.
- **A stored message is data, never markup.** A value tampered with in localStorage is still escaped on the way
  out, and a parse failure reads as an empty list.
- **The working line** shows only on a `running` card, hides on idle or an open dialog, and repaints only when its
  signature changes.
- **compose.js** raises `m-sent` only after a send that was not refused.

## Findings

### Low

1. **The size of each message is not bounded, and neither is the number of cards.** `SENT_KEEP` caps each card at 20
   messages, but a message is stored whole, so one pasted log of a few MB fills most of the origin's ~5 MB quota.
   The `setItem` failure is caught here. The drafts and settings that share the origin then fail their own writes.
   Also, a key `atrium.msent.<id>` is written for every card ever messaged and never removed. Could the stored text
   be capped (for example 4 KB with an ellipsis), and could keys whose newest entry is older than a week, or beyond
   the newest 50 cards, be pruned when a message is noted?
2. **What was sent stays on the phone.** Text the operator sent, which can include a pasted secret, now persists in
   the phone's localStorage with no expiry. The pruning in 1 bounds this too. Should the changelog line say that the
   phone keeps your recent messages?

HUB DEPLOY OK 3eee75da
