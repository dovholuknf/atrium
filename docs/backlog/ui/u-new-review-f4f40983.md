# Review of fix-cover f4f40983 (@ui, the hub restart cover that stayed up 10 minutes)

Range `727ebe27..f4f40983`, one commit on claude/fix-cover. Board only: `internal/api/web/js/hubrestart.js`,
`css/dialogs.css`, `index.html`, and the `coverPoll` and `coverSteps` headless sections. Order:
D:\tmp\ui-order-cover-filter.txt item 1.

## Against the order

- **The cover asks once a second, whatever the stream or `#conn` do.** `hubShowRestarting` starts `hubCoverPoll`
  (`setInterval(hubCheckBack, 1000)`), and `hubClearRestarting` stops it. Those are the only two places that touch
  it, so the poll exists only while the cover does. A cover put back after a reload starts it too, through
  `holdTheCover` and then `hubShowRestarting`.
- **Suspect (a), a check that meets the old hub, is closed.** That answer used to end the chain. Now the next tick
  asks again.
- **Suspect (b), the retry gated on `#conn` being live, is gone.** A failed check now returns, and the poll retries.
- **Suspect (c), a check in flight swallowing the new hub's stream open, is closed.** The next tick runs once
  `hubChecking` clears. A hung request cannot hold `hubChecking`, because `AbortSignal.timeout(3000)` rejects it.
- **The settle phase cannot strand the cover.** Once the hub is back, `hubSettleFrom` is set, so the poll ticks
  without asking. `runPass` calls `onRefreshSettled` in a `finally` with a watchdog, so the cover always comes down.
  A new build reloads under the written-down cover, and the reloaded page polls again with `hubDropped` true and the
  old boot as `from`.
- **10s and 30s.** Past 10s the line says something is wrong, and `.stalled` sets `animation: none` on the ring and
  the bar. The blur stays, which is the look clint keeps. It no longer repaints every frame. Past 30s the reload
  button shows. It is static markup with a literal `location.reload()`, and nothing from the server reaches it.
  `hubClearRestarting` removes `stalled` and hides the button.
- **The tests.** `coverPoll` flips the gate boot between the old and the new hub, and asserts that the old hub's
  answer keeps the cover up and the new hub's answer clears it. `coverSteps` checks 6s (no wrong line, still
  animated), past 10s (wrong line, animations off, no reload) and past 30s (reload, and the reloaded page clears
  against the new hub). @ui says both fail on the old file. I read them and did not run them, since board checks are
  @ui's.

## Findings

1. **Low.** `AbortSignal.timeout` is used here for the first time on the board. `hubCheckBack` sets
   `hubChecking = true` and then builds the `fetch` arguments. On a browser without `AbortSignal.timeout` (Safari
   before 16, older Android WebViews), that line throws synchronously, and `hubChecking` stays true for the life of
   the page. Every later check returns early, the stream-open check included, so the cover clears only by the 30s
   reload. That is worse than before this commit. Fix: build the signal only when
   `typeof AbortSignal.timeout === "function"`, or set `hubChecking` after the call starts, inside a `try`.
2. **Low, older than this commit, and now reached sooner.** When `hubRestartFrom` is empty, `back` is `hubDropped`.
   That happens when the page never learned the hub's boot before `restarting`, or a reloaded page had no `from`. If
   the old hub's stream blips before the hub goes, as the file's own comment describes, the next poll meets the old
   hub with `hubDropped` true and settles. The poll reaches that check within a second instead of waiting for a
   stream open. On a hub, `onHubStreamOpen` sets `hubBoot` at load, so this needs a page that missed it. Note only.
3. **Nit.** In `coverStepsSection`, a `//` comment trails the `waitForFunction(...).catch(...)` statement on the same
   line ("The hub comes back and the reload lands on a cover that clears"). Move it to its own line.

Quality: after the Sonnet switch, no drop. The suspects were proven or killed with tests that fail on the old code,
the poll is bounded to the cover as the order asked, and the CPU point was handled.

**HUB DEPLOY OK and ROOM DEPLOY OK 727ebe27..f4f40983.** Finding 1 should be folded in before a phone that old meets a
restart. It does not affect desktop Chrome, Edge or Firefox.
