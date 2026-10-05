# r-context-cycle

Built to docs/context-cycle-design.md. Merged claude/r-context-nudge-handoff first.

## What landed
- Daemon (7b341759): one trigger, the card past its limit (card override, else the hub's per-harness `context_limits`, default claude=200). Checked on every statusline update and every context tick. Limit prompt typed as soon as the line is free, even mid-turn, at most once more per turn, never cleared without the ack. `/ready` on the agent listener stores the handoff on the card as a notified event, acks, and the cycle types `/clear` at the turn end, waits for the new SessionStart, then `read <path> and continue.` Handoff at `$TEMP/atrium/handoffs/<card-id>.md`, or the room's `context_handoff_dir`. holdingMessages kept through the cycle, permission chain order unchanged. Nudge, launcher notice, atrium:subagent exclusion, context tags, auto new context and ceiling paths deleted.
- CLI (85a3dfde): `atrium ready`, finish posture, prints the daemon's line, exits non-zero on a refusal with its reason.
- Hub (c8303056): link/contextlimits.go. A `/v1/settings` write naming `context_limits` is validated and stored at the hub in any scope. In the ALL view the hub fans it to every room and answers itself; in a named room's view the room gets the write and the hub passes it to the others. `PushContextLimits` on attach; the ALL view's settings read overlays the hub's value.
- Board (812f1ba2): details "context cycle" section (switch, own limit, the limit in force and its source); gear `context limit per harness` (`claude=200, codex=300`) replacing the warn box; handoff directory in the room's own pane; `handoff` and `context` rows in the card history (text behind a disclosure, escaped); chip words `waiting for ack / clearing / waking`; peek sources card/hub/default; runner limit field removed.
- Headless (524cce6a): `contextCycle` section; `ctxLimitLayers` and `contextSize` reworked for the new sources and the new gear box.

## Choices to know about
- `ready` refuses a missing or blank handoff file (409, "write your handoff to <path> first"), so the clear never follows an empty handoff. It also refuses when no cycle is waiting.
- Fixture and guest cards and cards frozen for a move are still excluded, besides unsupervised ones. "Every supervised card" otherwise holds, clint's own included.
- `harness.context_limit_k` (the runner column) is no longer read; the field stays on the row so old rows load.
- A card that falls back under its limit before the ack (e.g. its own /compact) drops the cycle with a `dropped` history line rather than keep asking.
- A room restart on the limit step leaves no chip, only a history line; the cycle starts again on the next reading if still past the limit. A restart during clear or wake leaves the failed chip, and rerunning it resumes at the clear, no new handoff asked.
- Only claude cards with a transcript have a size today, so a non-claude harness entry (codex=300) is stored and handed out but cannot trigger until that harness reports a size.
- The limit and wake prompts carry the existing `[atrium] new context:` label ahead of the plan's wording, so a reader can tell atrium typed them. Otherwise the wording is the plan's verbatim. The other "Details decided without asking" are followed as written.
- The turn a prompt belongs to is read inside the typing gate, before the write; reading it after the Enter raced with the turn the prompt itself started.
- The manual "new context" menu action now runs the same limit/ack/clear/wake sequence. Idle parking and moves still use the old capture step (token-checked handoff), which is not a context cycle.

## Tests
- New Go tests: internal/daemon/contextcycle_test.go (once per turn mid-turn, no ack never clears, ack then clear then wake with the handoff stored, card switch and override, hub limit, back under the limit, ready refusals, supervised only, wording), journal restarts, internal/link/contextlimits_test.go, internal/cli/ready_test.go.
- `HEADLESS_ONLY=bootClean,contextCycle node scripts/test-board-headless.js`: passed. `ctxLimitLayers,contextSize`: passed.
- `env -u ATRIUM_LOCATION -u ATRIUM_DEBUG_INPUTLAG go test ./internal/...`: all packages pass except failures that also fail at the base commit 7b3ba864 on this Mac: ptyhost and the daemon's hostterm/reattach tests (unix socket path too long, "bind: invalid argument"), TestKeepaliveForkCarriesALeanCardsPromptToolsAndMCP, TestNoTestHereCanReachALiveRoom, api TestTheWalkerLaunchSetAndClear (/private/var symlink). TestIdleParkWorkersFirst failed once under the full run and passes alone.
- `go build -o build.claude/ ./...`: ok.
- docs/test-plan.md: new section IX; BZ, CG, CI, ED, EW, FN, HF, HO, HX marked superseded; ID updated. changelog/runtime/2026-10-05-r-context-cycle.md added.

Nothing deployed or restarted.
