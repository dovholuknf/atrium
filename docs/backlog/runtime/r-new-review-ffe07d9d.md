# Review: r-opencode-bubbles 6bce194f..ffe07d9d (m1mini, 2026-10-02): HOLD

One commit on claude/r-opencode-bubbles, a pause exception clint approved. It adds a per-runner transcript reader,
with claude's read moved behind it unchanged, and an opencode reader through a bounded `opencode export`. Unsigned.

## What holds

- **The interface is right.** `transcriptReader` (transcriptreader.go) uses ok/err as stated. `claudeReader` is the
  old `repliesPage` body moved as is: the same session lookup, the same `readTranscriptPage` or `…Before`,
  `fillEdited`, and `logRepliesFallback` on error. Its 20 replies tests pass unchanged. codex and gemini can drop in as
  further readers.
- **The opencode read keeps the brief's line.** It never reads opencode's files, only the CLI. There is no shell,
  the args are an argv, and the session id is checked against `^ses_[A-Za-z0-9]+$` before it is an argument. That
  also makes a Windows `.cmd` safe from cmd.exe metacharacters. A stdout cap fails rather than cuts. Results are
  cached for 3 s with one export per card. Only `text` parts are read, synthetic ones are skipped, the text is cut
  at `replyTextMax`, and paging uses `before` over the cached session.
- **Tests:** `go vet` is clean, and `go test ./internal/daemon/ -run 'Opencode|Replies|Transcript'` passes all 30,
  the 10 new opencode cases included, with no opencode installed.

## High: the timeout does not bound the export, and a stuck export holds the card's lock (proven)

`opencodeExec` reads stdout through `StdoutPipe` with `io.ReadAll` before `Wait`. `CommandContext` kills only the
direct child when the deadline passes. Any process that inherited the pipe keeps it open, so `ReadAll` does not
return until that process exits. Proven on m1mini against the real `opencodeExec`, with a 1 s context:
- a fake `opencode` that prints the export and leaves a background `sleep 20`: it **took 20.1 s**, and returned
  `context deadline exceeded`, losing even a good answer;
- a fake whose foreground child outlives the killed parent (`sleep 30`): it **took 30.2 s**.

A child that never exits never returns. `opencodeCard.mu` is held across the export, so every replies request for
that card then blocks on it. The phone and the board poll, so goroutines pile up per poll, for as long as the stray
process lives.

**On Windows this is the default case, not an edge.** `npm install -g opencode-ai` installs an `opencode.cmd` shim.
`exec` runs it through `cmd.exe`, the deadline kills `cmd.exe`, and the real opencode, its grandchild, keeps
stdout. sg4 is where the live check runs.

Fix:
- Give `cmd.Stdout` a capped writer: a buffer that returns an error past `max`, so the over-cap kill still happens.
  Do not use `StdoutPipe` and `ReadAll`.
- Set `cmd.WaitDelay` (about 1 s), so `Wait` closes the pipes and returns even while a stray process holds them.
- Kill the whole tree on timeout. On unix, start it in its own process group (`Setpgid`) and kill `-pgid` from
  `cmd.Cancel`. On Windows, use a job object, or `taskkill /T /F /PID` from `cmd.Cancel`.
- Test with the two fakes above: the call returns within timeout plus WaitDelay, and a second call for the card is
  not blocked.

## Lows

- **Clear `ATRIUM_RUNNER` (and the card's atrium env) in the export's env.** opencode loads its plugins at startup,
  and atrium's own plugin (scripts/opencode/atrium.js) reports a session start whenever `ATRIUM_RUNNER` is set. The
  daemon does not set it today, so this is a guard and not a bug. An export must never look like a session to the
  board.
- **No cap on exports across cards.** Each card has one in flight, but 50 opencode cards polled at once are 50
  processes of up to 8 MB each. Put a small semaphore (2 to 4) in front of the export.
- `opencodeReaders` is a package-level `sync.Map` keyed by `*Daemon`, so every test daemon leaks one entry. That is
  harmless, and fixed by putting the reader on the Daemon.

## Verdict

HOLD 6bce194f..ffe07d9d: the export bound (High), with its test. A re-read starts at 6bce194f, room-ok (daemon
code, served on the board API, so hub-ok too, since the board reads the replies through the hub).

Quality: after the Sonnet switch, the work is clean. The interface move is behaviour-neutral, the reader follows
every rule of the brief, and the tests fake the exec well. The miss is a classic one: a timeout on the parent is not
a timeout on its output pipe, and the Windows shim makes it the common case.
