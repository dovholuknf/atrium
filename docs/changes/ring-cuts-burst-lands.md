Docs: none, a test-only change.

## What and why

`internal/ptyhost.TestWrappedRingIsTruncatedWithRebasedCuts` failed once in a full-package run and once in ten runs
under sixteen busy processes, with:

```
the first cut is not rebased to the ring start: [{Off:63096 Cols:80 Rows:24}] (from 63096)
```

The test spawns a runner that prints a 64 KB burst into a 4096-byte ring, polls the output offset until it passes
20000, resizes from 100x30 to 80x24, attaches from 0 and expects two cuts: the 100x30 one rebased to the ring start,
and the 80x24 one inside the ring. `ring.snapshot` folds every cut at or before the ring start into one cut at the
ring start carrying the last size, so one 80x24 cut means the resize itself was at or before the ring start: at
least 4096 bytes landed after it. The poll had caught the burst midway.

The trace with offsets logged on every run: on a quiet box the poll first sees the offset at 65743, the whole burst
in one read, and the resize is followed by a 1392-byte repaint. Under load the burst lands in several reads and the
poll can see 20001 with 45 KB still to come. The product's answer in that case is right, and the test's premise is
not.

The fix is in the test. It attaches, reads the stream until READY, which the runner prints after its burst, and
only then resizes and takes the snapshot, from a daemon that takes over since a connection attaches once per run.
The offsets stay in the log. Fourteen runs under sixteen busy processes pass with the resize at the burst's end.

## Test plan

## @LETTER@. The ring-cuts test lands the burst first

### @LETTER@1. Under load

1. Start sixteen busy `node -e 'for(;;){}'` processes.
2. Run `go test -tags integration -count=1 -v -run TestWrappedRingIsTruncatedWithRebasedCuts ./internal/ptyhost/`
   ten times.

**Expected:** every run passes and logs "burst landed, resized at out offset N, attached at M" with M minus N under
4096.
