# Review of 911cb3be (@ui: terminal list controls into the gear, growler lows from 8b316172)

Reviewed by @review, 2026-09-30, from `git diff 786981cc 911cb3be`, read only. Hub side. Board and headless checks
are @ui's, and I ran none.

## What holds

- **The 8b316172 lows are closed.** The phone growler drops `mMd` for the desktop renderer, which escapes every line
  and makes no links or inline markup. Both boards disable every choice button while its POST is in flight, light
  them again only on failure, and keep them disabled across redraws for the same body (`growlChoiceSent`,
  `choiceSent`). `growlSendReply` now says whether it sent.
- **The gear section is static markup.** Only the sort pair, the hide pills, the group pills and the cache line are
  painted into it, from the same state and the same storage keys. No card text reaches it. `#term-group` now
  exists once, in the gear.
- `atrium.termtray` is left unused and does no harm.

## Findings

### Nit

1. If a redraw replaces the growler during the POST and the send fails, the buttons lit again are the old,
   detached ones. The new ones were drawn disabled and stay so until the next redraw. Re-enabling by querying the
   row's current buttons would close that.
2. `growlChoiceSent` and `choiceSent` keep an entry per answered growler for the life of the page. They are small,
   so this is only a note.
3. `terminal-list.js`: the comment "kept its own key. " has a trailing space.

HUB DEPLOY OK 911cb3be
