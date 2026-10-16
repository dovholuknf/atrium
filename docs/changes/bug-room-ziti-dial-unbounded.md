# Room data dials are bounded (bug-room-ziti-dial-unbounded)

Postmortem: a room attached over ziti kept the control link up while every data dial timed out. `Room.open` started a
goroutine per requested connection with no cap, the hub asked again on every request that found its pool short, and a
ziti `Dial` takes no context, so a timed-out dial goroutine lived on. 1322 of them stacked up.

## Design

- **Cap.** `Room` holds one counter of data dials in flight, per attachment (reset with the session). The cap is
  `maxDataDials = 8`. `open(n)` starts at most `cap - inflight` dials and drops the rest. The hub asks again when
  its pool is still short, so a dropped ask costs nothing.
- **In-flight rule.** The counter goes up before the goroutine starts and down when `Dial` returns, whatever it
  returned, so a dial that ignores its context still counts until it really ends. A pending dial is never
  abandoned and replaced by a new one. A dial that finishes after its attachment ended closes its connection.
- **N failures redial the control link.** `dataFailed` counts consecutive failures, `dataWorked` clears the count.
  At `dataRedialAfter = 5` in a row the room closes its control connection, so `Run` falls into its normal backoff
  and `attach` dials afresh. A Dialer that implements the optional `Reset()` is told first, so a transport can
  throw away a stale context. The count restarts at zero after the drop, and the drop is logged.
- **Test.** Unit test with a fake Dialer whose `Dial` blocks forever: 1000 `open` calls leave in-flight at the cap and
  the goroutine count within a small constant of where it started. A second test has a Dialer that always fails and
  asserts the control connection is dropped once after N failures and the failure count resets.

Out of scope: what creates a ziti context per attempt (`bug-room-ziti-goroutine-pileup`).

## Revised after review

- The in-flight counter is on the Room for the life of the process and is never reset on reattach. A dial gives its
  slot back only when `Dial` returns, whichever attachment it belonged to. A dial that returns late with an ended
  attachment (cancelled context or a stale session) closes its connection.
- A dial still pending after `Timings.DataDeadline` (30s default) calls `dataFailed` once and keeps its slot. When
  the cap is full and every slot is past its deadline, each ask counts as one more failure toward `dataRedialAfter`.
- Tests: a blocking Dialer, five deadlines with no return drop the control link once (and call `Reset`); a full cap of
  late dials turns asks into failures; ten reattaches against a blocking Dialer never exceed the cap.
