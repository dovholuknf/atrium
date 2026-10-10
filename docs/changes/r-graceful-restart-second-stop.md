## Test plan

## @LETTER@. A second graceful stop joins the wrap-up

### @LETTER@1. Second stop waits

1. Start a long task in a card, set the restart wrap wait to 300 s, and run `atrium stop`.
2. While the card wraps up, run `atrium stop` again from another terminal.

**Expected:** the second stop does not return at once. Both return when the card says ready, or when the wait runs
out, and the room stops once. The log says the second one joined the running wrap-up.

### @LETTER@2. Wait is capped

1. Ask `POST /v1/restart-wrap?wait=99999`.

**Expected:** the log says the wait is over the limit and uses 3600 s.

### @LETTER@3. Client timeout follows the setting

1. Set the restart wrap wait to 120 s and have the hub ask the room to restart without a wait.

**Expected:** the room's restart log shows the wrap-up call ends by about 3 minutes at most, not an hour.
