- **A timeline note can no longer halt a room.** An event on a card that does not exist, or on no card at all, is
  refused as "no such card" instead of failing the event's foreign key, which halted the store. A fixture's first
  start in a directory whose newest conversation a live card holds took exactly that path (r-new-review-c184ae8c).
  The refused resume is now written on the card the fixture made, once it exists, and says whether a fixture or a
  reopen refused it.
- **Only a person's /exit keeps a card down.** An exit atrium types itself, the idle park or the sweep of a worktree
  that went away, no longer counts as somebody asking (r-new-review-b144c66a). `atrium join` and the reaper's revival
  clear an asked exit like any launch, and a terminate that failed no longer leaves a running card marked.
- **Growler review lows** (r-new-review-a23a9034, r-new-review-06b876bb). The set is built and compared under one
  lock, a new stream waits briefly for its opening set, a permission's request is asked for at most three times,
  `growl.since` stays put when it cannot be written, and two comments say what the code does.
- Room side, and the growler part is hub side.
