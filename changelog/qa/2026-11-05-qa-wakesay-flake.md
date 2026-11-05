# TestAWakeSayToAParkedCardTypesNothingUntilReadyThenOneTurn flake

Main run 42113412537 failed at keep_integration_test.go:255 with "undelivered 1, want 0".

Cause: the test polled for the submit (`\r`) in the fake pty, slept 10 ticks, then read `UndeliveredCount` once. The
typist in `keep.go` writes first and calls `markDelivered` after, so on a slow runner the row was still pending when the
test looked. The sibling test already polls for the count to reach 0.

Fix: wait with `testwait.Eventually` for `UndeliveredCount` to reach 0 before the no-second-turn sleep. No timeout was
widened and product code is unchanged.

PROVEN: 20 of 20 passes after the fix (`-count=20`, integration tag).
INFERRED: the failure before the fix, from the code order. It did not reproduce locally, so it was not run red.
