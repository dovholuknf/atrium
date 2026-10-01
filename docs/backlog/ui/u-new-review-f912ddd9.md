# Review: u-no-toast ff5def61..f912ddd9 (m1mini, 2026-10-01): HOLD

claude/u-no-toast, 5 commits. Item: u-new-no-toast-on-focused-terminal.md, a pause exception approved by clint.
Commits are unsigned.

## What holds

- The rule sits in the right place. `growlOnIt` is `growlAsks && readySilenced(card)` (growl.js:49). That is the
  same test the ready alert follows: focused and visible (`focusIsHere`, notify.js:99), with the card attached on the
  terminals view or in solo (`termWatching`, notify.js:507), or another window's fresh focus claim naming the card.
  A raise under it is logged (growl.js:92), and it is kept out of `raised`, `growlDrawn` and `growlSay`.
- **Permission always shows: agreed.** The gate is answered on the board, not in the terminal, so hiding it hides
  the only way to unblock the card. Halts and deploy holds are not about one terminal. Keep both as they are.
- **The pop-out pairing at raise time is right.** The pop-out sends `win-focus` with `watch: watchedCard()`, the
  board's `readySilenced` matches it, and both windows hide the growler. The claim expires after `focusClaimFor`.
- **"Open" is right.** It awaits `landOnAlert`, then posts `dismiss` with the undo, the same as "dismiss this".
  `closeOpenDialogs` refusing still returns before anything is posted. On a permission, open now dismisses the
  growler. The request stays in the perms view, and the permission nag takes over from there, since `growlHas` goes
  false. That is fine.

## Medium (holds): a hidden question comes back only on a reminder, and after the 2h step never

`growlQuiet` loses an id in two places only: when the growler leaves the set (growl.js:102), and when a reminder
finds you elsewhere (growl.js:644). Nothing re-checks the rule when the conditions that made the growler quiet end:
window blur, the tab hidden, another card attached, the terminals view left, or the pop-out blurred or closed.
Reminders follow `growlBackoff` (internal/link/growl.go:84: 1, 2, 5, 10, 30, 60, 120 minutes from the raise) and stop
after the 2h step.

Scenario: clint is on the orchestrator terminal, a question is raised, he reads part of it and switches to another
card. Nothing is drawn until the next step, up to 30 or 60 minutes later. If he stayed on that terminal past the 2h
step (normal for the orchestrator), the growler never comes back. Only the card badge is left. Board and pop-out are
the same: a blurred or closed pop-out leaves the board's copy hidden.

The new case shows the gap. Its last step says "the next raise draws on the board again", but it only asserts that
the focus claim cleared. It never checks the hidden question.

Fix: re-check on the events that change the rule. That means `focus`, `blur` and `visibilitychange`, a terminal
attach or detach, a view switch, and the `win-focus` or `win-blur` message (plus the claim expiring). Drop every
quiet id where `!growlOnIt(g)` and call `growlDraw()`. Drawing without a ring is enough: you were on the card when it
was raised, and the next reminder rings as usual. Test: raise quiet, blur or attach another card, and expect it drawn
with no reminder. Do the same for the pop-out blur and close.

## Low

- The `growlOnIt` case's closing comment claims a redraw it does not test (see above). The fix's test covers it.

## Tests

Board checks are @ui's. I read the cases and did not run the suite. `growlOnIt` covers the raise rule, each
condition false in turn, a permission still showing, a reminder releasing the question, and the pop-out raise on
both windows. `growlActions` adds open, then one board `dismiss` post with the undo. They are meaningful and fail on
the old code, as the worker reports.

Quality: after the Sonnet switch, the work is clean and well reasoned. It reuses the existing silencing rule rather
than inventing one, and the design note answers the item's questions. The gap is state that leaves on the
"leaves the set" path only, never on "the reason went away", which is the usual miss for a hide-while rule.

Verdict: HOLD ff5def61..f912ddd9. A re-read starts at ff5def61, hub-ok and room-ok.

## Re-read at 79b20421 (2026-10-01, m1mini): OK ff5def61..79b20421

One commit over f912ddd9. Unsigned.

- **The medium is closed.** `growlRecheck` (growl.js:56) lets out every quiet id whose growler has left, is no
  longer open, or no longer meets `growlOnIt`. It then calls `growlDraw()` and `growlAttention()`. `growlAttention`
  only sets the favicon count and the title blink, and plays no sound or notification, so "no ring" holds. It runs
  on window `focus`, `blur` and `visibilitychange`, and on every `atrium-solo` message. That includes `win-blur`,
  which a pop-out sends on `pagehide` (notify.js:140), so closing a focused pop-out releases at once. A 1 s tick
  covers the rest.
- **The 1 s tick is acceptable. Keep it.** It returns at once while nothing is quiet, and `readySilenced` is a few
  field reads. Event-only would mean hooks in attach, detach, `switchView` and the 12 s claim expiry
  (`focusClaimFor`, notify.js:90), with a new miss each time someone adds a way to change the view. The tick is the
  backstop for exactly that. A hidden tab throttles it, and `visibilitychange` covers that case. The second
  `BroadcastChannel("atrium-solo")` also hears this window's own posts, which is harmless: it only re-checks. If a
  re-check runs before notify.js has applied the same message, the next tick catches it.
- **Tests.** `growlOnIt` now covers six ways of leaving, none with a reminder: blur, hidden tab, another card
  attached, the terminals view left, pop-out blur, and a focused pop-out closed. Each waits for the growler to be
  drawn and checks that the tone and notification counts did not move. The earlier test gap (the claim-only
  assertion) is fixed. I read the cases and did not run them.

Quality: after the Sonnet switch, the fix is small and covers every exit the review named. The comment says why the
tick exists, and the test asserts both the drawing and the silence.

Verdict: OK ff5def61..79b20421, hub-ok and room-ok. No room deploy during the pause.
