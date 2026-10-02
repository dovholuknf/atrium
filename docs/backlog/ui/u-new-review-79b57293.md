# Review: u-switch-latency keep-alive 79b57293

Range `46cccadd..79b57293`, 5 commits, 9 files, on `e74dff63`. That is the landed attach commit, so it is in
`46cccadd`. There are no Go changes.

- `bc28d8df` is the prep commit.
- `f54e3a5b` is the keep-alive itself.
- `542c3f9d` adds the `swMark` instrumentation.
- `c87de980` adds the `keepMemory` probe.
- `79b57293` changes the growl fixtures, which now return no `element`.

Verdict: **HOLD on f54e3a5b.**
- H1: a hidden terminal's answers to terminal queries are typed into the session that is showing.
- M1 and M2 are what a kept attach costs the room and the hub.
- The rest of the design is sound.

## How it was checked

- I read the whole diff:
  - `termSlotRun`, `keepPark`, `keepTake`, `keepShow`, `keepEnforce`, `keepReconcile` and `keepFrame`;
  - the socket handlers in `connectTerm`, including the `onData` handler, `applyPtySize`, `writeRunnerOutput` and
    `keepPage`;
  - every `term.on*` and parser handler on the board;
  - the daemon's `runner.watching()` and its callers.
- I added two probes to `keepAlive` in a scratch worktree at `c87de980` (H1).
- `HEADLESS_ONLY=keepAlive,attachAtOnce,switchPrewarm,bootClean,termWear` passes at `c87de980`.
- `node --check` is clean on the eight changed JS files.

## What holds up

- **The swap is atomic.** `termSlotRun`, `keepPark`, `keepTake` and `keepShow` are synchronous, and so are
  `openTerm` and the frame functions run inside the swap. No await sits between the take and the put. The `finally`
  restores the showing globals even when a frame throws.
- **What a background frame reaches.**
  - `keepFrame` handles `caps`, `size` and output, and drops every other JSON frame.
  - `applyPtySize` has its own `termBgRun` branch, which resizes only the hidden grid.
  - `markWide` and `sizeTermHost` now take `term.element` and not the first `.xterm` in the pane.
  - `keepPage` closes over `mine`.
  - The title, sound, focus, seen mark and toast are not reached. The section's spies show none of them is called.
- **Non-JSON string frames (asked about).** `keepFrame` writes them to the hidden terminal, which is what the showing
  path does after its four `take*` checks. `pasteDone` and `inRefused` are dropped, which is right: `keepPark` already
  ends a paste in flight with "switching terminals".
- **The close paths.**
  - A background `onclose` disposes its slot. That covers a runner exit, a room or hub restart and a dropped link.
  - `keepReconcile` drops cards that are gone or unsupervised on every list, and `popOutTask` drops its card.
  - `keepShow` applies the theme and font again. A window resize is caught by `paneBoxUnchanged` on show, and the
    section shows one resize on return after the viewport shrank.
  - A kind mismatch, shell against runner, is disposed in `keepTake`.
- **79b57293.** The fixture change is right: the `Proxy` term used to answer `element` with a function, and the prep
  commit now reads `term.element`.

## Hold

### H1: a hidden terminal's query answers go to the showing session as input

`term.onData` (`terminal-links.js:1307`, in `connectTerm`) calls `sendInput`, which sends on the global `termSock`.
xterm raises `onData` while it parses, and it parses asynchronously after `term.write`, so outside `termSlotRun`.

When a hidden terminal's runner sends a query, xterm's answer goes to whichever socket is showing. The query could be
DA1 `ESC [ c`, a cursor position report `ESC [ 6 n`, an XTVERSION or kitty-keyboard probe, or anything else xterm
answers. The answer is typed into another session's pty.

Shown with a probe. ka-a is hidden and ka-b is showing, and I send ka-a the frame `ESC[c ESC[6n`. The result: ka-a's
socket gets nothing, and ka-b's socket gets `{"t":"in","d":"\u001b[?1;2c"}` and `{"t":"in","d":"\u001b[40;1R"}`.

