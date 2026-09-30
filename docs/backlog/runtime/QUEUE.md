# @runtime queue

Top down. @runtime takes the next item from the top and reads this file first after a restart or a new context. The
orchestrator reorders it by editing this file. An item leaves the list when it lands on claude/main, with its sha in
`notes/director-reports.md`.

## Next

1. **The a92bb5f7 review fix** on `claude/r-esc-fix`: the medium (an aged message reaches a runner with mid-turn
   input off by the hook), lows 1 to 4, and a deadline on the test log's waitFor. Send the sha to @review.
2. **r-new-hosts-setting** (`docs/backlog/runtime/r-new-hosts-setting.md`), with the two lows from
   `r-new-review-e25a2c2f.md` folded in: the wildcard is safe only for a domain whose DNS one operator alone sets
   (dynamic DNS like `*.duckdns.org` is not, and `*.co.uk` passes the bare-label guard), and an ignored entry logs
   once instead of vanishing.
4. **Held-message escalation R2** (section 4): over context mid-turn, told once, then once more at +50k.
5. **Resume-says-continue R5** (`docs/rnd/resume-says-continue-design.md`): the deploy hold records who was working,
   and its lift wakes only those.
6. **Resume-says-continue R6**: `scripts/live/deploy-batch.ps1` takes a hold.
7. **@ui ask, hub side:** the 409 for an alias clash (`placeByName`, `internal/link/cardroute.go`) carries each
   candidate's `status`, `activity` and `created_at` beside its name, so the board's chooser need not fetch each one.
   Say to ui-director-of-the-board-on-claude-ui-2 when it is on claude/main.
8. **@ui ask, room side:** the card view carries `output_at` (RFC3339, last pty output, absent when not supervised),
   re-sent on the card's change events only when the wait starts.

Workers go to sg3 or m1mini, whichever has fewer running (`scripts/room-git.ps1 worktree <room> claude/<id>`, then a
launch on that room). At most two heavy jobs of mine on sg4 at once.

## Done today (2026-09-30)

Growler R1 to R3, handle-HTTP R1, H1, R2, C1, card URLs R3 and R4, security stages 0 and 1, the restart fixes, and
the review fixes for 54794900, 5edc1821, ff747683, 0b4e3f0d, c184ae8c, b144c66a and f4466ea0. Then, in a92bb5f7:
the new context on a card that never leaves running (0858cba7), the TestMain guard B (4dc56f1a), held-message
escalation R1 (8ae74914) and R3 (4535263e), all with @review. See `notes/director-reports.md`.
