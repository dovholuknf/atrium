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

## Re-read: b305efd5

Range `6bce194f..b305efd5`. b305efd5 is one commit on ffe07d9d, in `internal/daemon/` only (opencodereader.go, the
new opencodeproc_unix.go and opencodeproc_windows.go, two fields on Daemon, and tests), plus the changelog line.
Unsigned, like every m1mini commit.

Closed:
- **The High: the export is now bounded.**
  - stdout is a `cappedWriter` on `cmd.Stdout`. There is no pipe read to EOF any more.
  - `cmd.WaitDelay` is 1 s.
  - The export starts in its own process group (`Setpgid`), and `cmd.Cancel` kills `-pgid`. `reapTree` kills
    whatever is left in the group once the export returns.
  - On Windows, `cmd.Cancel` runs `taskkill /T /F /PID`. If taskkill fails, WaitDelay still bounds `Wait`, and Go
    then kills the direct child.
  - Rerun on m1mini against the real `opencodeExec`, with a 1 s context and the default 1 s WaitDelay, each fake
    called twice:
    - the fake that prints a good export and leaves `sleep 20 &`: **1.13 s and 1.01 s**, down from 20.1 s. The
      export is kept and parses, and the `sleep 20` is gone afterwards, so the group kill took it.
    - the fake whose `sleep 30` outlives the parent: **1.00 s and 1.00 s**, down from 30.2 s, with `signal: killed`.
    - a third fake, whose child leaves the group with `setsid` and holds stdout: **1.17 s and 1.01 s**. The call is
      bounded, and only the escaped child survives (N2).
  - The card's lock is still held across the export, but the export now ends within `opencodeTimeout` (8 s) plus
    1 s, so a poll waits at most that long.
  - `TestOpencodeExecIsBoundedAgainstAChildHoldingStdout` covers both of my fakes, with a second call each.
- **The cap.** Past `max`, the writer marks the export as over, cancels the context (which kills the group), and
  the call returns nothing. The test runs `yes` against a 1 KiB cap.
- **A good answer printed before a timeout is kept.** The output is parsed whatever the error, and a cut-off JSON
  fails the parse, so only a complete export is used. `TestOpencodeKeepsAnAnswerPrintedBeforeATimeout` covers this.
- **The three Lows.**
  - `opencodeEnv()` drops every `ATRIUM_*` variable, and a test checks it.
  - At most 3 cards export at once (`opencodeSlots`).
  - The reader is on the Daemon, behind a `sync.Once`, so the package-level map is gone.

`go vet` is clean, and so is `GOOS=windows go vet`. `go test ./internal/daemon/ -run 'Opencode|Replies|Transcript'
-count=1` passes all 33. `gofmt -l` flags only `fyi_test.go`, the known low from before.

Open, Lows, none of which holds the merge:
- **N1: `reapTree` kills `-pgid` after `Wait` has reaped the leader.**
  - If the group is empty by then, its id is free, and in theory a new process could start a group with the same
    number and take the SIGKILL.
  - That needs the pid to be reused within microseconds, so the risk is small.
  - Kill the group only when the export did not exit cleanly, or note the race.
- **N2: a child that calls `setsid` leaves the group and survives on unix.** The call itself is still bounded by
  WaitDelay. A comment saying so is enough. A real opencode does not do this.
- **N3: the slot is taken and given back by hand, not with `defer`.** A panic inside `opencodeExec` would keep a
  slot. Give it back in a closure with `defer`.
- **Cost:** an export that exits normally but leaves a background child holding stdout costs the full 1 s WaitDelay
  on every call. That is acceptable at a 3 s cache.

Landing note: its test-plan section "IC" collides with landing's IC and ID, so it lands renamed to **IE**.

Atrium-Verdict: room-ok 6bce194f..b305efd5
Atrium-Verdict: hub-ok 6bce194f..b305efd5
Quality: a complete fix. Each of the four suggested parts is in, the Windows shim case is handled by reading the
tree kill, and the tests cover the bug that was proven.
