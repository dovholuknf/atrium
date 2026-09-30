# u-new-persistent-growler. An alert that stays until clint dismisses it or acts on it

Status: not started. PRIORITY (clint, 2026-09-30: "this has become a priority to me"). Ideate and design through
@rnd, build by @ui. Filed by the orchestrator on clint's word:

> i'd like the idea of a persistent growler that gets my attention if possible something that doesn't just
> disappear and forces me to dismiss it or act on it. it seems separate but related to a modal/growler.

## The gap

Toasts (the growler) time out and disappear, so an alert raised while clint is looking elsewhere is gone when he
looks back. The toast log keeps a record, but nothing on screen asks for attention. A modal blocks the whole board,
which is too much for most alerts. What is missing sits between the two: it does not go away on its own, it does
not block the board, and it offers the action to take.

## For the ideation

- What earns one. Candidates: a permission waiting past N minutes, a card in needs-input with unseen Open Questions,
  an orchestrator ask (`atrium_report` status question or blocked), a room halt, a deploy hold waiting on a human.
  Toasts stay for everything else.
- Actions on its face: approve or deny, open the card, reply, snooze for a set time, dismiss. Dismissing or acting
  on it records who and when, the way `seen` does.
- A stack, not a pile: several at once collapse to a count, newest or most urgent on top.
- It follows clint between tabs, browsers and the phone (`/m`). A dismissal on one clears it everywhere, so the state
  belongs on the room or hub, not in `localStorage`.
- Getting attention when the tab is not focused: title badge, favicon, the desktop notification path (log it the
  way the toast log does, see d322f71), and the card's sound. A reminder repeats until it is handled, with a limit.
- How it relates to the modal, the toast and the toast log: one alert model with three levels of persistence, or a
  separate surface. @rnd recommends one.

## Done means

Designed and reviewed by @rnd, built by @ui (with @runtime for any room or hub state), on claude/main, live.
