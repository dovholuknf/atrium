# Review: r-autocompact 06c45ed2..16488230 (m1mini, 2026-10-02): ROOM DEPLOY OK

One commit on claude/r-autocompact, clint's top priority (a session was seen at 548k of 1M). Every claude card
starts with `--autocompact` at its atrium limit plus 10 percent, clamped to 100k-1M. Unsigned.

## What holds

- **One limit, no statusline.** `cardLimit` (autocontext.go) is the old `autoThreshold` minus the statusline part:
  auto_new_context_k, or the ceiling for an `atrium:context-ceiling` card, the lower of the two when the mode
  reaches it. It uses the Effective* values, so a setting that is off or under the floor still gives the floor
  (contextsize.go:104-110, 142-148), which answers the brief's point 3. `autoThreshold` now calls it, so the cycle
  and the flag cannot drift apart. `autocompactK` is limit × 1.1, clamped to 100..1000 k. The card details show both
  numbers (`autocompact` on the view, the peek footer).
- **The flag value is valid by construction.** The installed claude, 2.1.287, takes `--autocompact <auto|tokens>`,
  "100k–1M". Verified on m1mini: `--autocompact 330k` runs (answered "ok"). `50k`, `2000k` and `bogus` are hard
  errors at startup ("argument '50k' is invalid. It must be 'auto', or between 100k and 1M"). The clamp keeps every
  computed value inside that range.
- **Every start of a card carries it.** All starts go through `launchLocked`: a new launch, the park reopen
  (park.go:147), the session restart and new-context cycle (restart_session.go:97), and the stale-resume fallback,
  which reuses `opts` (launch.go:1166). The launch probe adds the agent mark the launch will add, so an agent
  card's limit follows the mode.
- **No empty flag anywhere else.** The other `runnerArgsWith` callers pass no value (keep-alive forks
  keepalive.go:852, the lean MCP scan lean.go:107), and `withMapped` returns the args unchanged on an empty value. So
  no `--autocompact ""` is ever built. PR runner forks are `-p` with a turn cap, so a window does not apply.
- **The runner row is the right home.** `autocompact_args` follows `model_args` and `effort_args`, with the
  placeholder required (refused without `{autocompact}`). It is skipped, not refused, on a row that has none.
  Migration 0080 backfills the claude row only while it is empty.
- **Tests:** `go vet` is clean on daemon, store and api. The 5 new autocompact tests pass, and `./internal/store`
  passes. The one daemon failure, `TestAHostStillHoldingRunnersIsReattachedWithTheSettingOff`, is the known macOS
  unix-socket bind (`bind: invalid argument`) and fails the same way on the base 06c45ed2.

## Conditions for the deploy (not a hold)

- **An older claude on a room breaks every claude launch there.** An unknown option is a hard error (`error:
  unknown option`), and migration 0080 adds the flag to the claude row on every room the build reaches. That
  includes directors' resumes after a restart. Before each room's deploy, check `claude --help` shows
  `--autocompact`. m1mini (2.1.287) does, and the brief says sg4 was checked. sg3 is the one to check. Follow-up for
  @runtime: probe the runner once at daemon start (`--help` contains the flag, cached) and skip it with a log line,
  and show "runner does not take autocompact" in the details, so a stale claude degrades instead of failing.
- **A window bigger than the model's.** With a 200k model and a 300k ceiling, the card starts with
  `--autocompact 330k`. Claude accepts it (the check is the range only), but whether it then compacts before the
  model's own limit is not verified. Clamp to the model's window when the model is known (a `[1m]` model is 1M,
  otherwise 200k), or verify that claude caps it itself.

Verdict: ROOM DEPLOY OK 06c45ed2..16488230 (room-ok, and hub-ok since the board shows the numbers).

Quality: after the Sonnet switch, a clean one. One function feeds both the cycle and the flag, the runner row
extends the existing pattern, and the coverage of every start path is real. The gap is a capability check for older
runners, which a hard-error flag makes necessary.
