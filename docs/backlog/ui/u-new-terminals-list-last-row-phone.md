# u-new-terminals-list-last-row-phone: the opened session list cannot scroll to its last row on a phone

**Cause.** With a terminal attached, the session list opens as a panel floating over the terminal
(`.term-layout.has-term.tl-open .termbody`, css/phone.css). It was capped at `max-height: 70vh`, and `vh` is the
layout viewport. On a phone with the browser's bottom bar (or the keyboard) up, the visible screen is shorter than
that, so with enough rows the panel's bottom sat below the visible edge, and `main` (overflow hidden) clipped it. The
panel's own scroll reached its end, but the end was off screen, so the last row could not be reached.

**Fix.** The cap is now what the layout has left under the trigger: `min(70vh, 100cqh - 100%)`, the layout being a
size container (its height comes from main's flex column, which `--vvh` already sizes to the visual viewport). In the
tray view the offset is `--trayh` instead of the trigger.

**Done.** `termListLastRow` (HEADLESS_ONLY) opens the list with 14 rows at 390x844 and 412x915, plain and tray view,
with the visual viewport 84px and 300px shorter than the layout viewport, and asserts the last row ends above the
visual viewport's bottom. It fails on the old `70vh` rule (the 300px case) and passes now. phoneKeyboard, phoneView,
phoneShare, phoneTermBar, phoneFocus, phonePan and bootClean pass alone.
