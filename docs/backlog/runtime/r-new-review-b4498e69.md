# Review: r-exit-guard 5f2bcd58..b4498e69 (m1mini, 2026-10-02): HOLD

One commit on claude/r-exit-guard, one of clint's exceptions. It changes four things:

- `atrium_exit` with no card exits the caller.
- The room refuses any card that is neither the caller nor one it launched, unless `force` is set, in which case the
  force is recorded on that card.
- An agent can never exit a director (`atrium:director` or `atrium:context-ceiling`).
- An `atrium_say` reply names the recipient as `to_card`, not `card`.

The commit is unsigned.

## What holds

- **The check is in one place, and it is the right place.** `guardExit` (internal/api/exitguard.go) runs in the room
  route that owns the card, `POST /v1/tasks/{id}/exit`. The stdio tool, the hub tool, the `atrium exit` command and a
  cross-room ask all reach it.
- **The order is right.**
  - A director is refused before anything else, so neither the self rule nor force gets past it.
  - An empty `from` is the operator (the board's button, or a shell with no `ATRIUM_AGENT_NAME`), and it can do
    anything.
- **The trust is stated, not implied.**
  - The caller is taken to be who it says it is, and the route is as open as the rest of the board. So the guard
    stops the mistake that exited a director, not an attacker.
  - An agent can still curl the route with no `from`. That is the honest scope, and the changelog and a code comment
    say so.
  - No MCP tool can change an existing card's tags, so an agent cannot untag a director through a tool. Tags are set
    only at launch.
- **`to_card`.**
  - The rename happens in both say outputs (stdio and hub), on every path that fills them.
  - Both tool schemas tell the model never to copy a say reply's id into `atrium_exit`.
  - Nothing outside the two tests read `card`.
- **Force is recorded on the target,** as a `forced_exit` notice with the asker's name, written before the stop.
- **Tests:**
  - `go build` and `go vet` are clean.
  - `TestExitGuard*`, `TestAHubSideExitWithNoCardExitsTheCaller` and `TestStdioExitWithNoCardExitsTheCaller` pass,
    and the link and cli packages pass in full.
  - Mutants that drop any of these each fail a new test: the director check, the self rule, the launched-child
    rule, the forced-exit record, and the stdio "no card is me" default.
  - Unrelated reds on macOS: `TestTheWalkerLaunchSetAndClear` (the `/private` path, which fails the same way at
    5f2bcd58) and two hostterm socket-path tests.

## M1: the cross-room path is untested, and losing `from` on any hop turns an agent into the operator

A cross-room ask passes `from` and `force` through four hops:

1. the room's tool to `/v1/peers/exit`;
2. `handleRoomExit` to `Relay.Exit`;
3. `linkRelay` to `RelayRequest{From, Force}`;
4. the hub's `exitAcross` to the target room's `/exit` body, marked foreign.

If any hop drops them, the target room sees no `from`, which means the operator, and the guard does not run. A
director on another room can then be exited by exactly the mistake this item closes.

Three mutants survive every test:

- `exitAcross` sends no body;
- `handleRoomExit` passes `""` and `false`;
- the hub tool's cross-room ask is sent without the `@room` and foreign marking.

A fourth survives too: `guardExit` ignoring `Foreign`. `TestExitGuardForeignCallerIsNotTheLocalCard` sends
`"me@elsewhere"`, which matches no local name anyway, so it does not test the flag.

Fix:

- Add one relay-pair test (`newRelayPair` already has two rooms) that asks a card on the other room to exit, from a
  named agent.
  - Assert that the target room's `/exit` received `from: "<me>@<room>"`, `foreign: true` and the `force` value.
  - Run it once with a director as the target, which must be refused even with force.
  - Run it once with a card the asker did not launch, which must be refused without force.
- Make the foreign test send `from: "me"` and `foreign: true`, against the local `me`.

Version skew, not a hold: an asking room or a hub on an older build sends no `From`, so a cross-room ask from it
arrives as the operator's. That is what happens today, so nothing gets worse. Say it in the changelog, so the hub and
the rooms are deployed together.

## M2: rebase onto landing, where a cross-room launch exists and its child cannot be exited without force

claude/landing has cross-room launch: `RelayLaunch` and `launchAcross`. It records the launcher as
`SpawnedBy = "<me>@<room>"` and `SpawnedByID = "<room>~<id>"`.

`guardExit` skips the launched-child rule whenever the caller is foreign. So a director on m1mini cannot exit a
worker it launched on sg4 without `force`, and every such routine exit would be recorded as a forced one.

The merge onto 526337e1 conflicts in three files:

- `docs/test-plan.md`;
- `internal/daemon/relay.go`, where the `Relay` interface gains `Launch`;
- `internal/link/relay.go`, where `RelayRequest` gains `Launch`.

The code conflicts are mechanical: keep both sides.

Fix, on the rebase: in the foreign branch, accept a target whose `SpawnedBy` equals the foreign `from`, ignoring
case. The room part of that name is the asking room, as the hub saw it on the relay connection. Or accept a target
whose `SpawnedByID` is `<fromRoom>~<caller id>`. Add this case to the M1 relay test.

## Lows

- **L1: a body that does not parse is treated as the operator's.** `exitRunner` ignores the decode error, so a
  truncated or malformed body counts as having no `from`. Treat `io.EOF` (no body, which is what the old callers
  send) as the operator, and answer 400 to any other decode error.
- **L2: the tools that force cannot send a reason.** The forced-exit record keeps `why`, but neither MCP tool nor
  `atrium exit` has a field for it, so every recorded force has no reason. Add a short `why` (capped at, say, 200
  runes) to both tools and the command, or drop it from the record.
- **L3: a director cannot exit itself, and the message hides that.** `atrium_exit` with no card, from a director, is
  refused with "an agent cannot exit one". That matches the rule, but it reads as if someone else were the target.
  When the target is the caller, say "you are a director: only the operator ends you".
- **L4: the launcher is matched by name on old rows.** With no `SpawnedByID`, the launcher is matched by `SpawnedBy`
  against the caller's wire name, so a later card that reuses the handle passes as the launcher. The code already
  prefers the id whenever one is recorded. Say so in the comment.
- **L5: a stale example.** `docs/fabric/cross-room-say-design.md:95` still shows a say reply with `"card"`.
- **Landing note:** this branch's test-plan section is IE. In landing order, r-opencode-bubbles took IE and
  r-scm-clone took IH, and r-restart-loopback takes IF, so this one lands as **IG**.

## Re-read

From 5f2bcd58, after the rebase onto landing. Check:

- the M1 relay test, against the three hop mutants and the `Foreign` one;
- the M2 cross-room-child case.

Atrium-Verdict: hold 5f2bcd58..b4498e69
Quality: the right fix in the right place: one guard in the room route that every caller reaches, the director
checked first, and an honest statement of what it does not stop. The cross-room path, which it most needs to cover,
is the one left untested.
