# r-new-exit-guard: a worker exited its own director

Status: HELD (pause). Owned by @runtime. Filed by the orchestrator 2026-10-02, from @fabric's diagnosis.

## What happened

At 13:36:30Z the u-growler-off worker (m1mini card 01a0fccd) called `atrium_exit` with card
01a0f8bd-c580-7a54-8551-ba3dc14a4a0d, meaning to exit itself. That id was @ui's, the director that launched it. The
worker had taken it from the `card` field of an `atrium_say` reply, which names the RECIPIENT. @ui's session got the
exit keys and its card went to done. @review's verdict six seconds later reached it as "refused". Nothing was lost,
but the director stayed down until the orchestrator woke it.

## Wanted

- `atrium_exit` with no card exits the caller. Exiting any card that is not the caller or one of the caller's own
  children is refused, or needs an explicit `force` that is recorded on the card.
- `atrium_say` replies name the recipient as `to`, not `card`, so the id cannot be mistaken for the caller's own.
- A director (a card tagged as one, or with a context-ceiling tag) cannot be exited by an agent at all, only by the
  operator.
- A test for each.

## @runtime director, 2026-10-04

Landed already under the id without `new-` (r-exit-guard ca014c1e, r-opencode-bubbles 82a63fd3). Nothing left.
