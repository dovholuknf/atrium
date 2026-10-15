## Test plan

## @LETTER@. A context cycle clears without a Stop hook

### @LETTER@1. Quiet card with a background shell

1. Start a background shell in a card (a long `sleep`), then press New context.
2. Let it run `atrium ready`, with the turn-end hook not installed or its post lost.

**Expected:** within about 20 seconds of its last tool call `/clear` is typed, and the wake follows. Before that, the
chip's tooltip reads "waiting to type /clear: a turn is in progress ...".

### @LETTER@2. The control room keeps its log

1. Run `start-atrium-control.ps1` on a machine where sg4-control has run before.

**Expected:** `ctl\room.err.<timestamp>` holds the previous run's log, and at most 20 are kept.
