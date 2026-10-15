## Test plan

## @LETTER@. A say can be forced past the typing gate

### @LETTER@1. Force after a restart

1. Restart the room with a runner at an idle prompt, so the typing gate reads "not sure what is on the line".
2. `atrium_say` to it with `force: true` and the text `continue`.

**Expected:** the answer is `delivered: terminal` with `forced: true`, `continue` is typed and submitted, and the card's
history shows the prompt as `typed anyway, past the typing gate`. Without `force` the same say stays held.

### @LETTER@2. Force is refused when something owns the terminal

1. With a permission dialog drawn by the runner, or a new-context cycle running, say with `force: true`.

**Expected:** a 409 whose reason names the dialog or the cycle, nothing is typed and nothing is queued.

### @LETTER@3. The held chip

1. Hold a message behind an unreadable line, hover the chip in the terminal pane and choose "Type anyway".

**Expected:** the waiting messages are typed and submitted, the draft on the line is untouched, and the chip goes. A
dialog or cycle shows the room's reason in a toast.
