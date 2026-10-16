# bug-phone-picker-needs-you-shows-done

In the phone card picker, the "needs you" chip listed done cards.

## Cause

`pickMatches` in `internal/api/web/m/js/card.js` accepted any card with a `mHome.reasons()` entry other than `ready`,
whatever its status. A done card with an unread report or an unseen finish still has one.

## Fix

`needs` skips a card whose status is done, dead or paused (`finishedStatus`, which the `done` chip now shares). The
landing page's Needs you list is left alone: there a finished card with an unread report is a deliberate entry (`mHomeLive`). Done cards stay reachable under the
`done` and `all` chips.

## Proof

Headless unit `mPickerDone` (412x892): with running and needs you on, a done card carrying an unread report is absent,
and it is present once the done chip is on. Before and after: `docs/changes/img/bug-phone-picker-needs-you-shows-done/`
(`picker-before.png` lists `done-rep`, `picker-after.png` does not).

## Test plan

## @LETTER@. Phone picker leaves finished cards out of needs you

### @LETTER@1. A done card with an unread report

1. Run `HEADLESS_UNITS=mPickerDone node scripts/test-board-headless.js`.
2. Expect `mPickerDone ok`.
