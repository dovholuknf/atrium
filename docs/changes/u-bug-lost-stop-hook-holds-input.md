## Test plan

## @LETTER@. A lost Stop hook does not hold typed input after a reattach

### @LETTER@1. Turn ends while the room is down

1. With `pty_host` on, start a claude card and let it work a turn.
2. Stop the room, let the turn end while it is down, then start the room again.
3. Queue a say to the card, or let it reach its context limit.

**Expected:** within about `LooksIdleAfter` of silence the log says the reattached screen shows an empty prompt, and
the say or the cycle prompt is typed with no keystroke from a human.

### @LETTER@2. A half-typed command is left alone

1. Repeat the above, but leave text on the prompt before the room stops.

**Expected:** the line stays unsure and nothing is typed over it.
