# u-new-suite-flakes-0930. Two sections fail in the full run only

Status: `phonePan` and `cacheChip` fixed by u-lows, 2026-09-30. `heldLine` was fixed earlier (fcf3b974).

The full suite on claude/main 02a8039c plus the ready-alert and pop-out bell fixes (6bb4e645) failed two sections:

- `heldLine (phone): the envelope count did not follow`
- `cacheChip flip: the flip made 1 requests: .../v1/tasks`, and once alone `cacheChip flip: starts as ❄ cold since
  14:57`

Both passed 3 of 3 alone on that merge and on plain claude/main. So they are timing under load, not the change.

Done means: each test waits for the state it asserts (or runs its timing inside the page), with no assertion loosened,
and both pass in two full runs in a row. Check whether the "cold since 14:57" message means the section reads the real
clock instead of the suite's HEADLESS_CLOCK.

## Causes (u-lows)

Both reproduce at once by running 12 copies of the section in parallel. Each passes 10 of 10 alone and 12 of 12 that way
after the fix.

- **phonePan, "the follow chip went away without input".** The test fed 50 lines on a 100 ms timer and read the chip
  after a fixed 5400 ms. On a busy machine the timer ran late, so the read came at line 43 to 45 of 50 with the cursor
  still sweeping through the pane. By design the chip goes off when output alone brings the cursor into view, so the
  chip was right and the test read it too soon. The same fixed waits (300 ms after typing, 300 ms after the chip tap)
  could read before the follow had drawn, which is the other failure seen, `typing did not follow the cursor`. The
  test now waits for the last line to be parsed (`term.write` callback) and for the state each step asserts.
- **cacheChip flip, "starts as cold since" and "the flip made 1 requests".** The card was given `warm_until` two and a
  half seconds ahead, then the test switched view, asked for a refetch and waited for the row. Under load the two and a
  half seconds passed on the way, so the chip was already cold when read. The refetch is also throttled by
  `TASKS_EVERY`, so it could land after the request count was taken, and a late read would put the server's time back
  over the one set. It was not the clock: the page and the suite agree, `12:00` in the message is the suite's clock. The
  card now arrives warm for an hour through the real paint, the test waits until no read is pending or in flight, then
  sets the two seconds from inside the page and waits for the timer to flip it. The assertions are unchanged.
