# Idle minion spins a core

Design note, for review. No code has changed. Gate 1: stop here until ACCEPTED. Revision 2 follows rnd review.

## What was seen

2026-10-16 on claude-sg4 (pty host on): 18 finished per-item minions idle at needs-input for 1 to 5 hours, each claude
process pinned near 100% of a core. Bash took 20s, git_collect timed out. Killing exactly those PIDs cured it. Idle
cards that were not minions were not hot. The hot ones share: `--tools Bash,ToolSearch`, `--system-prompt "<line>"`,
sonnet, effort low, lean, tags `atrium:director` + `atrium:minion`, keepalive on.

## Repro: not done, and why

The machine that showed it (sg4) is not this room. Here (sgg) only one claude runs, mine, so the 18-card load cannot be
recreated. The measurement below is written to be run on sg4. What I could do from here is read what atrium does to an
idle terminal. Findings are from the code, not a measurement.

## Finding: atrium sends nothing to an idle terminal on a timer

Every ticker in `internal/daemon` and `internal/ptyhost` was read:

- keepalive (`keepalive.go`, 1 minute tick) works through a forked headless `claude -p --fork-session` as its own
  process. It never writes to the card's pty, transcript or conversation. It can burn a core only inside the fork's
  own lifetime, bounded by `keepaliveForkTimeout` (120s). It cannot hold 18 long-lived claude PIDs hot for hours.
- `idletick.go` / `idlepark.go`: act only at `idle_park_after`, which is off on every room.
- Peer messages are queued and delivered by a hook, never typed into an idle pty (`peers.go`).
- `ptyhost` has one ticker, `idleLoop`, which looks at the host, not at a runner.
- Nothing else writes bytes, resizes, or polls a pty for an idle runner.

So the evidence points at the claude process itself: an idle claude TUI that re-renders or loops with nothing asked of
it. Keepalive is a coincidence of the set (every minion has it) unless arm 4 below says otherwise. The flag set is the
other thing the hot ones share and the cold ones do not, so it is the stronger suspect. That would be a claude bug to
report, with atrium choosing a workaround.

## Step 0: does `--tools Bash,ToolSearch` hide the MCP tools? (do this first)

Partial answer from here: `claude -p --tools "Bash,ToolSearch"` on this machine still lists
`mcp__claude_ai_Claude_Docs__*` next to Bash and ToolSearch, so `--tools` restricts built-ins and leaves MCP tools
alone. That does NOT settle it for `atrium-control`: a bare `claude -p` here has no atrium MCP config (the same
ToolSearch for `atrium_exit` fails without `--tools` too), so the test has to run on a card atrium launched.

On sg4, with a card launched exactly as the orchestrator launches a minion (same `--tools`, `--system-prompt`), ask it
once: "call ToolSearch select:mcp__atrium-control__atrium_exit,mcp__atrium-control__atrium_done and tell me if each
schema came back". Results:

- both found: `--tools` is not why the 18 did not end. They did not exit because exit-on-report skips a director (fix
  B) and nothing told them to call `atrium_exit`.
- not found: stop passing `--tools` on minions, or add the MCP tool names to it. No new CLI: `atrium finish` exists
  and `atrium_exit` is already the self-exit.

## Measurement that decides the spin (run by the minion on claude-sg4, PowerShell)

Five arms, all idle at once, for 10 minutes. Start each as a fresh card under the pty host, let it answer one trivial
prompt, then leave it alone:

| arm | how it differs |
| --- | --- |
| 1 | the exact minion set: `--tools Bash,ToolSearch --system-prompt "<line>"`, sonnet, low, lean, keepalive on |
| 2 | arm 1 without `--tools` |
| 3 | arm 1 without `--system-prompt` |
| 4 | arm 1 with keepalive off on the card |
| 5 | control: a plain idle non-minion card (the usual launch) |

Find each card's claude pid (`Get-CimInstance Win32_Process | ? Name -eq claude.exe | select ProcessId,CommandLine`,
matching the card's flags), then:

```powershell
pwsh scripts/idle-spin-measure.ps1 -Cards a1=<card1>:<pid1>,a2=<card2>:<pid2>,a3=<card3>:<pid3>,a4=<card4>:<pid4>,a5=<card5>:<pid5> -Minutes 10
```

It samples each claude's CPU every 30s and reads the card's raw pty ring from the room board
(`/v1/tasks/<id>/scrollback/raw?collapse=0`, default `http://127.0.0.1:7781`, `-Room` to change). No route exposes the
ring offset, so the ring is hashed: `ring_growth` (bytes gained) and `ring_changes` (samples that differed) stand in
for it. It prints one table (`arm, cpu_avg, cpu_max, ring_growth, ring_changes`) and writes a CSV.

| result | meaning |
| --- | --- |
| arm 1 hot, ring steady, 2 or 3 cool | the flag is the trigger and claude spins with no terminal traffic: report to claude, work around in atrium |
| arm 1 hot, ring steady, 4 cool | keepalive is implicated through something other than the pty: look at the fork's side effects |
| arm 1 hot, ring growing | claude is repainting: something feeds or resizes it, look at attach size and notices |
| arm 5 hot too | not minion-specific: idle claude spins on this build |
| nothing hot | the spin needs the hours or the 18-at-once load: leave a card running and re-run later |

For one hot process, before it is killed (a survivor of the 18, or the arm that turns hot):

```powershell
pwsh scripts/idle-spin-capture.ps1 -ProcId <pid> -Dir C:\temp\spin
```

It writes the claude version, command line, five one-second CPU samples and the busiest threads to `hot-<pid>.txt`,
and runs `procdump -s 5 -n 3 -ma` (three dumps, five seconds apart) when procdump is on PATH. Sysinternals procdump is
not installed here, so fetch it on sg4 first or accept the thread table alone.

The measurement table goes into this note before the orchestrator accepts the design.

## Proposed fixes

A. **A finished minion can end itself.** `guardExit` already lets a caller end itself even as a director, and
`atrium_exit` with no card means yourself, so the guard is not the gap. Gated on Step 0: if the MCP tools are hidden,
stop passing `--tools` on minions (or add the tool names), in the orchestrator's launch rather than in atrium.

B. **(Replaces the idle park, which is off on every room.)** Edit `internal/daemon/exitonreport.go`. `exitsOnReport`
skips cards with `atrium:director`, and a per-item minion is one. Exempt `atrium:minion`: a card wearing both tags is
exited on a final done after `exitOnReportDelay` and the usual quiet period, like any worker. Everything else stays:
a launcher-less card, a done carrying an ask, `atrium:keep-open` and `atrium:investigation` (via `staysOpen`) keep a
card open, a plain director is never exited, and the resident and orchestrator tags still win over the minion tag.
The change is about five lines: a resident, orchestrator, park-idle or hold-notices tag skips; `DirectorTag` skips
unless the card is a minion.

C. **Not proposed:** keepalive, pty host or typing changes. Nothing there is implicated by the code read, and the
measurement is what would change that.

## Order of work after ACCEPTED

1. Amend this note with the measurement table (run by the minion on sg4).
2. B, with tests. 3. A, only if Step 0 says the tools are hidden.

## Test plan

- `exitsOnReport`: a card with `atrium:minion` + `atrium:director` and a launcher exits on a final done.
- A plain `atrium:director` does not. `atrium:minion` + `atrium:director` + `atrium:keep-open` does not.
- `atrium:minion` + `atrium:resident` or the orchestrator tag does not.
- A done carrying an ask still does not, minion or not.
- On sg4: a minion that lands and calls done is gone after the delay, and idle CPU drops to about 0.
