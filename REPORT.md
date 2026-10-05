# r-new-held-message-escalation, room stages

## R1, held message stops waiting
Already met, merge a26aebba (claude/r-escalation-r1), `internal/daemon/escalate.go`, `messages.go`. Not touched here.

## R3, long turn
Already met, merge a26aebba (claude/r-escalation-r3), `longTurn` in `internal/daemon/a2a.go`, tests in `longturn_test.go`.

## R2, over context mid-turn
Not covered. `newContextStop` is the new-context step's nudge (a pending cycle), and `contextsize.go` only noticed the launcher at the first crossing. Built here:
- `internal/daemon/contextnudge.go`: `contextLine` gives the line once per turn, once more at +50k (launcher gets a `context-told` notice, keyed on the turn), then nothing. The claim is in memory in `d.ctx.told`, keyed on the turn's start. A new turn re-arms. The "atrium cycles your context" clause is added only where `autoContextSubject` holds.
- `daemon.go` permission hook: the line blocks that one tool call, alone or in front of a queued message's banner.
- Test: `contextnudge_test.go` (under threshold nothing, once, +50k again with one launcher notice, +100k nothing, new turn re-armed).
- Left: the hook wiring itself (daemon.go) has no test beyond `contextLine`. Design question 3 (block versus PostToolUse) kept at the design's default, block.

## Second case, one long tool call
The design covers it and nothing new was needed: the card's `activity.what` and `activity.seconds` give the `Bash 10m` chip (U1), and the existing `long-tool` escalation tells the launcher once at `LongToolAfter` (20 minutes, `ATRIUM_A2A_LONG_TOOL`). Design question 2 (tell at 10 instead of 20) is clint's and left at 20.

## U1, the board (not built, @ui's)
The board needs from card data:
- `held_count`, `held_seconds` (and `held_peer`/`held_for` kind) for `✉ 4 · 1h29m`.
- `context.warn`, `context.tokens` with status `running` for `253k mid-turn`.
- `escalation` with `source == "long-turn"` and `minutes` for `turn 1h32m`.
- `activity.what` and `activity.seconds >= 300` for `Bash 10m`.
No new field is needed.

## Tests
New test passes. Pre-existing failures ignored per the brief. Windows and linux cross-builds checked.

Full `go test ./internal/...` also showed failures unrelated to this change and not in the brief's known list: `TestTheWalkerLaunchSetAndClear` (api, launch body has a `url` key), `TestKeepaliveForkCarriesALeanCardsPromptToolsAndMCP` (daemon, the worker system prompt text). The rest were the known hostterm and testguard ones.