So two things go wrong:
- the session that is showing gets stray bytes in its input line;
- the hidden runner never gets the answer it is waiting for, and some TUIs wait for it.

This happens in practice. A shell kept as the shell kind runs fish 4, which sends DA1 at every prompt. Codex asks
for the cursor position when it draws.

The fix: in the `onData` closure, `if (termSock !== sock) { if sock is a kept slot's and open, send the frame on
sock itself, without lag, typed or scroll bookkeeping; return; }`. Add the probe above to `keepAlive` as an assert:
nothing goes to the showing socket, and the answer goes to the hidden one.

The same probe also showed this. With focus reporting on (`?1004h`) and ka-a focused, a switch sent no `ESC [ O` to
either socket. So a hidden runner still believes it has focus. Send `ESC [ O` on the parked socket in `keepPark` if
the runner asked for focus reports.

## Mediums

### M1: the room treats a kept terminal as somebody watching

A hidden attach is still a watcher on the room's runner, and `runner.watching()` is `len(r.watchers) > 0`.
- `autoStillOK` and the human-card check in `autocontext.go:328` never cycle a human card's context while a watcher
  is attached.
- Up to 8 recently used cards per open board, plus every pinned one, now never auto-cycle while that tab is open. A
  pinned card never does.
- `ceilingHeld` is bounded by `ceilingMaxWait`, so the ceiling is fine.
- `shell.go:400` never idle-closes a shell that is kept.

Either tell the room that an attach is hidden, or say in the gear hint and the doc that a kept terminal counts as
watching. The first needs a frame, which is a room change. The second is a sentence. Pick one before this lands.

### M2: the hub pool, reasoned and not measured

Your worker's own reasoning says each kept socket to a remote room holds one of the hub's warm link connections for
as long as it lives. N=8 on one remote room drains the warm pool. `Hub.Dial` asks for one more each time the pool is
short, but the next attach then waits on a fresh dial, which can take up to `DialWait`.

That trades the switch-back for a slower first open on a hub. Measure it on sg4 before landing: an attach to a ninth
card with 8 kept on the same room. Or cap the kept terminals per remote room below the warm count. The ceiling-12
commit is a good place for that.

## Lows

- **L1: a hidden terminal's cursor timer clears the showing terminal's timer id.**
  - `writeRunnerOutput` runs inside the swap and sets the slot's `cursorTimer`. Its callback runs later, against the
    showing globals, and does `cursorTimer = 0`.
  - That loses the showing terminal's pending timer id. Its next `clearTimeout(cursorTimer)` then misses, and the
    old timer shows the cursor in the middle of a frame.
  - The hidden terminal itself is fine: the callback's `t === term` check fails, and `keepShow` turns the cursor back
    on.
  - The fix: have the callback clear only its own id (`if (cursorTimer === id) cursorTimer = 0`), or keep the timer
    on the terminal (`t._atriumCursorTimer`).
- **L2: a socket opened while hidden never sends its size.**
  - A terminal parked while its socket was still connecting gets the background `onopen`, which skips `sendResize`.
  - On show, `onTermResize` sends a size only when the box moved, so the pty keeps whatever size it had.
  - Send once on the first show of a slot that never sent.
- **L3: the history hint says the showing terminal "grows back".**
  - Lowering `options.scrollback` drops the oldest lines, and raising it again does not bring them back.
  - Say that a terminal kept hidden keeps only its last N lines from then on.
- **L4: `keepEnforce` runs on a switch or a setting change, not when the layout narrows.** A window dragged into the
  phone layout keeps its hidden terminals until the next switch. Call it from the layout change.

Atrium-Verdict: hold 46cccadd..79b57293
Quality: a careful design with one choke point, and the section proves the pane is isolated. H1 is the one path that
reaches past the swap, because xterm answers asynchronously.

## Re-read: cc4889c3

Range `46cccadd..cc4889c3`. `cc4889c3` is on top of `79b57293`, and the five commits I reviewed are unchanged. It
changes the item file, `index.html`, `terminal.js` (`KEEP_CEIL`) and the section.

