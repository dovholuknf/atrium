# Review: u-blocker (board half), e0624583..10a19a37 (m1mini, 2026-10-02): OK

claude/u-blocker, one commit, JS and CSS only, clint's ask (not held by the pause). A launched card that never
starts (`escalation.source` `launch-idle` or `launch-prompt`, from a room contract @runtime adds later) is a red
BLOCKER: marked, pinned in the bell, rung on the permission tone and rhythm, with attach. Until rooms send those
sources, nothing changes. Unsigned.

## What holds

- **Recognised narrowly.** `isBlocker` matches exactly `^launch-(idle|prompt)$` on a card that is not done or dead
  (`over`, stack.js:566). Every existing source looks and rings as before. The stuck alert filters blockers out
  (`isStuck(t) && !isBlocker(t)`), so one card never rings twice. `stuckMark` hands off to `blockerMark`.
- **The setting choice is right.** A blocker is not silenced by "stuck agents", because like a permission it holds
  the agent at the door. It still respects mute and the perms notify hold (`play` treats it as the permission
  kind). A reload re-announces a standing blocker, as it does a pending permission.
- **The rhythm.** Toasts are keyed on `id#source#count`, so each step of the room's backoff rings once, and
  `reapToasts` retires the stale step and a cleared blocker.
- **Rendering is safe.** `blockerReason` maps the known prompts to fixed words, and the room's `text` fallback is
  escaped everywhere it is drawn (the mark's tip, the pinned row). The pinned rows are built from the live cards,
  so a handled blocker leaves nothing pinned. Attach goes through a `data-attach` and `closeOpenDialogs`, with no
  inline JS.
- **Tests.** The new `blockerMark` unit has 15 checks:
  - the mark appears only for a blocker and for no other source, and it is red with its words;
  - it rings with the stuck setting off, and again at the next backoff step;
  - the bell shows it as the pinned first row, with attach and its reason, and the bell button turns red;
  - clearing removes both the mark and the pin.

  I read it and did not run it, per the board rule.

## Lows

- **No test that a blocker rings once with the stuck alert on.** The filter is there, so assert it: stuck at
  "alert", one blocker, one tone.
- **A log row is painted red by the title text** (`/ is BLOCKED/`), so a card titled "x is BLOCKED y" turns its
  ordinary rows red. It is cosmetic, but agents set titles. Record the kind on the log entry when it is written,
  and style from that.

Verdict: OK e0624583..10a19a37, hub-ok and room-ok. The room half (the two sources and `escalation.prompt`) is
@runtime's, and the board shows nothing until it lands.

Quality: after the Sonnet switch, a well-contained change. It is narrow on what it recognises, reuses the permission
path rather than adding one, and the test walks the whole life of a blocker.
