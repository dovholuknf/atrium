# bug-say-in-chat-not-terminal

A say showed in the orchestrator card's phone chat but not in that card's terminal, and clint read it as meant for
another card.

## What the code says

1. **Which terminal atrium typed into.** A say from a session reaches `handleMessage`, which types through
   `typeThroughGate` into `d.sup.get(taskID)`. The board's attach (`attach.go`) takes its runner from the same
   `d.sup.get(taskID)`, and so do the retry (`pendinginject.go`), a wake (`keep.go`) and `forceSay`. After a restart the
   old runner is gone from the map (`removeRunner`, or `adoptLive` replaces it with the reattached one), a reopen
   cannot start a second runner on a live card (`ontoRefusal`, `launch.go`), and `sup.add` is the only writer. So there
   is no path in the code where the say goes to one pty and the board attaches to another. The in-process test
   `TestASayAfterARestartIsTypedIntoTheTerminalTheBoardAttachesTo` pins it: a runner replaced the way a reopen replaces
   it, a say from a peer, the bytes land on the new pty and none on the old, and an attach shows what the runner then
   echoes.
2. **Typed or carried.** The transcript line `"[atrium] @minion says: done: ..."` with `promptSource typed` is exactly
   `peerBanner(from) + text` plus Enter, which only `typeThroughGate` writes. A hook delivery arrives as a block reason
   and never carries the grey `[atrium]` label. So it was typed into a pty, and Claude Code drew it there.
3. **The chat bubble.** The phone chat headed a peer prompt with the sender's name and nothing else. The agent's own
   replies are headed with the card's name, so a bubble headed `@minion-...@claude-sg4` read as another card's output
   rather than a say to this one. It now carries a `-> you` mark beside the name.

## What is not proven

The terminal that clint was looking at is the open question. The daemon side cannot send a say to a terminal other than
the one an attach gets, so the likely remaining cause is the BOARD's terminal view: a pane that was attached before the
20:56 restart and not redrawn by the replay after it. That needs a live browser against a live restart, which this item
was told not to do. If it recurs, capture the board's console around the restart (`traceCtl` `sock-close` and the
attach frames) and the room log line `reattached runner for ...`.

The phone chat before and after is under `docs/changes/img/bug-say-in-chat-not-terminal/`. The headless unit `mPrompts`
asserts the mark.

## Test plan

## @LETTER@. A say to a card reads as to that card

### @LETTER@1. The phone chat

1. Have one session say something to another with `atrium_say`.
2. Open the receiving card's chat on the phone.

**Expected:** the bubble is headed with the sender's name and an `-> you` mark. The card's own replies carry no mark.

### @LETTER@2. The terminal after a restart

1. Restart the room with a card open and its terminal on the board.
2. Say something to that card from another session.

**Expected:** the say is typed into the card's terminal and shows there.
