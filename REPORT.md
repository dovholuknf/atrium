# r-new-room-response-time

Not landed under another name on claude/main (checked the log).

## Changed
- `internal/roomstats/drift.go`: a `Drift` probe that sleeps 20ms in a loop and records how late each wake was. `Drain` returns the p99 since the last drain.
- `roomstats.go`: `Sources.Drift`, a `driftRing`, and `sampleLatency` on each tick. The snapshot gains `latency` with `drift_p99_ms` and `drift_series_ms` (60 per-minute averages, null where unsampled, same grid as `machine.cpu_series_pct`). No samples yet leaves the section absent.
- `internal/daemon/roomstats.go`: wires the probe in.
- Board: `latencyRow` in `rooms-dash.js` beside the CPU and memory rows, plus `.rd-spark.lag` css. Small, uses `sparkSVG`. Red over 50ms.
- Changelog added. No migration.

## Measure
Scheduling drift, the default: it is what the sg4/sg3/m1mini benchmark measured and tracks keystroke lag. I did not add the hub round trip or hook round trip.

## Tests
- `drift_test.go`: p99 and drain, a tick carrying the latency section, and a loaded process (one P, 16 spinners) reading above an idle one (idle under 1ms, loaded 10-20ms typically, up to three tries).
- `go test ./internal/roomstats ./internal/api` pass.
- Not run: the board in a browser, and the headless board script.

## Left
- The board row is untested visually. `scripts/test-board-headless.js` has no check for `lag`.
- gofmt -l lists `internal/daemon/fyi_test.go` and `prrunner.go`, which I did not touch.
