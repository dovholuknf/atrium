Docs: none, a fix that makes the stream do what docs/fabric/hub-and-rooms.md already says. Three log lines added.

## What and why

Run 39042049947, windows go-test: internal/link hit the 20 minute package timeout with
TestOneRoomLiveAndOneRememberedStreamsTagged running for 17m59s. Its output says `no "task" event arrived` after 30 s,
then `httptest.Server blocked in Close after 5 seconds, waiting for connections`. Two faults, read off the dump.

The stream fault. The dump had the proxy's `serveEvents` alive, one `feeds.reconcile` alive at its select, and no
`feeds.pump` at all: a board streaming from no room. The test opens a stream, closes it, and opens a second one at
once. The handler of the first drops the last subscriber and cancels the first reconcile run. The second stream adds
a subscriber and, with the count back at one, starts a second run, which starts the room's pump. The first run,
still on its way out, then runs `closeAll`, which cancelled every pump in the shared map, the second run's included.
The second run waits for a change that never comes. On a four core runner under `-p 4` the first run can be that late,
and locally 1 run in 12 under 16 CPU burners was.

Each pump now remembers the reconcile run that started it, `closeAll` stops only that run's pumps, a run whose
context is done starts none, and a pump left by a finished run is replaced rather than kept. Under the same load,
36 of 36 runs of the failing test, the new reopen test and the pump count test passed, and 200 of 200 of the failing
test alone.

The hang fault. On the failure the test's stream client was never closed, `pairWith`'s cleanup called the front's
`Close`, and `httptest.Server.Close` waits for active connections without bound. `pairWith` and `two` now call
`CloseClientConnections` first, so a failed stream test ends in 30 s and not at the package timeout: with the old
events.go and this change alone, 4 of 200 runs under load failed in 30 s each and none hung.

Logs. A pump starting, stopping, failing to open, answering not 200, or ending now each log one line under `[hub]`,
bounded by the pump's backoff. There was nothing to read before.

## Test plan

## @LETTER@. A stream reopened at once still streams

### @LETTER@1. Under load

1. Start sixteen busy shell loops.
2. Run the link package with `-count=12` for `TestOneRoomLiveAndOneRememberedStreamsTagged`,
   `TestAStreamReopenedRightAfterACloseStillStreams` and `TestNobodyWatchingMeansNothingStreaming`.

**Expected:** every run passes. Before the fix one run in twelve of the first test failed and then hung the package.

### @LETTER@2. The hub says what it streams

1. Attach a room to a hub and open the board.
2. Close every board tab.

**Expected:** the hub log has `streaming events from "<room>"` on the first tab and `stopped streaming events from
"<room>": nobody is watching` after the last closes.
