# Review of r-card-model 108ced12 + 99fab624 + 9632dd93 (@runtime: switch a live card's model)

Reviewed by @review, 2026-09-30, from `git diff 108ced12~1 99fab624` and the branch tip 86240e3a (claude/main merged
in). Room side (`POST /v1/tasks/{id}/model`, `internal/daemon/modelswitch.go`) and hub side (`atrium_model`).

## What holds

- **`validModel` is a sufficient terminal guard.** It accepts one of four aliases (lower-cased), or a value that
  starts `claude-`, is at most 80 bytes, and uses only `A-Z a-z 0-9 - . _ [ ]`. That excludes ESC and every other
  control byte, CR, LF, space and quotes, so the typed line is `/model ` plus one token, and nothing in it can end
  the line, start an escape sequence or add a second command. The body is read through a 64 KiB limit before the
  check. `from` goes only into the event payload, never to the terminal.
- **Refused before anything is written.** A bad model, a missing card, a non-claude runner, and a card with no
  atrium-owned terminal are all answered before the store or the terminal is touched.
- **Typed through the say gate.** It checks holding, an open dialog and the turn rule, then calls `injectPeer`,
  the same writer a say uses, with no paste markers.
- **The store is written at request time**, so a relaunch or a resume starts on the new model whether or not the
  line was typed. TestAReopenedCardComesBackOnItsModel covers `reopenSaved`.
- **The waiter's lifetime is bounded.** It exits when a newer request replaces its entry, when the runner's `done`
  closes or the run is gone, when the daemon winds down, or at 30 minutes. A burst of requests leaves at most one
  live waiter per card, because every older one exits on its next 2-second tick.
- **The hub side.** `atrium_model` resolves the card the way `atrium_say` does, forwards only `model` and `from`,
  and leaves every check to the room. It is audited as `ctl-model`, with the model clipped to 80 runes, and it is
  not in the worker tool set. The guest surface is an allowlist (`overlay_guest.go`), so a lent card's guest cannot
  reach the route.

## Tests

In a detached worktree at 86240e3a, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` cleared:

- `go vet ./internal/daemon/ ./internal/link/ ./internal/api/`: clean
- `go test -count=1 -run 'Model|Reopen' ./internal/daemon/`: ok
- `go test -count=1 -run 'Model|CtlClass|CtlAudit|Ctl' ./internal/link/`: ok
- `-race` cannot run on this machine (no cgo), so finding 2 comes from reading only.

## Findings

### Low

1. **Cards on the `claude-worker` and `claude-fable` runners are refused.** `task.Runner` is the harness id
   (`launch.go:1012`, `Runner: h.ID`), and `handleModel` accepts only `strings.EqualFold(t.Runner, "claude")`. Both
   rows run Claude Code, and both are enabled on claude-sg4, so a card on either is refused with "only a claude card
   takes /model". A read-only GET of `/v1/tasks` on claude-sg4 shows all 13 live cards on `claude`, so the director
   switch is not blocked today. Deciding by the harness's command (or a `takes_model` flag on the row) rather than
   by its id would cover both.
2. **A late waiter can type an older model after a newer one.** The waiter checks `cur == mw` and then calls
   `typeModel`, and nothing holds between the two. If a new request's handler deletes the entry and types its own
   model in that gap, the old waiter still types its model afterwards. The terminal then ends on the old model while
   the card's store says the new one. `CompareAndDelete` fails afterwards, but only once the old model is already
   typed. A per-card mutex held across the check and the type, in both the handler and the waiter, would close it.
   The window is the length of one gate check and one write, so this is rare.
3. **A switch that gives up leaves no record.** At 30 minutes the waiter returns silently. Its `modelWaits` entry
   stays, with no event and no log line. The card then says one model and runs another until its next start, and
   nothing on the card says the typing never happened. One `notified` event with state "gave up" would show it.

### Nit

4. If `injectPeer` keeps failing, the waiter logs every 2 seconds for up to 30 minutes, which is about 900 lines.
5. This needs a live check (test plan HM). If Claude Code queues mid-turn input as a message, a `/model` typed
   mid-turn on a runner that takes mid-turn input may arrive as text after the turn rather than as the command. The
   unit tests cannot show which happens.

HUB DEPLOY OK and ROOM DEPLOY OK 86240e3a

## Re-read of 2ba81eaf + 32700a16 at 4bedce8b (@runtime, the lows, and hand-typed /model)

`git diff 2ba81eaf~1 32700a16`, read. In a detached worktree at 4bedce8b: `go vet ./internal/daemon/ ./internal/link/
./internal/api/` was clean, `go test -count=1 -run 'Model|Reopen|StartPath|Typed|Submitted' ./internal/daemon/` was ok,
and `-run 'Model|CtlClass|CtlAudit|Ctl' ./internal/link/` was ok. `-race` still cannot run here.

**32700a16 closes all three lows and nit 4.**

- `runsClaude` decides by the harness row (`isClaude`) and falls back to the id only when there is no row.
- `modelLock` is held across "replace the wait, type, store the new wait" in the handler, across "still current?
  type, delete" in the waiter, and across the store write in `noteTypedModel`. The order is always the model lock
  first and the runner's `typeMu` second, inside `injectPeer`. `takeSubmitted` takes `typeMu` alone. Nothing takes
  them the other way round, so there is no deadlock.
- A give-up writes a "gave up waiting" event and one log line, but only if it is still the current wait.
- An `injectPeer` failure logs once per wait.

**2ba81eaf: hand-typed `/model`.** Only operator bytes reach `typedLine` (`noteOperatorTyped` is the only caller of
`feed`), so neither a say nor atrium's own `/model` typing ever sets `submitted`. `submitted` is set on Enter only
when the line was followed exactly (`unsure` empty, nothing dropped), and `takeSubmitted` clears it on read, so each
line is acted on once. `noteTypedModel` takes exactly two fields with `/model` first and runs the second through
`validModel`, the same check the API uses. It skips a non-claude card and a model the card already has, and it drops
a waiting API switch. A bare `/model`, extra words, and `opus[1m]` record nothing. The start-path tests pin
`launch`, `reopenSaved` and resume to `task.Model`.

### Low

5. **A mistyped full id is stored and breaks the next start.** This is @runtime's own known gap, and hand typing is
   where it will happen. `/model claude-opus-4-8` passes `validModel`, Claude refuses it on screen, and atrium still
   stores it. Unless the operator types a correct one afterwards, the next resume (a room restart included) launches
   with that id and the card does not come back. Possible fixes: record only the four aliases from a typed line, or
   store an id only once the statusline's display name shows the switch happened.

### Nit

6. `modelLocks` keeps one mutex per card for the daemon's life. They are small.
7. A `/model x` sent as a message (the /m composer, the board's send) goes through the message path, not the attach
   path, so it is not recorded. Only keys at the attached terminal are.

Quality: after the Sonnet switch. Each fix maps onto its finding, the lock order is consistent in all three places,
the known gap was named unprompted, and the start paths got their own tests. No drop seen.

ROOM DEPLOY OK 4bedce8b
