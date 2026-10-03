# Review: f-store-lock-flake eb354fee

Range `fe891b44..eb354fee`, one commit on claude/f-store-lock-flake. It is a test-only change to
`internal/gitsync/store_test.go`.

Verdict: **OK** for hub and room. There is one Low.

- **The cause is right.** The "not the mirror" half gave a free `Init` 300 ms, but a real `git init` under `-race`
  takes over a second. That half now waits up to 60 s, and it ends the moment the init returns, so it costs nothing
  when the init is fast. The held half keeps 300 ms.
- **The run.** `go test -race -run TestAnInitOfAMirrorTakesTheMirrorsLock -count=15` passes at the tip, in 16 s.

## Low

- **L1: the held half cannot tell blocked from slow.** If the lock were not taken, an init that took more than 300 ms
  under `-race` would still look "blocked", and that half would pass. That was true before this change. A sturdier
  test lets the init signal when it reaches the lock: a test hook in `Init` that closes a channel just before
  `lock(...).Lock()`. Then it asserts that the init has reached the lock and not returned, with no timing in the
  held case.

Atrium-Verdict: room-ok fe891b44..eb354fee
Atrium-Verdict: hub-ok fe891b44..eb354fee
Quality: a precise diagnosis and the smallest fix. m1mini commits are unsigned.
