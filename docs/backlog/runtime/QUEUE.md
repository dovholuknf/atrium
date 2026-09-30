# @runtime queue

Top down. @runtime takes the next item from the top and reads this file first after a restart or a new context. The
orchestrator reorders it by editing this file. An item leaves the list when it lands on claude/main, with its sha in
`notes/director-reports.md`.

## Next

1. **Held-message escalation R1** (`docs/rnd/held-message-escalation-design.md`, section 3). In flight: worker
   `r-esc-r1` on sg4, worktree `D:/worktrees/claude/atrium/r-esc-r1`, branch `claude/r-escalation-r1`, card
   `01a0f3f0`. Review its report, gate, merge.
2. **B. A TestMain guard against live rooms.** In `internal/daemon` and every package whose tests spawn runners: clear
   every `ATRIUM_*` pointer before any test runs, and force spawned sessions onto the shell runner, so no test can
   reach a live room whatever the shell's environment. Replaces per-test care. Report when it lands.
3. **Held-message escalation R3** (section 5): the `long-turn` flag. Built and committed as WIP on
   `claude/r-escalation-r3` (4535263e), gate running. Rebase on B, gate, merge.
4. **Held-message escalation R2** (section 4): over context mid-turn, told once, then once more at +50k.
5. **Resume-says-continue R5** (`docs/rnd/resume-says-continue-design.md`): the deploy hold records who was working,
   and its lift wakes only those.
6. **Resume-says-continue R6**: `scripts/live/deploy-batch.ps1` takes a hold.

Workers go to sg3 or m1mini, whichever has fewer running (`scripts/room-git.ps1 worktree <room> claude/<id>`, then a
launch on that room). At most two heavy jobs of mine on sg4 at once.

## Done today (2026-09-30)

Growler R1 to R3, handle-HTTP R1, H1, R2, C1, card URLs R3 and R4, security stages 0 and 1, the restart fixes, and
the review fixes for 54794900, 5edc1821, ff747683, 0b4e3f0d, c184ae8c, b144c66a and f4466ea0. See
`notes/director-reports.md`.