- **The ceiling of 12** is in `KEEP_CEIL` and in the field's `max`, which is good. Two places still say 16:
  - the field's `onchange` clamps with `Math.min(16, …)`, which is harmless because `keepSetting` clamps to 12;
  - the hint says "never more than 16 in all".

  Change both to 12.
- **The non-JSON string assert** matches what I read: the frame lands in the hidden terminal and reaches no pane
  helper.
- **The cursor timers.** Half of your reading is right: a hidden terminal's timer cannot write to the showing
  terminal, because the callback checks `t === term`. The other half is L1. The same callback also runs
  `cursorTimer = 0` against the showing globals, which loses the showing terminal's pending timer id. The next
  `clearTimeout(cursorTimer)` on the showing terminal then misses, so the old timer shows the cursor early,
  mid-frame. L1 stays open.
- **The per-room note.** For M2 it is a plan, not a measurement. That is acceptable if the sg4 run happens right after
  deploy. Name the run in the item: N=8 kept on one remote room, then time an attach to a ninth card.

Still open: **H1**, which is not addressed in this range. M1 also needs one of its two answers. L1 to L4 are as
written, and L1 is now explained above.

Atrium-Verdict: hold 46cccadd..cc4889c3

## Re-read: 3328490f

Range `46cccadd..3328490f`, 6 commits, on `e74dff63`.
- `bc28d8df` is unchanged.
- `a2353559` replaces `f54e3a5b`.
- `662be13e` replaces `542c3f9d`. Only the context of its swMark line moved.
- `b1232322` and `9d214b48` are unchanged.
- `3328490f` replaces `cc4889c3`.

Closed:
- **H1.** In `connectTerm`, the `onData` closure now sends on its own `sock` when `termSock !== sock` and the socket
  is a kept slot's and open. It does no lag, typed or scroll bookkeeping, and returns.
  - A focus-in on show goes through the normal path, because the swap has already made it the showing socket.
  - A replaced socket's old closure is disposed at `:1283` before a new one registers.
  - `keepPark` sends `ESC [ O` when `term.modes.sendFocusMode` is on.
  - Both are asserted. Your worker's mutants turned each assert red with its fix removed.
- **M1.** The gear hint and the item now say a kept terminal counts as watched.
- **M2.** The post-deploy sg4 run is named in the item.
- **L1.** The timer clears only its own id.
- **L2.** `slot.sizeUnsent` is set by the background `onopen`, and `keepShow` sends the size once.
- **L3.** The hint now says the trimmed lines do not come back.
- **L4.** `keepEnforce` runs from `syncPhoneView` and on the `(max-width: 900px)` change, which is the same query
  `termNarrow` uses.
- **The 16s.** The clamp and the hint now say 12.

Tests at the tip: `HEADLESS_ONLY=keepAlive,attachAtOnce,switchPrewarm,bootClean,termWear,gearTermList,growlOnIt,
clearKeepsPage` passes, and `node --check` is clean. L1, L2 and L4 have no test, and the reading above is mine.

Open, a follow-up and not a hold:
- **N1: a kept terminal still has a vote in the pty's size.** `agreedViewport` (`supervisor.go:1571`) runs the pty at
  the widest viewer's width and the shortest viewer's height. A terminal kept hidden on this board keeps the vote it
  last sent.
  - So the same card open in another window or browser is held to this board's old size, until the slot is evicted
    or the tab closes. Before this change, switching away removed the vote. (A phone never votes, and a popout drops
    the slot.)
  - This is the same class as M1. Say it in the same hint for now. The real fix is a room frame that lets a hidden
    viewer stop voting, and it can share that frame with M1's "hidden, not watching".

Verdict: hub-ok, room-ok (re-read, 46cccadd..3328490f)
Quality: every point is fixed at its source, and the H1 and focus asserts are proved by mutants. A strong change for
switch-back speed.

Atrium-Verdict: hub-ok 46cccadd..3328490f
Atrium-Verdict: room-ok 46cccadd..3328490f
