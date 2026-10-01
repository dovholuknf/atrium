# @runtime queue

Top down. @runtime takes the next item from the top and reads this file first after a restart or a new context. The
orchestrator reorders it by editing this file. An item leaves the list when it lands on claude/main, with its sha in
`notes/director-reports.md`.

## Next

0. **r-card-model** (clint via orchestrator, FIRST): `POST /v1/tasks/{id}/model` and an `atrium_model` tool type
   `/model <id>` into a live card and record its model. Worker r-card-model on `claude/r-card-model`. Report in
   director-reports.md the moment it is live.
0a. **Director context ceiling 150k, mid-turn included** (`docs/rnd/factory-shape.md`): r-029 and r-016 are on main.
   Missing: a 150k ceiling for directors only, and cycling a card that never idles. Worktree r-director-ceiling.
0b. **Hub orchestration partial** (factory-shape.md): (b) fyi/needs kind on reports, held on the card, (c) liveness
   and ended-without-report as board state, (a) one-click "deploy ready", no timer. @review before landing each.
0c. **LP1 low** (`r-new-review-7065bd6f.md`): hubStoreFlags.nudge and hubCall use a hand-passed `--board-addr` as
   given, so `0.0.0.0:7778` is refused and the nudge fails silently. Run it through loopbackBase.

1. **The a92bb5f7 review fix** on `claude/r-esc-fix` 5f1058dc: @review ROOM DEPLOY OK, one low to add first (a test
   that a hook-delivered message is not typed again at turn end), and widen the settle window in
   TestALaunchMakesTheItemAndAFailedStartEndsItOnce. See `r-new-review-5f1058dc.md`.
2. **LANDED as LP1, 465a2c30.** LP2 and LP3 wait. **Design question, the loopback write gate** (`r-new-review-df724652.md` low a): `/_hub/hosts`, launch caps and
   the notify command take writes from loopback only, but a separate `zrok share` process proxying to 127.0.0.1
   arrives from loopback, so every share user passes. One answer for all three. Needs @rnd before code. Sent to @rnd
   2026-09-30 ~20:40 as a design item, with the eleven RemoteAddr gates found so far. Build when @rnd's design is
   approved. Ahead of item 1 then, since item 1 waits on UI only being lifted.

## Parked

4. **Held-message escalation R2** (section 4): over context mid-turn, told once, then once more at +50k.
5. **Resume-says-continue R5** (`docs/rnd/resume-says-continue-design.md`): the deploy hold records who was working,
   and its lift wakes only those.
6. **Resume-says-continue R6**: `scripts/live/deploy-batch.ps1` takes a hold.

Workers go to sg3 or m1mini, whichever has fewer running (`scripts/room-git.ps1 worktree <room> claude/<id>`, then a
launch on that room). At most two heavy jobs of mine on sg4 at once.

## Done today (2026-09-30)

Evening, UI only: paste-done with the spinner (dd97c0f6), board gzip and ETags (0c769458), `output_at` (3c1ac798,
aed000f6), the alias-clash 409 choices (96d07778), /replies `prompts` (f19e6fbd), hosts as a setting (00b9dc0d).

Growler R1 to R3, handle-HTTP R1, H1, R2, C1, card URLs R3 and R4, security stages 0 and 1, the restart fixes, and
the review fixes for 54794900, 5edc1821, ff747683, 0b4e3f0d, c184ae8c, b144c66a and f4466ea0. Then, in a92bb5f7:
the new context on a card that never leaves running (0858cba7), the TestMain guard B (4dc56f1a), held-message
escalation R1 (8ae74914) and R3 (4535263e), all with @review. See `notes/director-reports.md`.
