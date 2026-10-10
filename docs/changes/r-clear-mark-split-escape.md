## Test plan

## @LETTER@. The clear mark waits for the end of a split escape sequence

### @LETTER@1. Divider lands whole

Run `go test ./internal/daemon -count=1 -run 'Mark|Esc'`. Every split offset of a CSI, OSC, DCS and UTF-8 character puts the mark whole
after the sequence, with the other bytes unchanged.

**Expected:** the tests pass, and a stream stuck mid-sequence still gets its mark after 4096 bytes or 2 seconds.
