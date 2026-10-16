# Idle minion spins a core

Design note, for review. No code has changed. Gate 1: stop here until ACCEPTED.

## What was seen

2026-10-16 on claude-sg4 (pty host on): 18 finished per-item minions idle at needs-input for 1 to 5 hours, each claude
process pinned near 100% of a core. Bash took 20s, git_collect timed out. Killing exactly those PIDs cured it. Idle
cards that were not minions were not hot. The hot ones share: `--tools Bash,ToolSearch`, `--system-prompt "<line>"`,
sonnet, effort low, lean, tags `atrium:director` + `atrium:minion`, keepalive on.

## Repro: not done, and why

The machine that showed it (sg4) is not this room. Here (sgg) only one claude runs, mine, so the 18-card load cannot be
recreated, and a single idle card at 100% would only show up on the real flag set under a real pty host. What I could
do without that is read what atrium does to an idle terminal. Findings below are from the code, not a measurement.
The proposed first step is a measurement, below.

## Finding: atrium sends nothing to an idle terminal on a timer

Every ticker in `internal/daemon` and `internal/ptyhost` was read:

- keepalive (`keepalive.go`, 1 minute tick) works through a forked headless `claude -p --fork-session` as its own
  process. It never writes to the card's pty, transcript or conversation. It can burn a core only inside the fork's
  own lifetime, bounded by `keepaliveForkTimeout` (120s). It cannot hold 18 long-lived claude PIDs hot for hours.
- `idletick.go` / `idlepark.go`: only act at `idle_park_after`, by a wind-down. A card that cannot be exited (director)
  is skipped for park, so nothing is typed.
- Peer messages are queued and delivered by a hook, never typed into an idle pty (`peers.go`).
- `ptyhost` has one ticker, `idleLoop`, which looks at the host, not at a runner, and is 5s at most.
- Nothing else writes bytes, resizes, or polls a pty for a runner that is idle.

So the evidence points at the claude process itself: an idle claude TUI that re-renders or loops with nothing asked of
it. Keepalive is a coincidence of the set (every minion has it) unless the measurement below says otherwise. The flag
set (`--tools Bash,ToolSearch`, `--system-prompt`) is the other thing the hot ones share and the cold ones do not, so it
is the stronger suspect. That would be a claude bug to report, with atrium choosing whether to work around it.

## Measurement that decides it (proposed, on sg4, no atrium change)

On sg4, for 10 minutes each, sample CPU (`Get-Process` deltas) of an idle card started three ways under the pty host:

1. the exact minion flag set;
2. the same minus `--tools Bash,ToolSearch`;
3. the same minus `--system-prompt`.

Also, for one hot card, watch pty host traffic for it (bytes out of the pty per second, from the host log or the ring
offset moving): if the offset is steady, the spin is claude with no terminal input and no output churn, and atrium is
cleared. If the offset climbs, claude is repainting, and the next question is what resizes or redraws it (an attached
board tab with a stale size is the likely feeder).

## Proposed fixes

A. **The cure whatever the cause: a finished minion ends.** Self-exit is already in place on this base:
`guardExit` lets the caller end itself even when it is a director (`internal/api/exitguard.go`), and `atrium_exit`
with no card means yourself (`internal/link/control_mcp.go`, `exitHandler`). `item-minion.md` step 6 already says to
call it. So the missing piece is not the guard. It is that the 18 did not. Two things to confirm before coding:
whether a minion launched with `--tools Bash,ToolSearch` can see the MCP tool `atrium_exit` at all (`--tools` should
leave MCP tools alone, but I have not shown it), and whether the orchestrator's brief told them to. If `--tools` hides
MCP tools, the minion could not even call `atrium_done`, which would show in the card's state.
Smallest atrium change if so: a `atrium exit` CLI (the command is the channel every runner already has, as
`atrium finish` is) that does the same `POST /v1/tasks/{self}/exit`. Only if it does not exist yet: I will check on
acceptance.

B. **Backstop: park an idle director minion.** `idlepark.go` skips directors, which is why these sat for hours. Let
a card tagged `atrium:minion` be subject to the idle park (wind-down after `idle_park_after`), since a minion has no
work after it lands. This bounds the damage of any future spin to one idle window. It touches `idlepark.go` only.

C. **Not proposed:** changes to keepalive, the pty host or typing. Nothing there is implicated.

## Order of work after ACCEPTED

1. Run the measurement (or have the orchestrator run it on sg4 if I cannot reach it) and amend this note.
2. A, whichever part is missing. 3. B. Tests for B in `idlepark` integration style, under 10ms for unit parts.

## Test plan

- A minion with `atrium:minion` + `atrium:director` is parked after `idle_park_after` and not before.
- A director without `atrium:minion` is still never parked.
- A minion calling `atrium_exit` with no card is asked to leave.
- On sg4: idle minion CPU under the pty host is about 0 after the fix is live.
