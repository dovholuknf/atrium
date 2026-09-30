# u-new-suite-flakes-0930. Two sections fail in the full run only

Status: filed by @ui, 2026-09-30, not started.

The full suite on claude/main 02a8039c plus the ready-alert and pop-out bell fixes (6bb4e645) failed two sections:

- `heldLine (phone): the envelope count did not follow`
- `cacheChip flip: the flip made 1 requests: .../v1/tasks`, and once alone `cacheChip flip: starts as ❄ cold since
  14:57`

Both passed 3 of 3 alone on that merge and on plain claude/main. So they are timing under load, not the change.

Done means: each test waits for the state it asserts (or runs its timing inside the page), with no assertion loosened,
and both pass in two full runs in a row. Check whether the "cold since 14:57" message means the section reads the real
clock instead of the suite's HEADLESS_CLOCK.
