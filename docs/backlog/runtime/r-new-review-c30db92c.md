# Review: r-autocompact follow-up c30db92c (the 16488230 review's conditions)

Range e4b7653c..c30db92c, read by @review on m1mini. This commit is unsigned, like every m1mini commit.

## What the conditions asked and what came

- **Older claude:** done. The runner is asked `--help` once, and a claude that doesn't list `--autocompact` is
  started without the flag. The card details say "runner does not take autocompact" (peek.js shows the note).
  A runner that cannot be asked is assumed to take the flag and is not cached. That is right, since a runner that
  can't print `--help` can't launch either.
- **A window above the model's:** clamped. `[1m]` gives 1000k, any other named model 200k, and an unnamed model is
  left to the 100k-1M range. launchLocked now passes the resolved model (the card's model is the fallback) into
  the probe task, so a resume clamps on the same model.
- **Checked:** `claude --help` on m1mini (2.1.287) lists `--autocompact`, so the substring probe keeps the flag on.
  go vet is clean, and the autocompact tests and the five new ones pass. gofmt is clean on the touched files, and
  `node --check peek.js` passes.

## Lows (none block)

- **L1, the pipe class (REVIEWER-NOTES).** runHelp is CommandContext plus CombinedOutput with no `WaitDelay`. On
  Windows, `claude` can resolve to the npm `claude.cmd`. If its `--help` ever hangs, the 15 s kill reaches cmd.exe
  but not node, which still holds the pipe, so the call never returns. `takes` holds `p.mu` across that exec, which
  then blocks every launch and board render of a claude card. Fix: set `cmd.WaitDelay`, and don't hold the mutex
  while probing.
- **L2.** autocompactFor runs on the board's card-view path (api.go:982), so it can exec. Before the startup probe
  finishes, a render waits on it. An exec that fails with empty output is not cached, so it re-runs on every render
  of every claude card. The board path should read the cache only, and launches should do the probing.
- **L3.** The answer is kept for the daemon's life. claude updates itself in place, so a room that started on an
  older claude stays without the flag until a restart. A log line says so. That is fine; it just goes in the doc.
- **L4.** Clamping to exactly 200k means compacting at the model's full window, which is no earlier than claude's
  own default. A margin or `auto` may be better. That aliases like `opus` are 200k is unverified.

Closed: the 16488230 conditions (older claude, window above the model's) / Open: L1, L2, L3, L4

Verdict: ROOM DEPLOY OK e4b7653c..c30db92c. The sg3 condition still applies: check `claude --help` there before its
deploy, which the probe now makes safe either way.

Quality: tight and well commented, with each condition covered by a named test. The probe sits on the board path,
and its exec repeats the pipe-timeout class this repo has already seen.
