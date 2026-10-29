# internal/daemon

The room. Wires the store and the API together, owns the hooks, launching, and supervision. Two listeners on
purpose: one for the human, one for agents. As `atrium room` they are `:7781` (the room's own board, the hub's board
is `:7778`) and `:7777`. The older `atrium daemon` single process serves its board on `:7778`.

A room keeps two listeners on purpose. When the store halts the agent listener closes and stays closed, so runners
park on connection-refused and burn nothing, while the board stays up to say what broke.

`screen/` is the terminal screen model: the grid, replay, flatten, idle frame and width tables. It imports nothing
from the daemon and holds no daemon state, so a change there runs only its own tests.

`launch/` (the Git Bash lookup) and `publish/` (the ziti SDK enrol, the login provider key fetch) are the same: they
import nothing from the daemon, and the daemon calls their exported names.

## The permission chain: order is critical

`onPermRequest` in `daemon.go`. Each step can answer and stop. **Reordering changes what the tool means**, so
do not do it without reading why each one sits where it does.

1. **A replayed decision.** A request with a dedup key already answered returns that answer.
2. **A queued message.** Delivered as a block carrying the text, framed so the model reads it as you talking
   rather than as a policy refusal. Ahead of everything else because a message is you reaching out.
3. **A shelved card.** A standing no. Putting work down has to answer for that work, or the agent freezes
   behind a card you stopped looking at.
4. **A standing rule.** The most specific match wins. Prefix, glob, or folder.
5. **Auto mode.** Approve without asking, record everything. LAST on purpose: it means "stop asking me new
   questions", not "forget the answers I already gave", so 2, 3 and 4 all still win.
6. **Ask the human.** The card moves to `needs-permission` and the agent blocks.

## Resilience guarantees. Do not weaken these

1. **Storage failure halts, it does not degrade.** See `internal/store/AGENTS.md`.
2. **A hook must never fail a session.** Everything posted by a hook is best effort. `/activity` answers before
   it does any work and swallows unknown agents, `/session` logs and moves on, and the permission hook fails
   open when atrium is unreachable. A session must never fail to start, finish a turn, or make a tool call
   because atrium was not listening.
3. **`/activity` is fire and forget from the caller's side too.** It rides `PreToolUse`, the hot path for every
   tool call in every session. One second timeout, every failure ignored, nothing downstream reads the result.
   Do not add a retry. Do not raise the timeout. `docs/runtime/activity-design.md`.
4. **Shutdown is narrated and bounded.** Event streams released first, since `Shutdown` waits on in-flight
   requests and one open tab would otherwise hold the whole grace period. Supervised runners get ten seconds.
   Every step logs, because several seconds of silence reads as a hang.
5. **A kill is not a stop.** Without the `pty_host` setting, closing a pseudo terminal takes the attached process
   with it, so `taskkill` on the room ends every runner at once. Use `atrium stop`. With `pty_host` on, the
   terminals live in a separate pty host process (`internal/ptyhost`) and survive a room restart.
6. **A source is a suggestion, never durable state.** Intake cannot halt anything. A failing source is reported
   on its own row and retried on its next tick, its output is bounded WHILE being read rather than after, and
   three consecutive failures switch it off with the reason attached. A run that could not land everything is
   recorded as a failure, because items already offered are deduplicated on the next tick and the thing that must
   not happen is a row claiming success while the inbox is short.
7. **`/finish` follows the hook posture.** An agent atrium has never heard of gets `ok` and nothing recorded,
   because failing there would mean a session could fail at the moment it tried to end tidily.
8. **Files leave a card only to the card, an atrium path or an `open_paths` folder.** Upload computes its own
   destination and accepts no path, download resolves one through `internal/base/safepath`, and everything else answers
   `403` whether or not it exists. The rule lives in `internal/api/AGENTS.md`.

## Supervision: two maps, and keeping them apart is the design

`supervisor` holds `runners` and `shells`, both keyed by card.

`runners` is the process doing the work. The reaper asks the operating system about it, the park tells it to
stop, shelving refuses on its behalf, and its exit files the card as `dead`. **Every existing `sup.Get(taskID)`
means the runner.** Consumers reach it as a `Runner` through the `Registry` that `supervisor` implements
(`registry.go`), and the supervision internals still hold `*runner` through `sup.get`.

`shells` is the plain shell an operator may open beside a wedged agent. It has no card status, writes no event,
is never resumed or retried, and does not survive its card. Folding it into `runners` is the obvious
simplification and the first consequence is a card filed as dead when somebody types `exit`.

**Close a pseudo terminal through `closePTY`, never `pty.Close()` directly.** Two paths reach it, and a double
close of a Windows handle is not a Go panic: the process disappears with no output at all.

## Out of scope, deliberately

**Injecting prompts into a running claude session from outside MCP.** There is no IPC channel into a claude
process; the agent loop IS the IPC.

This holds for the peer bus too: one session messaging another is QUEUED and delivered by a hook, never typed
into the target's terminal, even where atrium owns one and could. `peers.go` says so at length because the
tempting simplification is to reuse `handleMessage`, which does type.

**A lent session is the agent's terminal and never the card's shell.** `overlay_guest.go` is an allowlist, and
the hazard it did not catch on its own is a route that grows a parameter: `<id>/attach` was already allowed when
`?kind=shell` started meaning something else. An endpoint that grows a parameter is a new endpoint.

## The files

| | |
| --- | --- |
| `daemon.go` | both listeners, the permission chain, the halt |
| `activity.go` | what a runner is doing right now. In memory on purpose |
| `supervisor.go` | pseudo terminals atrium owns |
| `shell.go` | the second terminal a card may hold, and everything it is not |
| `attach.go` | the websocket carrying one of those terminals. `?kind=shell` picks which |
| `messages.go` | saying something to a running session |
| `session.go` | join, leave, start, end, and the gate question |
| `launch.go` | spawning a runner from a harness row |
| `shutdown.go` | stopping without killing |
| `reaper.go` | free liveness: ask the OS, not the agent |
| `orphans.go` | a question asked for a session that has since gone |
| `drain.go` | turning auto mode on empties the queue it was turned on because of |
| `overlay*.go` | driving zrok or OpenZiti so the board is reachable from elsewhere |
| `fixtures.go` | terminals that come up with the daemon |
| `whereami.go` | where the daemon is listening AND which binary it is, written down for the CLI |
| `sources.go` | running the commands that find work, bounded and reported on the row |
| `finish.go` | a session declaring its work over |
| `actions.go` | saying a stored prompt to a card, and asking the runner to leave |
| `sweep.go` | two timers that are easy to confuse: archiving, and deleting |
| `task_launch.go` / `task_capture.go` | a task of an item: the folder atrium makes and removes by its ledger row, and capturing its files to the hub first |
| `newcontext_journal.go` | new contexts in flight, journalled so a restart can end them with a reason |
