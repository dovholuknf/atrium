# Review: u-switch-latency, attach at once 46b7c087

Range `762537f9..46b7c087`, 3 commits, 5 files. There are no Go changes. `185795bb` on top of the branch is not in this
range.

- `b51e130d` fixes the desktop peek bug.
- `e252ec51` adds the `--hover` mode to `measure-term-switch.js`.
- `46b7c087` makes `attachTask` open the pane from the card in `lastTasks` and refresh it when the read answers.

Verdict: **HOLD on 46b7c087.** The other two commits are OK. One defect, H1, is in exactly the stale-card case you asked
me to look at. The fix is one guard and one test.

## How it was checked

- I read the diff, `openTerm`, `connectTerm`, `markTermDead`, `termCold`, every write to `termTask`, and the room's
  list and single reads.
- I added a probe to `attachAtOnce` in a scratch worktree and ran it. It is described under H1.

## Points

- **b51e130d: OK.** `filedId` reads a missing `mNet` as no rooms, so the id stays bare, which is what a one-room hub
  files it under. The stub is gone, and the section asserts that `paintCard` does not throw. The four sections that
  were red now pass alone, by your run.
- **e252ec51: OK.** This is a measuring script only.
- **The ws URL and the attach size are unchanged.**
  - `connectTerm(task.id)` (terminal-links.js) takes only the id, and it still runs in the same `requestAnimationFrame`
    after the fit.
  - The `terminal.js` change only moves code: `paintTermChips` is the old chip block, byte for byte.
- **The list card has the same fields as the read.** Both come from `toView` plus `withSeen` (`api.go:1031`, `:1292`).
- **The discard on switch is right.** `openTerm` sets `termTask = task`, so `termTask !== shown` catches every move
  away: another card, a reattach (the in-flight and already-open checks return before `termTask` is set), and
  `closeTerm`. Your section covers the A to B to C case.
- **A failed read.** It is logged and leaves the pane open. That is right, because the socket says so if the session
  is gone.
- **The gate.** `termCold` is false for every supervised card, so `listed.supervised && !termCold(listed)` is just
  `listed.supervised`. An ended supervised card also opens at once. That is no change in what opens, because the read
  would have said the same, but the second test does nothing.

## Hold

### H1: the late read repaints a dead pane as live

This is the stale-card case. The list says the card is running, but its session has gone. The socket answers first,
and `markTermDead` marks the pane:
- the title reads "<name> (exited)";
- the chips are replaced with one `exited` chip;
- the buttons are hidden.

Then the read answers, with a different `status` and `pid`. `refreshAttachedCard` applies it, because `termTask` is
still `shown`. It calls `paintTermTitle` and `paintTermChips`, which put back the plain name, the pid and the path
chips. The pane keeps its `dead` class and hidden buttons, but its bar no longer says it exited.

On a hub this order is the likely one. You measured ws open at 30-55 ms, and the remote read at 75-325 ms.

Shown with a probe in `attachAtOnce`:
1. Attach `ao-d` with the read held.
2. Call `markTermDead()`.
3. Answer `{status: "dead", pid: 0}`.

The result: before the answer, the title is `row ao-d (exited)` and the chips read `exited`. After it, they are
`ao/ao-d` and `/tmp/ao/ao-d`, and the pane is still `.dead`.

The fix: in `refreshAttachedCard`, update the fields, but repaint nothing when `#term-pane` is `.dead`.
`followTermAlias` already has the same guard, for the same reason. Add the probe above to the section as an assert.

## Lows

- **L1: `Object.assign(shown, fresh)` copies every field, not only the ones it compares.** Three writes can land on
  `termTask` while the read is out, and the older answer then undoes them:
  - `retagTermId` sets `termTask.id` when the room set changes;
  - a theme save sets `termTask.theme`;
  - `followTermAlias` sets the alias.

  Only memory is affected, but `id` is what a reconnect dials. Assign `REFRESH_FIELDS` only, or skip `id`, `theme`,
  `alias` and `alias_note`.
- **L2: the `termCold` test does nothing.** See the gate, under Points. Drop it, or say why it stays.
- **L3: the stale moment is as described.** The pid and status chips show the list's values until the read answers,
  which is at most one list refresh old. The socket is the truth meanwhile, so that is acceptable. Say it in the
  comment.

Atrium-Verdict: hold 762537f9..46b7c087
Quality: a good change aimed at the measured cost, and the discard on switch is tight. H1 is in the order of events a
hub makes likely, so it has to be guarded.
